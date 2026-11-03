//go:build unix

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// leftoverStep starts a background sleep that inherits the step's output,
// records its PID, then runs body.
func leftoverStep(t *testing.T, body string) (PipelineStep, string, string) {
	t.Helper()
	root := t.TempDir()
	pidFile := filepath.Join(root, "leftover.pid")
	return PipelineStep{Name: "leftover", Command: "/bin/sh", Args: []string{"-c", "sleep 300 & echo $! > leftover.pid; " + body}}, root, pidFile
}

func leftoverPID(t *testing.T, pidFile string) int {
	t.Helper()
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read leftover PID: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// assertGone waits briefly for pid to disappear; a killed child may linger
// as a zombie until its parent, the step's shell, is reaped.
func assertGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
			if fields := strings.Fields(string(raw)); len(fields) > 2 && fields[2] == "Z" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("the step's background process %d outlived the step", pid)
}

func TestHostStepTimeoutKillsDescendantsAndReturns(t *testing.T) {
	step, root, pidFile := leftoverStep(t, "sleep 1000")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var output bytes.Buffer
	started := time.Now()
	err := hostBackend{}.RunStep(ctx, root, step, []string{"PATH=/usr/bin:/bin"}, &output)
	if err == nil {
		t.Fatal("a timed-out step reported success")
	}
	if elapsed := time.Since(started); elapsed > 20*time.Second {
		t.Fatalf("a timed-out step returned after %s; a background process held its output", elapsed)
	}
	assertGone(t, leftoverPID(t, pidFile))
}

func TestHostStepThatExitsLeavesNoBackgroundProcess(t *testing.T) {
	step, root, pidFile := leftoverStep(t, "echo done")
	var output bytes.Buffer
	started := time.Now()
	if err := (hostBackend{}).RunStep(context.Background(), root, step, []string{"PATH=/usr/bin:/bin"}, &output); err != nil {
		t.Fatalf("a successful step that left a background process failed: %v\n%s", err, output.String())
	}
	if elapsed := time.Since(started); elapsed > stepWaitDelay+10*time.Second {
		t.Fatalf("a finished step returned after %s", elapsed)
	}
	if !strings.Contains(output.String(), "done") {
		t.Fatalf("step output lost: %q", output.String())
	}
	assertGone(t, leftoverPID(t, pidFile))
}
