package main

import (
	"os"
	"testing"
)

// Git's fsck refuses a pack whose tree names a .gitattributes or .gitmodules
// blob it does not carry. A repository with those files at the root and in a
// tree on the policy path must still authorize an audit rule, and the pack
// must carry exactly those blobs.
func TestAuditAuthorizeCarriesGitattributesAndGitmodulesOnThePath(t *testing.T) {
	_, id, at := auditFixture(t)
	files := map[string]string{
		".gitattributes":               "*.bin binary\n",
		".gitmodules":                  "",
		".hn/.gitattributes":           "*.json text\n",
		".hn/pipelines/.gitattributes": "*.json -diff\n",
	}
	for name, content := range files {
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		mustGit(t, "add", name)
	}
	mustGit(t, "commit", "-qm", "attributes and modules")
	at = mustGitText(t, "rev-parse", "HEAD")
	auth := auditAuthorizeFixture(t, id, at)
	objects, err := auditPackObjectIDs(auth.Attachments["audit.pack"], len(at))
	if err != nil {
		t.Fatal(err)
	}
	carried := map[string]bool{}
	for _, object := range objects {
		carried[object] = true
	}
	for name := range files {
		blob := mustGitText(t, "rev-parse", at+":"+name)
		if !carried[blob] {
			t.Fatalf("audit pack does not carry %s (%s)", name, blob)
		}
	}
	if _, err := AuditRequest(at, "check", "", id); err != nil {
		t.Fatalf("audit request after authorization: %v", err)
	}
}
