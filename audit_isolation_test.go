package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Audit snapshot verification runs Git only in its own temporary
// repository. A caller that switches system and global configuration off,
// here with a malformed global file the exclusion hides, still verifies a
// valid audit fact: the private commands keep those sources off rather
// than dropping the exclusions with the other inherited GIT_CONFIG*
// variables.
func TestAuditVerificationKeepsConfigurationExclusions(t *testing.T) {
	_, id, at := auditFixture(t)
	auth := auditAuthorizeFixture(t, id, at)
	home := t.TempDir()
	for _, file := range []string{filepath.Join(home, ".gitconfig"), filepath.Join(home, "xdg", "git", "config")} {
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("deliberately malformed configuration\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if _, err := gitOutput("rev-parse", "--git-dir"); err != nil {
		t.Fatalf("the excluded configuration affects the caller: %v", err)
	}
	if err := verifyAuditPack(auth.Attachments["audit.pack"], auth.Event); err != nil {
		t.Fatalf("verification read excluded configuration: %v", err)
	}
	if _, err := loadStoredEventAt("", auth.Commit); err != nil {
		t.Fatalf("loadStoredEventAt read excluded configuration: %v", err)
	}
	if _, err := issueReadStoredEvents([]string{auth.Commit}, defaultIssueReadLimits()); err != nil {
		t.Fatalf("the batched reader read excluded configuration: %v", err)
	}
}

// Verification writes nothing outside its temporary repository, which it
// removes, whatever template the ambient configuration names: from the
// global configuration file, from configuration in the environment or
// from GIT_TEMPLATE_DIR. The template here links the new repository's
// objects directory to another object database. A valid pack, a pack
// refused for an extra object and a pack refused by Git's object syntax
// check all leave that database and the temporary directory as they were.
func TestAuditVerificationWritesOnlyItsTemporaryRepository(t *testing.T) {
	_, id, at := auditFixture(t)
	auth := auditAuthorizeFixture(t, id, at)
	pack := auth.Attachments["audit.pack"]
	ids, err := auditPackObjectIDs(pack, len(at))
	if err != nil {
		t.Fatal(err)
	}
	extra := mustGitTextFromInput(t, []byte("an object no signed snapshot names\n"), "hash-object", "-w", "--stdin")
	withExtra, err := gitInput([]byte(strings.Join(append(slices.Clone(ids), extra), "\n")+"\n"), nil, "pack-objects", "--stdout", "--window=0", "--depth=0")
	if err != nil {
		t.Fatal(err)
	}
	malformed, malformedEvent := auditMalformedCommitPack(t, auth)

	cases := map[string]func(t *testing.T, template string){
		"global configuration": func(t *testing.T, template string) {
			home := t.TempDir()
			global := filepath.Join(home, ".gitconfig")
			if err := os.WriteFile(global, []byte("[init]\n\ttemplateDir = "+template+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("GIT_CONFIG_GLOBAL", global)
		},
		"environment configuration": func(t *testing.T, template string) {
			t.Setenv("GIT_CONFIG_COUNT", "1")
			t.Setenv("GIT_CONFIG_KEY_0", "init.templateDir")
			t.Setenv("GIT_CONFIG_VALUE_0", template)
		},
		"template variable": func(t *testing.T, template string) {
			t.Setenv("GIT_TEMPLATE_DIR", template)
		},
	}
	for _, name := range slices.Sorted(maps.Keys(cases)) {
		t.Run(name, func(t *testing.T) {
			external := migrationBare(t)
			template := t.TempDir()
			if err := os.Symlink(filepath.Join(external, "objects"), filepath.Join(template, "objects")); err != nil {
				t.Fatal(err)
			}
			cases[name](t, template)
			temporary := t.TempDir()
			t.Setenv("TMPDIR", temporary)
			before := auditObjectFiles(t, external)
			if err := verifyAuditPack(pack, auth.Event); err != nil {
				t.Fatalf("valid pack: %v", err)
			}
			if err := verifyAuditPack(withExtra, auth.Event); err == nil {
				t.Fatal("a pack with an extra object verified")
			}
			if err := verifyAuditPack(malformed, malformedEvent); err == nil || !strings.Contains(err.Error(), "index-pack") {
				t.Fatalf("a pack Git's object check refuses gave %v, want an index-pack refusal", err)
			}
			if after := auditObjectFiles(t, external); !slices.Equal(after, before) {
				t.Fatalf("verification wrote %q into the template's object database, which held %q", after, before)
			}
			if left, err := os.ReadDir(temporary); err != nil || len(left) != 0 {
				t.Fatalf("verification left %v in the temporary directory: %v", left, err)
			}
		})
	}
}

// auditMalformedCommitPack returns a pack and an audit fact whose policy
// and source commit is auth's policy commit rewritten with an invalid
// author date: the pack is exact for the fact and its shapes are valid,
// so only Git's object syntax check refuses it.
func auditMalformedCommitPack(t *testing.T, auth *StoredEvent) ([]byte, Event) {
	t.Helper()
	at := auth.Event.Audit.PolicyCommit
	raw := fmt.Sprintf("tree %s\nauthor Test <test@hn.invalid> not-a-date +0000\ncommitter Test <test@hn.invalid> 1000000000 +0000\n\nmessage\n", mustGitText(t, "rev-parse", at+"^{tree}"))
	commit := mustGitTextFromInput(t, []byte(raw), "hash-object", "--literally", "-w", "-t", "commit", "--stdin")
	e := auth.Event
	fact := *e.Audit
	source := *fact.Source
	fact.PolicyCommit, source.Commit = commit, commit
	fact.Source = &source
	e.Audit = &fact
	attachments, err := auditAttachments(e, nil)
	if err != nil {
		t.Fatal(err)
	}
	return attachments["audit.pack"], e
}

// auditObjectFiles lists the files below gitDir's object database.
func auditObjectFiles(t *testing.T, gitDir string) []string {
	t.Helper()
	var files []string
	root := filepath.Join(gitDir, "objects")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			files = append(files, strings.TrimPrefix(path, root))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
