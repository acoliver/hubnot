package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Known-answer keys generated once with ssh-keygen; the fingerprints are the
// second field of ssh-keygen -l -E sha256 -f KEY.pub. The private halves
// were discarded.
const (
	serveKeysTestEd25519Line        = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICqPfpPz/R1uz/vd9nUpjEhAMWYVmeerTHMSJDGYgg3A kat@example"
	serveKeysTestEd25519Fingerprint = "SHA256:Mnm7DCteTtegvPg/NcGZVvsMTFM5gR+dbIBbGci5vSg"
	serveKeysTestEd25519Canonical   = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICqPfpPz/R1uz/vd9nUpjEhAMWYVmeerTHMSJDGYgg3A"
	serveKeysTestECDSALine          = "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBKetOwlkf3I6Wwaceiac+c+lnMQvaa1FGsLGeTvmKPx4yihM934N1hDRyGBgk/4tOKuyZHglc7lWIWDc91duqAo="
	serveKeysTestECDSAFingerprint   = "SHA256:abAM+G2dCj1j8Lezv3Nv9jitQtaAIvAlXa5Oo3f8Z3U"
	serveKeysTestRSALine            = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDQq591T7GNfeHFInkNOPWpx1hTTPheNyeGFGfouKd+RCXJVhOb5WkNXVU+Oq1GpJsZmGv5eCTiYAuLfRPD+lqv+GMm2Sbdd0seQ5snbBOxYDG4nudYax8/kBQnxjCgWYBPlUhxoobOiEE8EalgF6na1k4RfOe2/QyToVcSvaDdk+O6X0Jd67CxlrDVsU0UZh+dVsF7dm02yYYO55BaOBgTOMs5FeBWWEjsLrs4X54+W96mIsPP5VZ1WzgVc1ZbeaD56+uS3IL3hiJaJWPGIU3sQEYmtWQMZa+f6n+Vzd28QtdTvnSZPFgd7x8RE9BkbFnoHZKOIFwXyzmfRxbVXMov rsa"
	serveKeysTestRSAFingerprint     = "SHA256:TKBWi4b++4aoAgxl7sh+MHpGCJqqtSvsgG5hEqFkXmQ"

	// serveKeysTestContractActor and serveKeysTestHNPath are the
	// contracts/server-state.md example values.
	serveKeysTestContractActor = "4f1c9a6d0d2b7e3a8c5f6e1b9d0a7c3e2f4b6d8a0c1e3f5a7b9d1c3e5f7a9b1c"
	serveKeysTestHNPath        = "/usr/local/bin/hn"
)

// serveTestKey is a fresh ed25519 key whose OpenSSH public line and
// fingerprint are built without the code under test. Later harness code
// (WP05, WP06) reuses it; keep its name and shape stable.
type serveTestKey struct {
	Private     ed25519.PrivateKey
	PublicLine  string // "ssh-ed25519 BASE64 comment"
	Fingerprint string
}

func newServeTestKey(t *testing.T, comment string) serveTestKey {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	blob := serveTestBlob("ssh-ed25519", string(public))
	line := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob)
	if comment != "" {
		line += " " + comment
	}
	sum := sha256.Sum256(blob)
	return serveTestKey{
		Private:     private,
		PublicLine:  line,
		Fingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]),
	}
}

// serveTestBlob builds an OpenSSH key blob from uint32 big-endian
// length-prefixed strings.
func serveTestBlob(fields ...string) []byte {
	var blob []byte
	for _, field := range fields {
		blob = binary.BigEndian.AppendUint32(blob, uint32(len(field)))
		blob = append(blob, field...)
	}
	return blob
}

// serveTestBlobFields splits a blob built from well-formed length-prefixed
// strings; it is only used on known-good blobs.
func serveTestBlobFields(t *testing.T, blob []byte) []string {
	t.Helper()
	var fields []string
	for len(blob) > 0 {
		length := binary.BigEndian.Uint32(blob)
		fields = append(fields, string(blob[4:4+length]))
		blob = blob[4+length:]
	}
	return fields
}

// serveTestKeyLine is the "TYPE BASE64" line for a blob whose first string
// is its type.
func serveTestKeyLine(blob []byte) string {
	length := binary.BigEndian.Uint32(blob)
	return string(blob[4:4+length]) + " " + base64.StdEncoding.EncodeToString(blob)
}

func serveKeysTestBlobOf(t *testing.T, line string) []byte {
	t.Helper()
	fields := strings.Fields(line)
	blob, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

// serveKeysTestBinding binds a public key line to the contract actor.
func serveKeysTestBinding(publicLine, fingerprint, actor string) serveKeyBinding {
	fields := strings.Fields(publicLine)
	return serveKeyBinding{
		Fingerprint: fingerprint,
		PublicKey:   fields[0] + " " + fields[1],
		Actor:       actor,
		Added:       "2026-10-24T20:00:00Z",
	}
}

// T008: parsing.

func TestServeKeysParseCanonicalFormIgnoresCommentAndOptions(t *testing.T) {
	keyAndBlob := serveKeysTestEd25519Canonical
	lines := []string{
		keyAndBlob,
		keyAndBlob + " kat@example",
		keyAndBlob + " a comment with spaces",
		keyAndBlob + "\n",
		keyAndBlob + " kat@example\r\n",
		"   " + keyAndBlob + "    trailing   comment ",
		`restrict,command="echo a b" ` + keyAndBlob + " kat@example",
		`from="10.0.0.1",command="say \"x y\" now" ` + keyAndBlob,
		"restrict " + keyAndBlob,
		`restrict,command="/usr/local/bin/hn serve git --key SHA256:x" ` + keyAndBlob + " hn-serve:4f1c9a6d0d2b",
	}
	for _, line := range lines {
		parsed, err := parseServePublicKey(line)
		if err != nil {
			t.Fatalf("parseServePublicKey(%q): %v", line, err)
		}
		if parsed.Type != "ssh-ed25519" || parsed.Canonical != serveKeysTestEd25519Canonical ||
			parsed.Fingerprint != serveKeysTestEd25519Fingerprint {
			t.Fatalf("parseServePublicKey(%q) = %+v, want canonical %q and fingerprint %s", line, parsed, serveKeysTestEd25519Canonical, serveKeysTestEd25519Fingerprint)
		}
		if !bytes.Equal(parsed.Blob, serveKeysTestBlobOf(t, keyAndBlob)) {
			t.Fatalf("parseServePublicKey(%q) blob differs from the decoded key", line)
		}
	}
}

func TestServeKeysParseAcceptsEverySupportedType(t *testing.T) {
	point := strings.Repeat("\x04", 65)
	blobs := map[string][]byte{
		"ssh-ed25519":                        serveTestBlob("ssh-ed25519", strings.Repeat("k", 32)),
		"sk-ssh-ed25519@openssh.com":         serveTestBlob("sk-ssh-ed25519@openssh.com", strings.Repeat("k", 32), "ssh:"),
		"ecdsa-sha2-nistp256":                serveTestBlob("ecdsa-sha2-nistp256", "nistp256", point),
		"ecdsa-sha2-nistp384":                serveTestBlob("ecdsa-sha2-nistp384", "nistp384", strings.Repeat("\x04", 97)),
		"ecdsa-sha2-nistp521":                serveTestBlob("ecdsa-sha2-nistp521", "nistp521", strings.Repeat("\x04", 133)),
		"sk-ecdsa-sha2-nistp256@openssh.com": serveTestBlob("sk-ecdsa-sha2-nistp256@openssh.com", "nistp256", point, "ssh:"),
		"ssh-rsa":                            serveTestBlob("ssh-rsa", "\x01\x00\x01", strings.Repeat("\xc1", 257)),
	}
	if len(blobs) != 7 {
		t.Fatalf("test covers %d types, want the 7 supported types", len(blobs))
	}
	for keyType, blob := range blobs {
		line := keyType + " " + base64.StdEncoding.EncodeToString(blob) + " comment"
		parsed, err := parseServePublicKey(line)
		if err != nil {
			t.Fatalf("%s: %v", keyType, err)
		}
		sum := sha256.Sum256(blob)
		if parsed.Type != keyType || parsed.Fingerprint != "SHA256:"+base64.RawStdEncoding.EncodeToString(sum[:]) ||
			parsed.Canonical != strings.TrimSuffix(line, " comment") {
			t.Fatalf("%s parsed to %+v", keyType, parsed)
		}
	}
}

func TestServeKeysParseRefusesMalformedInput(t *testing.T) {
	ed25519Blob := serveKeysTestBlobOf(t, serveKeysTestEd25519Line)
	encode := base64.StdEncoding.EncodeToString
	cases := []struct {
		name, line, fragment string
	}{
		{"empty", "", "empty"},
		{"only newline", "\n", "empty"},
		{"two newlines", "\n\n", "control byte 0x0a"},
		{"multi-line", serveKeysTestEd25519Canonical + "\n" + serveKeysTestECDSALine, "control byte 0x0a"},
		{"carriage return inside", serveKeysTestEd25519Canonical + "\rx", "control byte 0x0d"},
		{"tab separator", "ssh-ed25519\t" + encode(ed25519Blob), "control byte 0x09"},
		{"NUL byte", serveKeysTestEd25519Canonical + " a\x00b", "control byte 0x00"},
		{"DEL byte", serveKeysTestEd25519Canonical + " a\x7fb", "control byte 0x7f"},
		{"too long", serveKeysTestEd25519Canonical + " " + strings.Repeat("c", servePublicKeyLineLimit), "longer than 16384 bytes"},
		{"unsupported ssh-dss", "ssh-dss " + encode(serveTestBlob("ssh-dss", "p", "q", "g", "y")) + " old", `unsupported SSH key type "ssh-dss"`},
		{"unsupported type after options", `command="a b" ssh-dss ` + encode(serveTestBlob("ssh-dss", "p")), `unsupported SSH key type "ssh-dss"`},
		{"certificate type", "ssh-ed25519-cert-v01@openssh.com " + encode(ed25519Blob), `unsupported SSH key type "ssh-ed25519-cert-v01@openssh.com"`},
		{"two option fields", "restrict no-pty " + serveKeysTestEd25519Canonical, "unsupported SSH key type"},
		{"unterminated quote", `command="a b ` + serveKeysTestEd25519Canonical, "unterminated double quote"},
		{"options only", `restrict,command="x y"`, "unsupported SSH key type"},
		{"type without blob", "ssh-ed25519", "no base64 key"},
		{"type with spaces only", "ssh-ed25519    ", "no base64 key"},
		{"bad base64", "ssh-ed25519 AAAA$$$$", "not valid padded base64"},
		{"missing padding", "ecdsa-sha2-nistp256 " + strings.TrimSuffix(strings.Fields(serveKeysTestECDSALine)[1], "="), "not valid padded base64"},
		{"non-canonical trailing bits", "ecdsa-sha2-nistp256 " + strings.TrimSuffix(strings.Fields(serveKeysTestECDSALine)[1], "o=") + "p=", "not valid padded base64"},
		{"type mismatch", "ssh-rsa " + encode(ed25519Blob), `does not match the type "ssh-ed25519"`},
		{"truncated blob", "ssh-ed25519 " + encode(ed25519Blob[:len(ed25519Blob)-1]), "truncated"},
		{"truncated length prefix", "ssh-ed25519 " + encode(append(serveTestBlob("ssh-ed25519"), 0, 0)), "truncated"},
		{"missing key field", "ssh-ed25519 " + encode(serveTestBlob("ssh-ed25519")), "1 of 2 fields"},
		{"trailing bytes", "ssh-ed25519 " + encode(append(append([]byte{}, ed25519Blob...), 0)), "1 trailing bytes"},
		{"trailing field", "ssh-ed25519 " + encode(append(append([]byte{}, ed25519Blob...), serveTestBlob("extra")...)), "trailing bytes"},
		{"length overruns", "ssh-ed25519 " + encode(append(serveTestBlob("ssh-ed25519"), 0xff, 0xff, 0xff, 0xff, 1)), "claims 4294967295 bytes"},
		{"overlong type length", "ssh-ed25519 " + encode([]byte{0, 0, 0, 200, 's'}), "claims 200 bytes"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			parsed, err := parseServePublicKey(testCase.line)
			if err == nil {
				t.Fatalf("parseServePublicKey(%q) = %+v, want an error containing %q", testCase.line, parsed, testCase.fragment)
			}
			requireServeErrorContains(t, err, testCase.fragment)
		})
	}
}

// T009: fingerprints.

func TestServeKeysFingerprintKnownAnswers(t *testing.T) {
	for line, want := range map[string]string{
		serveKeysTestEd25519Line: serveKeysTestEd25519Fingerprint,
		serveKeysTestECDSALine:   serveKeysTestECDSAFingerprint,
		serveKeysTestRSALine:     serveKeysTestRSAFingerprint,
	} {
		parsed, err := parseServePublicKey(line)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Fingerprint != want || serveFingerprint(parsed.Blob) != want {
			t.Fatalf("fingerprint of %.40s... = %s, want %s", line, parsed.Fingerprint, want)
		}
	}
}

func TestServeKeysFingerprintsPassRegistrySyntaxCheck(t *testing.T) {
	for index := 0; index < 256; index++ {
		key := newServeTestKey(t, "")
		parsed, err := parseServePublicKey(key.PublicLine)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Fingerprint != key.Fingerprint {
			t.Fatalf("fingerprint %s, want %s", parsed.Fingerprint, key.Fingerprint)
		}
		if !validServeFingerprint(parsed.Fingerprint) || len(parsed.Fingerprint) != len("SHA256:")+43 {
			t.Fatalf("serveFingerprint produced %q, which validServeFingerprint refuses", parsed.Fingerprint)
		}
	}
	empty := serveFingerprint(nil)
	if !validServeFingerprint(empty) {
		t.Fatalf("serveFingerprint(nil) = %q fails validServeFingerprint", empty)
	}
}

func TestServeKeysFingerprintMatchesSSHKeygen(t *testing.T) {
	sshKeygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen is not on PATH; skipping the cross-check against OpenSSH fingerprints")
	}
	directory := t.TempDir()
	keygenFingerprint := func(t *testing.T, path string) string {
		t.Helper()
		output, err := exec.Command(sshKeygen, "-l", "-E", "sha256", "-f", path).CombinedOutput()
		if err != nil {
			t.Fatalf("ssh-keygen -l -E sha256 -f %s: %v\n%s", path, err, output)
		}
		fields := strings.Fields(string(output))
		if len(fields) < 2 {
			t.Fatalf("ssh-keygen printed %q", output)
		}
		return fields[1]
	}
	compare := func(t *testing.T, name, line string) {
		t.Helper()
		path := filepath.Join(directory, name+".pub")
		writeServeTestFile(t, path, line+"\n", 0o600)
		parsed, err := parseServePublicKey(line)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if want := keygenFingerprint(t, path); parsed.Fingerprint != want {
			t.Fatalf("%s: serveFingerprint = %s, ssh-keygen prints %s", name, parsed.Fingerprint, want)
		}
	}

	key := newServeTestKey(t, "fresh@test")
	compare(t, "fresh-ed25519", key.PublicLine)
	path := filepath.Join(directory, "fresh-ed25519.pub")
	if want := keygenFingerprint(t, path); key.Fingerprint != want {
		t.Fatalf("newServeTestKey fingerprint %s, ssh-keygen prints %s", key.Fingerprint, want)
	}

	for _, generated := range []struct{ name, keyType, bits string }{
		{"ecdsa256", "ecdsa", "256"},
		{"ecdsa384", "ecdsa", "384"},
		{"ecdsa521", "ecdsa", "521"},
		{"rsa", "rsa", "2048"},
	} {
		private := filepath.Join(directory, generated.name)
		output, err := exec.Command(sshKeygen, "-q", "-t", generated.keyType, "-b", generated.bits, "-N", "", "-C", "generated", "-f", private).CombinedOutput()
		if err != nil {
			t.Fatalf("ssh-keygen -t %s -b %s: %v\n%s", generated.keyType, generated.bits, err, output)
		}
		public, err := os.ReadFile(private + ".pub")
		if err != nil {
			t.Fatal(err)
		}
		compare(t, generated.name+"-copy", string(public))
	}

	// Security-key types need hardware to generate, but ssh-keygen can
	// fingerprint their public halves; build them from real key material.
	edFields := serveTestBlobFields(t, serveKeysTestBlobOf(t, key.PublicLine))
	compare(t, "sk-ed25519", serveTestKeyLine(serveTestBlob("sk-ssh-ed25519@openssh.com", edFields[1], "ssh:")))
	ecFields := serveTestBlobFields(t, serveKeysTestBlobOf(t, serveKeysTestECDSALine))
	compare(t, "sk-ecdsa", serveTestKeyLine(serveTestBlob("sk-ecdsa-sha2-nistp256@openssh.com", ecFields[1], ecFields[2], "ssh:")))
}

// T010: managed block rendering.

func TestServeKeysRenderMatchesContractExample(t *testing.T) {
	binding := serveKeysTestBinding(serveKeysTestEd25519Line, serveKeysTestEd25519Fingerprint, serveKeysTestContractActor)
	block, err := renderServeAuthorizedKeysBlock([]serveKeyBinding{binding}, serveKeysTestHNPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "# BEGIN hn serve managed keys; edit with hn serve key\n" +
		`restrict,command="/usr/local/bin/hn serve git --key SHA256:Mnm7DCteTtegvPg/NcGZVvsMTFM5gR+dbIBbGci5vSg" ` +
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICqPfpPz/R1uz/vd9nUpjEhAMWYVmeerTHMSJDGYgg3A hn-serve:4f1c9a6d0d2b\n" +
		"# END hn serve managed keys\n"
	if string(block) != want {
		t.Fatalf("block:\n%s\nwant:\n%s", block, want)
	}
}

func TestServeKeysRenderSortsBindingsAndRendersEmptyRegistry(t *testing.T) {
	empty, err := renderServeAuthorizedKeysBlock(nil, serveKeysTestHNPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := serveAuthorizedKeysBegin + "\n" + serveAuthorizedKeysEnd + "\n"; string(empty) != want {
		t.Fatalf("empty block = %q, want %q", empty, want)
	}

	bindings := []serveKeyBinding{
		serveKeysTestBinding(serveKeysTestRSALine, serveKeysTestRSAFingerprint, serveStateTestActor("rsa")),
		serveKeysTestBinding(serveKeysTestEd25519Line, serveKeysTestEd25519Fingerprint, serveStateTestActor("ed")),
		serveKeysTestBinding(serveKeysTestECDSALine, serveKeysTestECDSAFingerprint, serveStateTestActor("ed")),
	}
	block, err := renderServeAuthorizedKeysBlock(bindings, "/home/hn/bin/hn")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(block), "\n"), "\n")
	if len(lines) != 5 || lines[0] != serveAuthorizedKeysBegin || lines[4] != serveAuthorizedKeysEnd {
		t.Fatalf("block has unexpected framing:\n%s", block)
	}
	// Byte order: "SHA256:M..." < "SHA256:T..." < "SHA256:a...".
	for index, fingerprint := range []string{serveKeysTestEd25519Fingerprint, serveKeysTestRSAFingerprint, serveKeysTestECDSAFingerprint} {
		want := `restrict,command="/home/hn/bin/hn serve git --key ` + fingerprint + `" `
		if !strings.HasPrefix(lines[index+1], want) {
			t.Fatalf("line %d = %q, want prefix %q (sorted by fingerprint)", index+1, lines[index+1], want)
		}
	}
	if bindings[0].Fingerprint != serveKeysTestRSAFingerprint {
		t.Fatal("renderServeAuthorizedKeysBlock reordered its input slice")
	}
}

func TestServeKeysRenderNeverCopiesOptionsOrComments(t *testing.T) {
	binding := serveKeysTestBinding(serveKeysTestEd25519Line, serveKeysTestEd25519Fingerprint, serveKeysTestContractActor)
	binding.PublicKey = `command="/bin/sh",no-restrict ` + serveKeysTestEd25519Canonical + " evil comment"
	block, err := renderServeAuthorizedKeysBlock([]serveKeyBinding{binding}, serveKeysTestHNPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(block), "/bin/sh") || strings.Contains(string(block), "evil") || strings.Contains(string(block), "no-restrict") {
		t.Fatalf("block copied options or comment from the stored key:\n%s", block)
	}
}

func TestServeKeysRenderRefusesUnsafeCommandPath(t *testing.T) {
	binding := serveKeysTestBinding(serveKeysTestEd25519Line, serveKeysTestEd25519Fingerprint, serveKeysTestContractActor)
	for _, path := range []string{
		"hn",
		"bin/hn",
		"",
		`/usr/local/bin/h"n`,
		`/usr/local/bin/h\n`,
		"/usr/local/bin/h'n",
		"/usr/local/my bin/hn",
		"/usr/local/bin/h\tn",
		"/usr/local/bin/hn\n",
	} {
		if block, err := renderServeAuthorizedKeysBlock([]serveKeyBinding{binding}, path); err == nil {
			t.Fatalf("renderServeAuthorizedKeysBlock accepted hn path %q:\n%s", path, block)
		}
	}
}

func TestServeKeysRenderRefusesTamperedRegistry(t *testing.T) {
	valid := serveKeysTestBinding(serveKeysTestEd25519Line, serveKeysTestEd25519Fingerprint, serveKeysTestContractActor)
	other := newServeTestKey(t, "")
	cases := []struct {
		name     string
		mutate   func(binding *serveKeyBinding)
		fragment string
	}{
		{"public key replaced", func(b *serveKeyBinding) {
			b.PublicKey = strings.Fields(other.PublicLine)[0] + " " + strings.Fields(other.PublicLine)[1]
		}, "does not match its public key, which has fingerprint " + other.Fingerprint},
		{"fingerprint replaced", func(b *serveKeyBinding) { b.Fingerprint = serveKeysTestECDSAFingerprint }, "does not match its public key"},
		{"public key malformed", func(b *serveKeyBinding) { b.PublicKey = "ssh-ed25519 AAAA" }, "invalid public key"},
		{"public key unsupported", func(b *serveKeyBinding) { b.PublicKey = "ssh-dss AAAA" }, "invalid public key"},
		{"actor short", func(b *serveKeyBinding) { b.Actor = serveKeysTestContractActor[:12] }, "malformed actor"},
		{"actor with newline", func(b *serveKeyBinding) { b.Actor = serveKeysTestContractActor[:63] + "\n" }, "malformed actor"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			binding := valid
			testCase.mutate(&binding)
			block, err := renderServeAuthorizedKeysBlock([]serveKeyBinding{binding}, serveKeysTestHNPath)
			if err == nil {
				t.Fatalf("rendered a tampered binding:\n%s", block)
			}
			requireServeErrorContains(t, err, testCase.fragment)
		})
	}
	if _, err := renderServeAuthorizedKeysBlock([]serveKeyBinding{valid, valid}, serveKeysTestHNPath); err == nil {
		t.Fatal("rendered a registry that binds one fingerprint twice")
	} else {
		requireServeErrorContains(t, err, "more than once")
	}
}

// T011: merging the managed block.

const (
	serveKeysTestAdminA = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAdminAAdminAAdminAAdminAAdminAAdminAAdmin admin-a"
	serveKeysTestAdminB = `from="10.0.0.0/8" ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAdminBAdminBAdminBAdminBAdminBAdminBAdmin admin-b`
)

func serveKeysTestBlock(t *testing.T, keys ...serveTestKey) []byte {
	t.Helper()
	bindings := make([]serveKeyBinding, 0, len(keys))
	for index, key := range keys {
		bindings = append(bindings, serveKeysTestBinding(key.PublicLine, key.Fingerprint, serveStateTestActor(string(rune('a'+index)))))
	}
	block, err := renderServeAuthorizedKeysBlock(bindings, serveKeysTestHNPath)
	if err != nil {
		t.Fatal(err)
	}
	return block
}

func TestServeKeysMergeGoldenCases(t *testing.T) {
	block := serveKeysTestBlock(t, newServeTestKey(t, ""), newServeTestKey(t, ""))
	emptyBlock := serveKeysTestBlock(t)
	oldBlock := string(serveKeysTestBlock(t, newServeTestKey(t, "")))
	oldBlockCRLF := strings.ReplaceAll(oldBlock, "\n", "\r\n")
	oldBlockNoFinalNewline := strings.TrimSuffix(oldBlock, "\n")

	cases := []struct {
		name          string
		before, old   string // existing = before + old + after
		after         string
		replacement   []byte
		appendNewline bool // the contract's one permitted addition
	}{
		{name: "empty file", replacement: block},
		{name: "two admin keys without final newline", before: serveKeysTestAdminA + "\n" + serveKeysTestAdminB, replacement: block, appendNewline: true},
		{name: "two admin keys with final newline", before: serveKeysTestAdminA + "\n" + serveKeysTestAdminB + "\n", replacement: block},
		{name: "admin key ending in CR only", before: serveKeysTestAdminA + "\r", replacement: block, appendNewline: true},
		{name: "CRLF admin lines around an existing block", before: serveKeysTestAdminA + "\r\n# admin comment\r\n", old: oldBlock, after: serveKeysTestAdminB + "\r\n\r\n", replacement: block},
		{name: "CRLF existing block", before: serveKeysTestAdminA + "\r\n", old: oldBlockCRLF, after: serveKeysTestAdminB, replacement: block},
		{name: "block in the middle", before: serveKeysTestAdminA + "\n", old: oldBlock, after: serveKeysTestAdminB + "\n", replacement: block},
		{name: "block at the end", before: serveKeysTestAdminA + "\n" + serveKeysTestAdminB + "\n", old: oldBlock, replacement: block},
		{name: "block at the end without final newline", before: serveKeysTestAdminA + "\n", old: oldBlockNoFinalNewline, replacement: block},
		{name: "block at the start", old: oldBlock, after: serveKeysTestAdminA, replacement: block},
		{name: "block replaced by an empty block", before: serveKeysTestAdminA + "\n", old: oldBlock, after: serveKeysTestAdminB + "\n", replacement: emptyBlock},
		{name: "empty block replaced", before: "# keys\n", old: string(emptyBlock), after: "\n", replacement: block},
		{name: "marker text that is not a whole line", before: "# note: " + serveAuthorizedKeysBegin + "\n" + serveAuthorizedKeysEnd + " trailing\n", replacement: block},
		{name: "marker with leading space is not a marker", before: " " + serveAuthorizedKeysBegin + "\n", replacement: block},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			existing := []byte(testCase.before + testCase.old + testCase.after)
			original := bytes.Clone(existing)
			merged, err := mergeServeAuthorizedKeys(existing, testCase.replacement)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(existing, original) {
				t.Fatal("mergeServeAuthorizedKeys modified its input")
			}
			separator := ""
			if testCase.appendNewline {
				separator = "\n"
			}
			want := testCase.before + separator + string(testCase.replacement) + testCase.after
			if string(merged) != want {
				t.Fatalf("merged = %q, want %q", merged, want)
			}
			requireServeKeysOutsidePreserved(t, merged, []byte(testCase.before+separator), []byte(testCase.after))
			again, err := mergeServeAuthorizedKeys(merged, testCase.replacement)
			if err != nil || !bytes.Equal(again, merged) {
				t.Fatalf("merging the same block again changed the file: %q (%v)", again, err)
			}
		})
	}
}

// requireServeKeysOutsidePreserved checks the property that bytes before the
// begin marker and after the end marker are exactly before and after.
func requireServeKeysOutsidePreserved(t *testing.T, merged, before, after []byte) {
	t.Helper()
	if !bytes.HasPrefix(merged, before) || !bytes.HasSuffix(merged, after) || len(merged) < len(before)+len(after) {
		t.Fatalf("outside bytes changed: merged %q, want prefix %q and suffix %q", merged, before, after)
	}
	inside := merged[len(before) : len(merged)-len(after)]
	if !bytes.HasPrefix(inside, []byte(serveAuthorizedKeysBegin+"\n")) || !bytes.HasSuffix(inside, []byte(serveAuthorizedKeysEnd+"\n")) {
		t.Fatalf("bytes between the outside parts are not exactly one managed block: %q", inside)
	}
}

func TestServeKeysMergePreservesRandomOutsideLines(t *testing.T) {
	block := serveKeysTestBlock(t, newServeTestKey(t, ""))
	oldBlock := serveKeysTestBlock(t, newServeTestKey(t, ""), newServeTestKey(t, ""))
	pieces := []string{serveKeysTestAdminA, serveKeysTestAdminB, "", "# comment", " ", "\r", "x\r"}
	terminators := []string{"\n", "\r\n"}
	random := func(limit int64) int {
		value, err := rand.Int(rand.Reader, big.NewInt(limit))
		if err != nil {
			t.Fatal(err)
		}
		return int(value.Int64())
	}
	outside := func(final bool) string {
		var text strings.Builder
		for count := random(4); count > 0; count-- {
			text.WriteString(pieces[random(int64(len(pieces)))])
			text.WriteString(terminators[random(int64(len(terminators)))])
		}
		if final && random(2) == 0 {
			text.WriteString(pieces[random(int64(len(pieces)))])
		}
		return text.String()
	}
	for iteration := 0; iteration < 500; iteration++ {
		before := outside(false)
		after := outside(true)
		existing := before + string(oldBlock) + after
		merged, err := mergeServeAuthorizedKeys([]byte(existing), block)
		if err != nil {
			t.Fatalf("merge %q: %v", existing, err)
		}
		requireServeKeysOutsidePreserved(t, merged, []byte(before), []byte(after))

		withoutBlock := before + after
		merged, err = mergeServeAuthorizedKeys([]byte(withoutBlock), block)
		if err != nil {
			t.Fatalf("merge %q: %v", withoutBlock, err)
		}
		prefix := withoutBlock
		if prefix != "" && !strings.HasSuffix(prefix, "\n") {
			prefix += "\n"
		}
		requireServeKeysOutsidePreserved(t, merged, []byte(prefix), nil)
	}
}

func TestServeKeysMergeRefusesMarkerDefects(t *testing.T) {
	block := serveKeysTestBlock(t)
	begin := serveAuthorizedKeysBegin + "\n"
	end := serveAuthorizedKeysEnd + "\n"
	cases := []struct {
		name, existing, fragment string
	}{
		{"two begin markers", begin + begin + end, "line 2 is a second managed block begin marker"},
		{"two blocks", begin + end + "admin\n" + begin + end, "line 4 is a second managed block begin marker"},
		{"two end markers", "admin\n" + begin + end + end, "line 4 is a second managed block end marker"},
		{"end without begin", "admin\n" + end, "line 2 is a managed block end marker without a preceding begin marker"},
		{"end before begin", end + begin, "line 1 is a managed block end marker without a preceding begin marker"},
		{"begin without end", "admin\n" + begin + "restrict ssh-ed25519 AAAA\n", "begin marker has no following end marker"},
		{"begin at end of file without newline", "admin\n" + serveAuthorizedKeysBegin, "begin marker has no following end marker"},
		{"CRLF begin without end", "admin\r\n" + serveAuthorizedKeysBegin + "\r\n", "begin marker has no following end marker"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			merged, err := mergeServeAuthorizedKeys([]byte(testCase.existing), block)
			if err == nil {
				t.Fatalf("merged a defective file into %q", merged)
			}
			requireServeErrorContains(t, err, testCase.fragment)
		})
	}
}

// T012: writing $HOME/.ssh/authorized_keys.

func newServeKeysTestLayout(t *testing.T) serveLayout {
	t.Helper()
	serveTestHome(t)
	layout, err := resolveServeLayout()
	if err != nil {
		t.Fatal(err)
	}
	return layout
}

func serveKeysTestBindings(t *testing.T) []serveKeyBinding {
	t.Helper()
	key := newServeTestKey(t, "")
	return []serveKeyBinding{serveKeysTestBinding(key.PublicLine, key.Fingerprint, serveKeysTestContractActor)}
}

func requireServeKeysMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != want {
		t.Fatalf("%s mode = %v, want %04o", path, info.Mode(), want)
	}
}

func TestServeKeysWriteCreatesPrivateSSHFiles(t *testing.T) {
	layout := newServeKeysTestLayout(t)
	bindings := serveKeysTestBindings(t)
	if err := writeServeAuthorizedKeys(layout, bindings, serveKeysTestHNPath); err != nil {
		t.Fatal(err)
	}
	requireServeKeysMode(t, layout.SSHDir, 0o700)
	requireServeKeysMode(t, layout.AuthorizedKeys, 0o600)
	contents, err := os.ReadFile(layout.AuthorizedKeys)
	if err != nil {
		t.Fatal(err)
	}
	block, err := renderServeAuthorizedKeysBlock(bindings, serveKeysTestHNPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(contents, block) {
		t.Fatalf("authorized_keys = %q, want %q", contents, block)
	}
	entries, err := os.ReadDir(layout.SSHDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "authorized_keys" {
		t.Fatalf(".ssh holds %v (%v), want only authorized_keys", entries, err)
	}
	if entries, err := os.ReadDir(layout.Home); err != nil || len(entries) != 1 {
		t.Fatalf("home holds %v (%v), want only .ssh", entries, err)
	}
}

func TestServeKeysWritePreservesAdministratorLines(t *testing.T) {
	layout := newServeKeysTestLayout(t)
	mkdirServeTest(t, layout.SSHDir, 0o755)
	before := serveKeysTestAdminA + "\r\n"
	after := "# trailing admin comment\n" + serveKeysTestAdminB
	oldBlock := serveKeysTestBlock(t, newServeTestKey(t, ""))
	writeServeTestFile(t, layout.AuthorizedKeys, before+string(oldBlock)+after, 0o644)

	bindings := serveKeysTestBindings(t)
	if err := writeServeAuthorizedKeys(layout, bindings, serveKeysTestHNPath); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(layout.AuthorizedKeys)
	if err != nil {
		t.Fatal(err)
	}
	block, err := renderServeAuthorizedKeysBlock(bindings, serveKeysTestHNPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := before + string(block) + after; string(contents) != want {
		t.Fatalf("authorized_keys = %q, want %q", contents, want)
	}
	requireServeKeysMode(t, layout.AuthorizedKeys, 0o600)
	// An existing .ssh that sshd accepts is left as the administrator set it.
	requireServeKeysMode(t, layout.SSHDir, 0o755)
}

// serveKeysTestSnapshot records a path's type, mode and contents so a test
// can prove a refused write changed nothing.
type serveKeysTestSnapshot struct {
	exists   bool
	mode     os.FileMode
	contents string
	link     string
	entries  string
}

func snapshotServeKeysTest(t *testing.T, path string) serveKeysTestSnapshot {
	t.Helper()
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return serveKeysTestSnapshot{}
	}
	if err != nil {
		t.Fatal(err)
	}
	snapshot := serveKeysTestSnapshot{exists: true, mode: info.Mode()}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		if snapshot.link, err = os.Readlink(path); err != nil {
			t.Fatal(err)
		}
	case info.IsDir():
		entries, err := os.ReadDir(path)
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		snapshot.entries = strings.Join(names, "/")
	default:
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		snapshot.contents = string(contents)
	}
	return snapshot
}

func TestServeKeysWriteRefusesUnsafeTargets(t *testing.T) {
	const adminContents = serveKeysTestAdminA + "\n"
	cases := []struct {
		name     string
		setup    func(t *testing.T, layout serveLayout) []string // paths that must stay unchanged
		hnPath   string
		tamper   bool
		path     func(layout serveLayout) string
		fragment string
	}{
		{
			name: "symlinked authorized_keys",
			setup: func(t *testing.T, layout serveLayout) []string {
				target := filepath.Join(t.TempDir(), "elsewhere")
				writeServeTestFile(t, target, adminContents, 0o600)
				mkdirServeTest(t, layout.SSHDir, 0o700)
				symlinkServeTest(t, target, layout.AuthorizedKeys)
				return []string{target, layout.AuthorizedKeys, layout.SSHDir}
			},
			path:     func(layout serveLayout) string { return layout.AuthorizedKeys },
			fragment: "is a symbolic link",
		},
		{
			name: "symlinked .ssh",
			setup: func(t *testing.T, layout serveLayout) []string {
				target := t.TempDir()
				symlinkServeTest(t, target, layout.SSHDir)
				return []string{target, layout.SSHDir}
			},
			path:     func(layout serveLayout) string { return layout.SSHDir },
			fragment: "is a symbolic link",
		},
		{
			name: "group-writable authorized_keys",
			setup: func(t *testing.T, layout serveLayout) []string {
				mkdirServeTest(t, layout.SSHDir, 0o700)
				writeServeTestFile(t, layout.AuthorizedKeys, adminContents, 0o664)
				return []string{layout.AuthorizedKeys, layout.SSHDir}
			},
			path:     func(layout serveLayout) string { return layout.AuthorizedKeys },
			fragment: "file mode 0664 is writable by group or others; run chmod 600",
		},
		{
			name: "other-writable authorized_keys",
			setup: func(t *testing.T, layout serveLayout) []string {
				mkdirServeTest(t, layout.SSHDir, 0o700)
				writeServeTestFile(t, layout.AuthorizedKeys, adminContents, 0o602)
				return []string{layout.AuthorizedKeys, layout.SSHDir}
			},
			path:     func(layout serveLayout) string { return layout.AuthorizedKeys },
			fragment: "file mode 0602 is writable by group or others",
		},
		{
			name: "group-writable .ssh",
			setup: func(t *testing.T, layout serveLayout) []string {
				mkdirServeTest(t, layout.SSHDir, 0o775)
				writeServeTestFile(t, layout.AuthorizedKeys, adminContents, 0o600)
				return []string{layout.AuthorizedKeys, layout.SSHDir}
			},
			path:     func(layout serveLayout) string { return layout.SSHDir },
			fragment: "directory mode 0775 is writable by group or others; run chmod 700",
		},
		{
			name: ".ssh is a file",
			setup: func(t *testing.T, layout serveLayout) []string {
				writeServeTestFile(t, layout.SSHDir, "not a directory\n", 0o600)
				return []string{layout.SSHDir}
			},
			path:     func(layout serveLayout) string { return layout.SSHDir },
			fragment: "is not a directory",
		},
		{
			name: "authorized_keys is a directory",
			setup: func(t *testing.T, layout serveLayout) []string {
				mkdirServeTest(t, layout.SSHDir, 0o700)
				mkdirServeTest(t, layout.AuthorizedKeys, 0o700)
				return []string{layout.AuthorizedKeys, layout.SSHDir}
			},
			path:     func(layout serveLayout) string { return layout.AuthorizedKeys },
			fragment: "is not a regular file",
		},
		{
			name: "authorized_keys too large",
			setup: func(t *testing.T, layout serveLayout) []string {
				mkdirServeTest(t, layout.SSHDir, 0o700)
				writeServeTestFile(t, layout.AuthorizedKeys, strings.Repeat("#\n", serveAuthorizedKeysLimit/2+1), 0o600)
				return []string{layout.AuthorizedKeys, layout.SSHDir}
			},
			path:     func(layout serveLayout) string { return layout.AuthorizedKeys },
			fragment: "larger than 4194304 bytes",
		},
		{
			name: "duplicate begin marker",
			setup: func(t *testing.T, layout serveLayout) []string {
				mkdirServeTest(t, layout.SSHDir, 0o700)
				writeServeTestFile(t, layout.AuthorizedKeys, adminContents+serveAuthorizedKeysBegin+"\n"+serveAuthorizedKeysBegin+"\n"+serveAuthorizedKeysEnd+"\n", 0o600)
				return []string{layout.AuthorizedKeys, layout.SSHDir}
			},
			path:     func(layout serveLayout) string { return layout.AuthorizedKeys },
			fragment: "second managed block begin marker",
		},
		{
			name: "unsafe hn path leaves a missing .ssh missing",
			setup: func(t *testing.T, layout serveLayout) []string {
				return []string{layout.SSHDir}
			},
			hnPath:   "/opt/my hn/hn",
			fragment: "/opt/my hn/hn",
		},
		{
			name: "tampered registry leaves an existing file unchanged",
			setup: func(t *testing.T, layout serveLayout) []string {
				mkdirServeTest(t, layout.SSHDir, 0o700)
				writeServeTestFile(t, layout.AuthorizedKeys, adminContents, 0o600)
				return []string{layout.AuthorizedKeys, layout.SSHDir}
			},
			tamper:   true,
			fragment: "does not match its public key",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			layout := newServeKeysTestLayout(t)
			watched := testCase.setup(t, layout)
			snapshots := make([]serveKeysTestSnapshot, len(watched))
			for index, path := range watched {
				snapshots[index] = snapshotServeKeysTest(t, path)
			}
			bindings := serveKeysTestBindings(t)
			if testCase.tamper {
				bindings[0].PublicKey = serveKeysTestEd25519Canonical
			}
			hnPath := testCase.hnPath
			if hnPath == "" {
				hnPath = serveKeysTestHNPath
			}
			err := writeServeAuthorizedKeys(layout, bindings, hnPath)
			if err == nil {
				t.Fatal("writeServeAuthorizedKeys succeeded, want a refusal")
			}
			if testCase.path != nil {
				requireServeDefect(t, err, testCase.path(layout), testCase.fragment)
			} else {
				requireServeErrorContains(t, err, testCase.fragment)
			}
			for index, path := range watched {
				if got := snapshotServeKeysTest(t, path); got != snapshots[index] {
					t.Fatalf("%s changed after a refused write: %+v, was %+v", path, got, snapshots[index])
				}
			}
		})
	}
}

// T013: the guide documents the implemented format.

func TestServeKeysGuideMatchesImplementedFormat(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("docs", "serve-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	guide := string(contents)
	start := strings.Index(guide, "\n## Key bindings and authorized_keys\n")
	stop := strings.Index(guide, "\n## Administering hosted repositories\n")
	if start < 0 || stop < start {
		t.Fatal("guide has no Key bindings and authorized_keys section before Administering hosted repositories")
	}
	section := guide[start:stop]
	binding := serveKeysTestBinding(serveKeysTestEd25519Line, serveKeysTestEd25519Fingerprint, serveKeysTestContractActor)
	block, err := renderServeAuthorizedKeysBlock([]serveKeyBinding{binding}, serveKeysTestHNPath)
	if err != nil {
		t.Fatal(err)
	}
	required := []string{"```text\n" + string(block) + "```", "ssh-keygen -l -E sha256", serveKeysTestEd25519Fingerprint}
	for keyType := range serveKeyTypeFields {
		required = append(required, "`"+keyType+"`")
	}
	for _, fragment := range required {
		if !strings.Contains(section, fragment) {
			t.Fatalf("guide section does not contain %q", fragment)
		}
	}
}
