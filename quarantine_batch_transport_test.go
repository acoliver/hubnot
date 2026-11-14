package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A real Git protocol proxy injects faults only at the subprocess boundary.
// The event bytes, cryptography and transaction use the production code.
func TestQuarantineBatchFinalizationFallback(t *testing.T) {
	for _, fault := range []string{"late-exit", "surplus", "partial-pair"} {
		t.Run(fault, func(t *testing.T) {
			selection, actor, first, root := setupSingleActorReplication(t)
			receiver, _ := os.Getwd()
			if err := os.Chdir(filepath.Join(root, "publisher")); err != nil {
				t.Fatal(err)
			}
			event, err := nextEvent(actor, "issue.open")
			if err != nil {
				t.Fatal(err)
			}
			event.Title = "second signed event"
			last, err := appendEvent(event, actor)
			if err != nil {
				t.Fatal(err)
			}
			mustGit(t, "push", "-q", "origin", actorRef(actor.Actor)+":"+actorRef(actor.Actor))
			if err := os.Chdir(receiver); err != nil {
				t.Fatal(err)
			}
			shim := t.TempDir()
			seen := filepath.Join(shim, "seen")
			trace := filepath.Join(shim, "trace.jsonl")
			script := `#!/usr/bin/env python3
import os,subprocess,sys
if '--batch' not in sys.argv:
 os.execv('/usr/bin/git',['git']+sys.argv[1:])
with open(os.environ['QB_BATCH_SEEN'],'a') as f:f.write('hit\n')
p=subprocess.Popen(['/usr/bin/git']+sys.argv[1:],stdin=subprocess.PIPE,stdout=subprocess.PIPE)
try:
 for n,line in enumerate(sys.stdin.buffer):
  if os.environ['QB_BATCH_FAULT']=='partial-pair' and n==2:
   sys.stdout.buffer.write(b'truncated');sys.stdout.buffer.flush();sys.exit(0)
  p.stdin.write(line);p.stdin.flush()
  header=p.stdout.readline();size=int(header.split()[2]);body=p.stdout.read(size+1)
  sys.stdout.buffer.write(header+body);sys.stdout.buffer.flush()
 p.stdin.close();status=p.wait()
 if os.environ['QB_BATCH_FAULT']=='surplus':
  sys.stdout.buffer.write(b'surplus\n');sys.stdout.buffer.flush()
 sys.exit(7 if os.environ['QB_BATCH_FAULT']=='late-exit' else status)
finally:
 if p.poll() is None:p.kill();p.wait()
`
			if err := os.WriteFile(filepath.Join(shim, "git"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("QB_BATCH_SEEN", seen)
			t.Setenv("QB_BATCH_FAULT", fault)
			t.Setenv("GIT_TRACE2_EVENT", trace)
			result, err := runReplicationTransaction(selection)
			if err != nil || result.hasFailures() {
				t.Fatalf("normal fallback import: %+v %v", result.Outcomes, err)
			}
			assertRefValue(t, acceptedActorRef("origin", actor.Actor), last.Commit)
			if _, err := os.Stat(seen); err != nil {
				t.Fatalf("batch fault unused: %v", err)
			}
			raw, err := os.ReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			// Finalization failure must discard every pair, even the earlier complete
			// pair. The ordinary loader exposes fallback as real per-blob Git calls.
			if !strings.Contains(string(raw), first.Commit+":event.json") || !strings.Contains(string(raw), last.Commit+":event.json") {
				t.Fatal("finalization failure did not use the ordinary loader")
			}
		})
	}
}
