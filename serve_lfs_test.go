package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// WP14: Git LFS transfer and quota (FR-020, FR-021, US5). The round trips
// use the real Git LFS client through the SSH shim; protocol edges are
// driven with hand-written pkt-lines against hn serve git directly, the way
// sshd runs the forced command.

// requireServeLFSClient skips the test with the reason when the Git LFS
// client is not installed.
func requireServeLFSClient(t *testing.T) {
	t.Helper()
	if output, err := exec.Command("git", "lfs", "version").CombinedOutput(); err != nil {
		t.Skipf("Git LFS client unavailable (git lfs version: %v): %s", err, bytes.TrimSpace(output))
	}
}

// serveLFSFixture is a server with one hosted repository r and two bound
// participant keys, A and B.
type serveLFSFixture struct {
	t       *testing.T
	h       *serveHarness
	actors  map[string]string
	keys    map[string]serveTestKey
	clients map[string]serveHarnessClientEnv
}

// newServeLFSFixture creates r and binds A and B on h. Set h.Wrap and h.Env
// before calling it so the shims carry them.
func newServeLFSFixture(t *testing.T, h *serveHarness) *serveLFSFixture {
	t.Helper()
	h.Serve("serve", "repo", "create", "r", "--from", serveHarnessSourceRepo(t))
	f := &serveLFSFixture{
		t:       t,
		h:       h,
		actors:  map[string]string{},
		keys:    map[string]serveTestKey{},
		clients: map[string]serveHarnessClientEnv{},
	}
	for _, name := range []string{"A", "B"} {
		f.actors[name] = serveHarnessActor("lfs-" + name)
		f.keys[name] = serveHarnessAddKey(h, f.actors[name])
		f.clients[name] = serveHarnessClient(t, h, f.keys[name], nil)
	}
	return f
}

// cloneWithLFS clones r for name with Git LFS configured in the clone only.
// The shim does not support SSH multiplexing, so automultiplex is off
// (plan risk IC-07).
func (f *serveLFSFixture) cloneWithLFS(name string, args ...string) string {
	f.t.Helper()
	work := filepath.Join(f.t.TempDir(), "work-"+name)
	client := f.clients[name]
	client.Git("", append(append([]string{"clone", "-q"}, args...), serveHarnessCloneURL("r"), work)...)
	client.Git(work, "lfs", "install", "--local")
	client.Git(work, "config", "lfs.ssh.automultiplex", "false")
	return work
}

// lfsStored reads the lfs-stored: line of hn serve repo show r.
func (f *serveLFSFixture) lfsStored() int64 {
	f.t.Helper()
	output := f.h.Serve("serve", "repo", "show", "r")
	for _, line := range strings.Split(output, "\n") {
		if value, ok := strings.CutPrefix(line, "lfs-stored: "); ok {
			stored, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				f.t.Fatalf("lfs-stored line %q: %v", line, err)
			}
			return stored
		}
	}
	f.t.Fatalf("hn serve repo show printed no lfs-stored line:\n%s", output)
	return 0
}

// serveLFSRandom returns size random bytes.
func serveLFSRandom(t *testing.T, size int) []byte {
	t.Helper()
	content := make([]byte, size)
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	return content
}

func serveLFSOID(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// Test-side pkt-line encoding, written independently of serve_lfs.go so the
// tests check the wire format itself.

func serveLFSWireText(line string) []byte {
	return []byte(fmt.Sprintf("%04x%s\n", len(line)+5, line))
}

// serveLFSWireData encodes content as data packets of 32 KiB, the size the
// Git LFS client sends. It never returns nil, so an empty object still gets
// its delim-pkt in serveLFSRequestBytes.
func serveLFSWireData(content []byte) []byte {
	wire := []byte{}
	for len(content) > 0 {
		chunk := content[:min(len(content), 32768)]
		wire = append(wire, fmt.Sprintf("%04x", len(chunk)+4)...)
		wire = append(wire, chunk...)
		content = content[len(chunk):]
	}
	return wire
}

const (
	serveLFSWireFlush = "0000"
	serveLFSWireDelim = "0001"
)

// serveLFSRequestBytes is one request: the command, its arguments, and
// when body is not nil a delim-pkt and the body, then a flush-pkt.
func serveLFSRequestBytes(command string, args []string, body []byte) []byte {
	var wire bytes.Buffer
	wire.Write(serveLFSWireText(command))
	for _, arg := range args {
		wire.Write(serveLFSWireText(arg))
	}
	if body != nil {
		wire.WriteString(serveLFSWireDelim)
		wire.Write(body)
	}
	wire.WriteString(serveLFSWireFlush)
	return wire.Bytes()
}

func serveLFSPutObject(oid string, size int, content []byte) []byte {
	return serveLFSRequestBytes("put-object "+oid, []string{"size=" + strconv.Itoa(size)}, serveLFSWireData(content))
}

func serveLFSBatch(objects ...string) []byte {
	body := []byte{}
	for _, object := range objects {
		body = append(body, serveLFSWireText(object)...)
	}
	return serveLFSRequestBytes("batch", []string{"transfer=ssh", "hash-algo=sha256", "refname=refs/heads/main"}, body)
}

// serveLFSResponse is one server message up to its flush-pkt.
type serveLFSResponse struct {
	Status int
	Args   []string
	Lines  []string
	Data   []byte
	Delim  bool
}

// serveLFSConn is one live hn serve git session speaking git-lfs-transfer.
type serveLFSConn struct {
	t      *testing.T
	cancel context.CancelFunc
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *bytes.Buffer
}

// startServeLFSConn starts hn serve git --key for key's fingerprint with
// SSH_ORIGINAL_COMMAND set to request, the way sshd would.
func startServeLFSConn(t *testing.T, h *serveHarness, fingerprint, request string) *serveLFSConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	program, arguments := h.Binary, []string{"serve", "git", "--key", fingerprint}
	if len(h.Wrap) > 0 {
		program = h.Wrap[0]
		arguments = append(append(append([]string{}, h.Wrap[1:]...), h.Binary), arguments...)
	}
	command := exec.CommandContext(ctx, program, arguments...)
	command.Dir = h.Home
	command.Env = serveHarnessEnvironment(h.Home, serveHarnessMergeEnv(h.Env, map[string]string{"SSH_ORIGINAL_COMMAND": request}))
	stdin, err := command.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	stderr := &bytes.Buffer{}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	conn := &serveLFSConn{t: t, cancel: cancel, cmd: command, stdin: stdin, stdout: bufio.NewReader(stdout), stderr: stderr}
	t.Cleanup(func() {
		cancel()
		_ = command.Wait()
	})
	return conn
}

// openServeLFS starts an LFS session for r and checks the capability
// advertisement and the version handshake.
func openServeLFS(t *testing.T, h *serveHarness, key serveTestKey, operation string) *serveLFSConn {
	t.Helper()
	conn := startServeLFSConn(t, h, key.Fingerprint, "git-lfs-transfer 'r.git' "+operation)
	if capabilities := conn.readPackets(); !equalStrings(capabilities, []string{"version=1"}) {
		t.Fatalf("capability advertisement = %q, want exactly version=1", capabilities)
	}
	conn.send(serveLFSRequestBytes("version 1", nil, nil))
	if response := conn.read(); response.Status != 200 {
		t.Fatalf("version 1 answered %+v", response)
	}
	return conn
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func (c *serveLFSConn) send(wire ...[]byte) {
	c.t.Helper()
	for _, chunk := range wire {
		if _, err := c.stdin.Write(chunk); err != nil {
			c.t.Fatalf("write to hn serve git: %v\nstderr %s", err, c.stderr)
		}
	}
}

// packet reads one packet: length 0 is flush, 1 is delim.
func (c *serveLFSConn) packet() (int, []byte) {
	c.t.Helper()
	var header [4]byte
	if _, err := io.ReadFull(c.stdout, header[:]); err != nil {
		c.t.Fatalf("read pkt-line header: %v\nstderr %s", err, c.stderr)
	}
	length, err := strconv.ParseUint(string(header[:]), 16, 16)
	if err != nil {
		c.t.Fatalf("pkt-line header %q: %v", header, err)
	}
	if length < 4 {
		return int(length), nil
	}
	if length > 65519 {
		c.t.Fatalf("server sent a pkt-line of length %d, above the proposal's 65519", length)
	}
	payload := make([]byte, length-4)
	if _, err := io.ReadFull(c.stdout, payload); err != nil {
		c.t.Fatalf("read pkt-line payload: %v", err)
	}
	return int(length), payload
}

// readPackets reads text packets up to a flush-pkt.
func (c *serveLFSConn) readPackets() []string {
	c.t.Helper()
	var lines []string
	for {
		length, payload := c.packet()
		switch length {
		case 0:
			return lines
		case 1:
			c.t.Fatalf("unexpected delim packet")
		}
		lines = append(lines, strings.TrimSuffix(string(payload), "\n"))
	}
}

// read reads one response: a status line, arguments and, after a
// delim-pkt, text lines (also kept as raw data) up to the flush-pkt.
func (c *serveLFSConn) read() serveLFSResponse {
	c.t.Helper()
	var response serveLFSResponse
	length, payload := c.packet()
	status, ok := strings.CutPrefix(strings.TrimSuffix(string(payload), "\n"), "status ")
	if length < 4 || !ok {
		c.t.Fatalf("response does not start with a status line: length %d payload %q", length, payload)
	}
	code, err := strconv.Atoi(status)
	if err != nil {
		c.t.Fatalf("status %q: %v", status, err)
	}
	response.Status = code
	for {
		length, payload := c.packet()
		switch {
		case length == 0:
			return response
		case length == 1 && response.Delim:
			c.t.Fatalf("second delim packet in one response")
		case length == 1:
			response.Delim = true
		case response.Delim:
			response.Data = append(response.Data, payload...)
			response.Lines = append(response.Lines, strings.TrimSuffix(string(payload), "\n"))
		default:
			response.Args = append(response.Args, strings.TrimSuffix(string(payload), "\n"))
		}
	}
}

// finish closes stdin and returns the exit status and stdout left unread.
func (c *serveLFSConn) finish() (int, string) {
	c.t.Helper()
	_ = c.stdin.Close()
	rest, _ := io.ReadAll(c.stdout)
	err := c.cmd.Wait()
	c.cancel()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, string(rest)
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), string(rest)
	default:
		c.t.Fatalf("hn serve git: %v\nstderr %s", err, c.stderr)
		return 0, ""
	}
}

// quit sends quit, requires status 200 and exit 0.
func (c *serveLFSConn) quit() {
	c.t.Helper()
	c.send(serveLFSRequestBytes("quit", nil, nil))
	if response := c.read(); response.Status != 200 || response.Delim || len(response.Args) > 0 {
		c.t.Fatalf("quit answered %+v, want a bare status 200", response)
	}
	if status, rest := c.finish(); status != 0 || rest != "" {
		c.t.Fatalf("after quit: exit %d, trailing output %q, stderr %s", status, rest, c.stderr)
	}
}

// upload stores content in r through one upload session.
func (f *serveLFSFixture) upload(name string, content []byte) string {
	f.t.Helper()
	oid := serveLFSOID(content)
	conn := openServeLFS(f.t, f.h, f.keys[name], "upload")
	conn.send(serveLFSPutObject(oid, len(content), content))
	if response := conn.read(); response.Status != 200 {
		f.t.Fatalf("put-object answered %+v", response)
	}
	conn.quit()
	return oid
}

// serveLFSEntries lists every regular file below root, relative to it.
func serveLFSEntries(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return files
}

// US5-1 and FR-020: A pushes an LFS-tracked 200 KiB file through the shim,
// and a second clone with key B downloads byte-identical content.
func TestServeLFSRoundTrip(t *testing.T) {
	requireServeLFSClient(t)
	f := newServeLFSFixture(t, newServeHarness(t))
	content := serveLFSRandom(t, 200*1024)
	pushed := serveLFSPushTracked(f, "A", "big.bin", content)

	work := f.cloneWithLFS("B", "-b", "lfs-data")
	f.clients["B"].Git(work, "lfs", "pull")
	got, err := os.ReadFile(filepath.Join(work, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("second clone has %d bytes that differ from the %d pushed", len(got), len(content))
	}
	if stored := f.lfsStored(); stored != int64(len(content)) {
		t.Fatalf("lfs-stored = %d, want %d", stored, len(content))
	}
	// Git LFS 3.7.1 asks for the lock list before the push and, after the
	// 501 answer, turns lock verification off for the remote.
	if verify := f.clients["A"].Git(pushed, "config", "--get-regexp", `^lfs\..*\.locksverify$`); !strings.HasSuffix(strings.TrimSpace(verify), ".locksverify false") {
		t.Fatalf("the pushing clone's lock verification setting = %q, want false", verify)
	}
	oid := serveLFSOID(content)
	path := filepath.Join(f.h.Layout().RepoLFSObjects("r"), oid[0:2], oid[2:4], oid)
	if info, err := os.Lstat(path); err != nil || info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() {
		t.Fatalf("stored object %s: %v %v", path, info, err)
	}
	if entries := serveHarnessEntries(t, f.h.Layout().RepoLFSIncoming("r")); len(entries) != 0 {
		t.Fatalf("lfs/incoming holds %q after the round trip", entries)
	}
}

// serveLFSPushTracked clones r for name, tracks *.bin, commits file with
// content on branch lfs-data and pushes the branch and its LFS object.
func serveLFSPushTracked(f *serveLFSFixture, name, file string, content []byte) string {
	f.t.Helper()
	client := f.clients[name]
	work := f.cloneWithLFS(name)
	client.Git(work, "lfs", "track", "*.bin")
	if err := os.WriteFile(filepath.Join(work, file), content, 0o644); err != nil {
		f.t.Fatal(err)
	}
	client.Git(work, "add", ".gitattributes", file)
	client.Git(work, "commit", "-q", "-m", "add "+file)
	client.Git(work, "push", "-q", "origin", "HEAD:refs/heads/lfs-data")
	return work
}

// serveLFSRequireRejection parses the single message line of response and
// checks its status, code and actor.
func serveLFSRequireRejection(t *testing.T, label string, response serveLFSResponse, status int, code, actor string) serveRejection {
	t.Helper()
	if response.Status != status || len(response.Lines) != 1 {
		t.Fatalf("%s: response %+v, want status %d with one message line", label, response, status)
	}
	rejection, err := parseServeRejectionLine(response.Lines[0])
	if err != nil {
		t.Fatalf("%s: message %q does not parse: %v", label, response.Lines[0], err)
	}
	if rejection.Ref != "-" || rejection.Code != code || rejection.Actor != actor || rejection.Next != serveReasonDefaultNext(code) {
		t.Fatalf("%s: rejection %+v, want ref=- code=%s actor=%s", label, rejection, code, actor)
	}
	return rejection
}

// serveLFSGuideExample returns the lines of the text block that follows
// marker in the serve guide, which must contain the marker exactly once.
func serveLFSGuideExample(t *testing.T, marker string) []string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("docs", "serve-v0.md"))
	if err != nil {
		t.Fatal(err)
	}
	guide := string(contents)
	if strings.Count(guide, marker+"\n") != 1 {
		t.Fatalf("the serve guide must contain exactly one %q", marker)
	}
	_, after, _ := strings.Cut(guide, marker+"\n")
	_, after, found := strings.Cut(after, "```text\n")
	if !found {
		t.Fatalf("no text block after %s", marker)
	}
	block, _, found := strings.Cut(after, "```\n")
	if !found {
		t.Fatalf("unterminated example block after %s", marker)
	}
	return strings.Split(strings.TrimSuffix(block, "\n"), "\n")
}

// US5-2 and FR-020: an upload whose content does not hash to its OID, or
// whose length differs from its declared size, is discarded with an
// lfs-object-mismatch line and is never stored or downloadable. The line is
// the guide's lfs-object-mismatch example.
func TestServeLFSObjectMismatch(t *testing.T) {
	documented := serveLFSGuideExample(t, "<!-- example: lfs-object-mismatch -->")
	if len(documented) != 1 {
		t.Fatalf("the lfs-object-mismatch example has %d lines, want 1", len(documented))
	}
	documentedRejection, err := parseServeRejectionLine(documented[0])
	if err != nil || documentedRejection.Code != serveReasonLFSObjectMismatch || len(documentedRejection.IDs) != 1 {
		t.Fatalf("documented example %q: %+v %v", documented[0], documentedRejection, err)
	}
	f := newServeLFSFixture(t, newServeHarness(t))
	layout := f.h.Layout()
	declared := serveLFSRandom(t, 4096)
	oid := serveLFSOID(declared)
	conn := openServeLFS(t, f.h, f.keys["A"], "upload")
	for _, attempt := range []struct {
		name    string
		content []byte
	}{
		{"other content of the declared size", serveLFSRandom(t, len(declared))},
		{"declared content cut short", declared[:len(declared)-1]},
		{"declared content with extra bytes", append(append([]byte{}, declared...), serveLFSRandom(t, 70000)...)},
		{"empty content", nil},
	} {
		conn.send(serveLFSPutObject(oid, len(declared), attempt.content))
		response := conn.read()
		rejection := serveLFSRequireRejection(t, attempt.name, response, 422, serveReasonLFSObjectMismatch, f.actors["A"])
		if len(rejection.IDs) != 1 || rejection.IDs[0] != oid {
			t.Fatalf("%s: ids %q, want the declared OID", attempt.name, rejection.IDs)
		}
		want := "hn-serve/1 reject ref=- actor=" + f.actors["A"] + " code=lfs-object-mismatch ids=" + oid + ` next="retry the upload"`
		if response.Lines[0] != want {
			t.Fatalf("%s: message %q, want %q", attempt.name, response.Lines[0], want)
		}
		example := strings.NewReplacer(documentedRejection.Actor, f.actors["A"], documentedRejection.IDs[0], oid).Replace(documented[0])
		if response.Lines[0] != example {
			t.Fatalf("%s: message %q differs from the guide's example %q", attempt.name, response.Lines[0], example)
		}
		if staged := serveHarnessEntries(t, layout.RepoLFSIncoming("r")); len(staged) != 0 {
			t.Fatalf("%s: lfs/incoming holds %q after the rejection", attempt.name, staged)
		}
	}
	conn.send(serveLFSRequestBytes("verify-object "+oid, []string{"size=4096"}, nil))
	if response := conn.read(); response.Status != 404 {
		t.Fatalf("verify-object after mismatches answered %+v, want 404", response)
	}
	conn.quit()
	if stored := serveLFSEntries(t, layout.RepoLFSObjects("r")); len(stored) != 0 {
		t.Fatalf("lfs/objects holds %q after only mismatched uploads", stored)
	}
	download := openServeLFS(t, f.h, f.keys["B"], "download")
	download.send(serveLFSRequestBytes("get-object "+oid, nil, nil))
	if response := download.read(); response.Status != 404 || !response.Delim {
		t.Fatalf("get-object of a mismatched upload answered %+v, want 404 with a message", response)
	}
	download.quit()
	if stored := f.lfsStored(); stored != 0 {
		t.Fatalf("lfs-stored = %d after only mismatched uploads", stored)
	}
}

// US5-3 and FR-021: with quota N and N-10 bytes stored, an 11-byte object is
// refused at batch and at put-object before it becomes downloadable, and a
// 10-byte object is accepted. The limit is inclusive.
func TestServeLFSQuotaBoundary(t *testing.T) {
	f := newServeLFSFixture(t, newServeHarness(t))
	first := serveLFSRandom(t, 1000)
	f.upload("A", first)
	const quota = 1010
	f.h.Serve("serve", "repo", "set", "r", "--lfs-quota", strconv.Itoa(quota))
	eleven, ten := serveLFSRandom(t, 11), serveLFSRandom(t, 10)
	elevenOID, tenOID, firstOID := serveLFSOID(eleven), serveLFSOID(ten), serveLFSOID(first)

	conn := openServeLFS(t, f.h, f.keys["A"], "upload")
	requireQuota := func(label string, response serveLFSResponse) {
		t.Helper()
		rejection := serveLFSRequireRejection(t, label, response, 507, serveReasonLFSQuota, f.actors["A"])
		if rejection.Budget != "lfs-quota" || rejection.Limit != quota || rejection.Measured != quota+1 {
			t.Fatalf("%s: budget=%s limit=%d measured=%d, want lfs-quota %d %d", label, rejection.Budget, rejection.Limit, rejection.Measured, quota, quota+1)
		}
	}
	// The batch counts only objects that are not stored, each OID once.
	conn.send(serveLFSBatch(firstOID+" 1000", elevenOID+" 11", elevenOID+" 11"))
	requireQuota("batch with an 11-byte object", conn.read())
	conn.send(serveLFSPutObject(elevenOID, 11, eleven))
	requireQuota("put-object of an 11-byte object", conn.read())
	conn.send(serveLFSRequestBytes("verify-object "+elevenOID, []string{"size=11"}, nil))
	if response := conn.read(); response.Status != 404 {
		t.Fatalf("verify-object of the refused object answered %+v", response)
	}
	if staged := serveHarnessEntries(t, f.h.Layout().RepoLFSIncoming("r")); len(staged) != 0 {
		t.Fatalf("the refused upload left %q in lfs/incoming", staged)
	}
	download := openServeLFS(t, f.h, f.keys["B"], "download")
	download.send(serveLFSRequestBytes("get-object "+elevenOID, nil, nil))
	if response := download.read(); response.Status != 404 {
		t.Fatalf("get-object of the refused object answered %+v, want 404", response)
	}

	conn.send(serveLFSBatch(firstOID+" 1000", tenOID+" 10"))
	if response := conn.read(); response.Status != 200 || !equalStrings(response.Lines, []string{firstOID + " 1000 noop", tenOID + " 10 upload"}) {
		t.Fatalf("batch with a 10-byte object answered %+v", response)
	}
	conn.send(serveLFSPutObject(tenOID, 10, ten))
	if response := conn.read(); response.Status != 200 {
		t.Fatalf("put-object of the 10-byte object answered %+v", response)
	}
	conn.send(serveLFSRequestBytes("verify-object "+tenOID, []string{"size=10"}, nil))
	if response := conn.read(); response.Status != 200 {
		t.Fatalf("verify-object of the 10-byte object answered %+v", response)
	}
	// Exactly at the limit, one more byte is over it; re-sending a stored
	// object costs nothing.
	conn.send(serveLFSPutObject(serveLFSOID([]byte{1}), 1, []byte{1}))
	if response := conn.read(); response.Status != 507 {
		t.Fatalf("put-object of one byte at the limit answered %+v, want 507", response)
	}
	conn.send(serveLFSPutObject(tenOID, 10, ten))
	if response := conn.read(); response.Status != 200 {
		t.Fatalf("re-sending a stored object at the limit answered %+v, want 200", response)
	}
	conn.quit()

	download.send(serveLFSRequestBytes("get-object "+tenOID, []string{"size=10"}, nil))
	response := download.read()
	if response.Status != 200 || !equalStrings(response.Args, []string{"size=10"}) || !bytes.Equal(response.Data, ten) {
		t.Fatalf("get-object of the 10-byte object answered %+v", response)
	}
	download.quit()
	if stored := f.lfsStored(); stored != quota {
		t.Fatalf("lfs-stored = %d, want the quota %d", stored, quota)
	}
}

// FR-021: the third check point. An upload that passed the check before its
// data is refused after verification when a concurrent upload filled the
// quota meanwhile, because the recheck under lfs.lock sees the new total.
func TestServeLFSQuotaRecheckedBeforeStoring(t *testing.T) {
	f := newServeLFSFixture(t, newServeHarness(t))
	f.upload("A", serveLFSRandom(t, 1000))
	const quota = 1010
	f.h.Serve("serve", "repo", "set", "r", "--lfs-quota", strconv.Itoa(quota))
	slow, fast := serveLFSRandom(t, 10), serveLFSRandom(t, 10)
	slowOID := serveLFSOID(slow)
	incoming := f.h.Layout().RepoLFSIncoming("r")

	conn := openServeLFS(t, f.h, f.keys["A"], "upload")
	conn.send(serveLFSWireText("put-object "+slowOID), serveLFSWireText("size=10"), []byte(serveLFSWireDelim), serveLFSWireData(slow[:5]))
	deadline := time.Now().Add(30 * time.Second)
	for len(serveHarnessEntries(t, incoming)) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the first upload never reached lfs/incoming; stderr %s", conn.stderr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.upload("B", fast)
	conn.send(serveLFSWireData(slow[5:]), []byte(serveLFSWireFlush))
	rejection := serveLFSRequireRejection(t, "recheck", conn.read(), 507, serveReasonLFSQuota, f.actors["A"])
	if rejection.Limit != quota || rejection.Measured != quota+10 {
		t.Fatalf("recheck limit=%d measured=%d, want %d and %d", rejection.Limit, rejection.Measured, quota, quota+10)
	}
	conn.quit()
	if staged := serveHarnessEntries(t, incoming); len(staged) != 0 {
		t.Fatalf("the refused upload left %q in lfs/incoming", staged)
	}
	if stored := f.lfsStored(); stored != quota {
		t.Fatalf("lfs-stored = %d, want %d", stored, quota)
	}
}

// US5-4 and FR-021: lock, unlock and list-lock answer 501 in both kinds of
// session, the advertisement never offers locking (openServeLFS requires it
// to be exactly version=1), and the Git LFS 3.7.1 client reports the
// server's message.
func TestServeLFSLockingUnsupported(t *testing.T) {
	f := newServeLFSFixture(t, newServeHarness(t))
	for _, operation := range []string{"upload", "download"} {
		conn := openServeLFS(t, f.h, f.keys["A"], operation)
		for _, request := range [][]byte{
			serveLFSRequestBytes("lock", []string{"path=big.bin", "refname=refs/heads/main"}, nil),
			serveLFSRequestBytes("unlock 1", []string{"force=true"}, nil),
			serveLFSRequestBytes("list-lock", []string{"refname=refs/heads/main", "limit=10"}, nil),
		} {
			conn.send(request)
			if response := conn.read(); response.Status != 501 || !equalStrings(response.Lines, []string{"locking is unsupported by hn serve"}) {
				t.Fatalf("%s session: %q answered %+v", operation, request, response)
			}
		}
		conn.quit()
	}

	t.Run("client", func(t *testing.T) {
		requireServeLFSClient(t)
		work := serveLFSPushTracked(f, "A", "big.bin", serveLFSRandom(t, 64))
		for _, command := range []struct {
			args []string
			want string
		}{
			{[]string{"lfs", "lock", "big.bin"}, "Locking big.bin failed: server unable to create lock: locking is unsupported by hn serve"},
			{[]string{"lfs", "locks"}, "Error while retrieving locks: server error searching for locks: locking is unsupported by hn serve"},
		} {
			output, status := f.clients["A"].exec(work, "git", command.args...)
			if status != 2 || !strings.Contains(output, command.want) {
				t.Fatalf("git %s: exit %d, want 2 with %q:\n%s", strings.Join(command.args, " "), status, command.want, output)
			}
		}
	})
}

// Interrupted uploads are never downloadable and never counted. A closed
// connection removes its staged file at once; a session killed outright
// leaves it, and the next session removes it once it is older than an hour
// while keeping younger staged files, which may be uploads in progress.
func TestServeLFSInterruptedUploadNeverStored(t *testing.T) {
	f := newServeLFSFixture(t, newServeHarness(t))
	layout := f.h.Layout()
	incoming := layout.RepoLFSIncoming("r")
	content := serveLFSRandom(t, 100*1024)
	oid := serveLFSOID(content)
	partial := func(conn *serveLFSConn) {
		conn.send(serveLFSWireText("put-object "+oid), serveLFSWireText("size="+strconv.Itoa(len(content))), []byte(serveLFSWireDelim), serveLFSWireData(content[:40000]))
	}

	closed := openServeLFS(t, f.h, f.keys["A"], "upload")
	partial(closed)
	if status, _ := closed.finish(); status != 1 {
		t.Fatalf("a session closed mid put-object exited %d, want 1", status)
	}
	if staged := serveHarnessEntries(t, incoming); len(staged) != 0 {
		t.Fatalf("a closed connection left %q in lfs/incoming", staged)
	}

	killed := openServeLFS(t, f.h, f.keys["A"], "upload")
	partial(killed)
	deadline := time.Now().Add(30 * time.Second)
	for len(serveHarnessEntries(t, incoming)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the upload never reached lfs/incoming")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := killed.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = killed.finish()
	abandoned := serveHarnessEntries(t, incoming)
	if len(abandoned) != 1 {
		t.Fatalf("lfs/incoming after a killed session = %q, want the one staged file", abandoned)
	}
	abandonedPath := filepath.Join(incoming, abandoned[0])

	download := openServeLFS(t, f.h, f.keys["B"], "download")
	download.send(serveLFSRequestBytes("get-object "+oid, nil, nil))
	if response := download.read(); response.Status != 404 {
		t.Fatalf("get-object of an interrupted upload answered %+v, want 404", response)
	}
	download.quit()
	if _, err := os.Lstat(abandonedPath); err != nil {
		t.Fatalf("a session removed a staged file younger than an hour: %v", err)
	}
	if stored := f.lfsStored(); stored != 0 {
		t.Fatalf("lfs-stored = %d with only a staged upload", stored)
	}

	young := filepath.Join(incoming, "put-young")
	writeServeTestFile(t, young, "in progress", 0o600)
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(abandonedPath, old, old); err != nil {
		t.Fatal(err)
	}
	openServeLFS(t, f.h, f.keys["A"], "download").quit()
	if staged := serveHarnessEntries(t, incoming); !equalStrings(staged, []string{"put-young"}) {
		t.Fatalf("lfs/incoming after cleanup = %q, want only the young staged file", staged)
	}
	if stored := serveLFSEntries(t, layout.RepoLFSObjects("r")); len(stored) != 0 {
		t.Fatalf("lfs/objects holds %q after interrupted uploads", stored)
	}
}

// Batch answers: upload marks stored objects noop and missing ones upload,
// download marks every object download, unknown arguments are ignored, no
// id or token values are sent, and malformed batches answer 400 without
// ending the session.
func TestServeLFSBatchActions(t *testing.T) {
	f := newServeLFSFixture(t, newServeHarness(t))
	stored := f.upload("A", []byte("stored object\n"))
	missing := serveLFSOID([]byte("missing object\n"))

	upload := openServeLFS(t, f.h, f.keys["A"], "upload")
	upload.send(serveLFSRequestBytes("batch", []string{"transfer=ssh", "refname=refs/heads/main", "unknown=ignored"},
		append(serveLFSWireText(stored+" 14"), serveLFSWireText(missing+" 15 extra=ignored")...)))
	if response := upload.read(); response.Status != 200 || !equalStrings(response.Lines, []string{stored + " 14 noop", missing + " 15 upload"}) {
		t.Fatalf("upload batch answered %+v", response)
	}
	for _, malformed := range []struct {
		name    string
		request []byte
	}{
		{"sha512", serveLFSRequestBytes("batch", []string{"hash-algo=sha512"}, serveLFSWireText(missing+" 15"))},
		{"uppercase OID", serveLFSBatch(strings.ToUpper(missing) + " 15")},
		{"short OID", serveLFSBatch(missing[:63] + " 15")},
		{"negative size", serveLFSBatch(missing + " -1")},
		{"missing size", serveLFSBatch(missing)},
		{"version 2", serveLFSRequestBytes("version 2", nil, nil)},
	} {
		upload.send(malformed.request)
		if response := upload.read(); response.Status != 400 || len(response.Lines) == 0 {
			t.Fatalf("%s answered %+v, want 400 with a message", malformed.name, response)
		}
	}
	upload.quit()

	download := openServeLFS(t, f.h, f.keys["B"], "download")
	download.send(serveLFSBatch(stored+" 14", missing+" 15"))
	if response := download.read(); response.Status != 200 || !equalStrings(response.Lines, []string{stored + " 14 download", missing + " 15 download"}) {
		t.Fatalf("download batch answered %+v", response)
	}
	download.quit()
}

// Framing and grammar: a length header above 65519, a reserved length, a
// non-hex header, an unknown command, a command that the session's
// operation does not allow and a request over the argument, line or text
// bound each end the session with status 400 and exit 1.
func TestServeLFSFramingRefusals(t *testing.T) {
	f := newServeLFSFixture(t, newServeHarness(t))
	oid := serveLFSOID([]byte("x"))
	manyArguments := make([]string, 65)
	for index := range manyArguments {
		manyArguments[index] = "a=" + strconv.Itoa(index)
	}
	manyObjects := make([]string, 10_001)
	for index := range manyObjects {
		manyObjects[index] = oid + " 1"
	}
	longLines := make([]string, 65)
	for index := range longLines {
		longLines[index] = strings.Repeat("x", 65000)
	}
	for _, test := range []struct {
		name      string
		operation string
		wire      []byte
		message   string
	}{
		{"length ffff", "upload", []byte("ffff"), "pkt-line length 65535 exceeds 65519"},
		{"length 65520", "download", []byte("fff0"), "pkt-line length 65520 exceeds 65519"},
		{"reserved length 0002", "upload", []byte("0002"), "pkt-line length 2 is reserved"},
		{"reserved length 0003", "upload", []byte("0003"), "pkt-line length 3 is reserved"},
		{"non-hex header", "upload", []byte("00zz"), "is not four hexadecimal digits"},
		{"flush as a command", "upload", []byte(serveLFSWireFlush), "expected a command"},
		{"unknown command", "upload", serveLFSRequestBytes("frobnicate", nil, nil), `unknown command "frobnicate"`},
		{"long unknown command", "upload", serveLFSRequestBytes(strings.Repeat("\x01", 60000), nil, nil), `unknown command "\x01\x01`},
		{"get-object in an upload session", "upload", serveLFSRequestBytes("get-object "+oid, nil, nil), "get-object is only allowed in download sessions"},
		{"put-object in a download session", "download", serveLFSPutObject(oid, 1, []byte("x")), "put-object is only allowed in upload sessions"},
		{"verify-object in a download session", "download", serveLFSRequestBytes("verify-object "+oid, []string{"size=1"}, nil), "verify-object is only allowed in upload sessions"},
		{"delim in quit", "upload", serveLFSRequestBytes("quit", nil, []byte{}), "unexpected delim packet"},
		{"65 arguments", "upload", serveLFSRequestBytes("version 1", manyArguments, nil), "more than 64 arguments"},
		{"10001 batch lines", "download", serveLFSBatch(manyObjects...), "more than 10000 lines"},
		{"more than 4 MiB of text", "download", serveLFSBatch(longLines...), "more than 4194304 bytes of text"},
	} {
		conn := openServeLFS(t, f.h, f.keys["A"], test.operation)
		// The server may stop reading and exit before the whole request is
		// written, so a failed write here is expected and ignored.
		go func(wire []byte) { _, _ = conn.stdin.Write(wire) }(test.wire)
		response := conn.read()
		if response.Status != 400 || len(response.Lines) != 1 || !strings.Contains(response.Lines[0], test.message) {
			t.Fatalf("%s: answered %+v, want 400 mentioning %q", test.name, response, test.message)
		}
		if status, rest := conn.finish(); status != 1 || rest != "" {
			t.Fatalf("%s: exit %d with trailing output %q, want exit 1", test.name, status, rest)
		}
	}
}

// T086: the reader rejects an oversized length before reading or
// allocating any payload, accepts the largest allowed packet, and the
// writer splits data at the maximum payload.
func TestServeLFSPacketReaderChecksLengthBeforeReading(t *testing.T) {
	refused := newServeLFSPacketReader(io.MultiReader(strings.NewReader("ffff"), serveLFSUnreadable{t}))
	if _, _, err := refused.next(); !errors.Is(err, errServeLFSMalformed) {
		t.Fatalf("length ffff: %v, want a malformed-request error before any payload read", err)
	}

	largest := bytes.Repeat([]byte{'a'}, 65515)
	stream := "ffef" + string(largest) + "0004" + "000ahello\n" + "0000" + "0001"
	reader := newServeLFSPacketReader(strings.NewReader(stream))
	for index, want := range []struct {
		kind    serveLFSPacketKind
		payload string
	}{
		{serveLFSDataPacket, string(largest)},
		{serveLFSDataPacket, ""},
		{serveLFSDataPacket, "hello\n"},
		{serveLFSFlushPacket, ""},
		{serveLFSDelimPacket, ""},
	} {
		kind, payload, err := reader.next()
		if err != nil || kind != want.kind || string(payload) != want.payload {
			t.Fatalf("packet %d: kind %d payload length %d err %v", index, kind, len(payload), err)
		}
	}
	if serveLFSText([]byte("hello\n\n")) != "hello\n" {
		t.Fatal("serveLFSText must remove exactly one trailing newline")
	}
	if _, _, err := reader.next(); !errors.Is(err, errServeLFSClosed) {
		t.Fatalf("end of stream: %v, want errServeLFSClosed", err)
	}

	var output bytes.Buffer
	writer := newServeLFSPacketWriter(&output)
	if err := writer.data(bytes.Repeat([]byte{'b'}, 2*65515+1)); err != nil {
		t.Fatal(err)
	}
	if err := writer.flush(); err != nil {
		t.Fatal(err)
	}
	wire := output.String()
	for _, header := range []int{0, 65519} {
		if got := wire[header : header+4]; got != "ffef" {
			t.Fatalf("header at %d = %q, want ffef", header, got)
		}
	}
	if tail := wire[2*65519:]; tail != "0005b0000" {
		t.Fatalf("tail %q, want one 1-byte packet and a flush", tail)
	}
}

// serveLFSUnreadable fails the test when anything reads from it.
type serveLFSUnreadable struct{ t *testing.T }

func (u serveLFSUnreadable) Read([]byte) (int, error) {
	u.t.Fatal("the pkt-line reader read past a refused length header")
	return 0, io.EOF
}

// serveLFSAllowedWrite reports whether an LFS upload may create or change
// path, given relative to the server HOME: the repository's lfs/ tree and
// its lfs.lock, nothing else.
func serveLFSAllowedWrite(path string) bool {
	repo := serveHomeDirectoryName + "/state/repos/r/"
	return path == repo+"lfs" || strings.HasPrefix(path, repo+"lfs/") || path == repo+"lfs.lock"
}

// serveLFSAuditedRoundTrip is the NFR-004 and NFR-008 evidence for LFS on h,
// whose Wrap must already be set: A uploads an object with git lfs push, and
// the upload creates paths only below state/repos/r/lfs/ and lfs.lock, with
// modes 0700 and 0600; B downloads it, and the download changes nothing.
// No server-side process touches the sentinel TMPDIR.
func serveLFSAuditedRoundTrip(t *testing.T, h *serveHarness) {
	t.Helper()
	sentinel := t.TempDir()
	h.Env = map[string]string{"TMPDIR": sentinel}
	f := newServeLFSFixture(t, h)
	sentinelBefore := serveQualitySnapshot(t, sentinel)
	client := f.clients["A"]
	work := f.cloneWithLFS("A")
	client.Git(work, "lfs", "track", "*.bin")
	content := serveLFSRandom(t, 100*1024)
	if err := os.WriteFile(filepath.Join(work, "audit.bin"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	client.Git(work, "add", ".gitattributes", "audit.bin")
	client.Git(work, "commit", "-q", "-m", "audit object")

	before := serveQualitySnapshot(t, h.Home)
	client.Git(work, "lfs", "push", "origin", "HEAD")
	after := serveQualitySnapshot(t, h.Home)
	oid := serveLFSOID(content)
	object := serveHomeDirectoryName + "/state/repos/r/lfs/objects/" + oid[0:2] + "/" + oid[2:4] + "/" + oid
	sawObject := false
	for _, change := range serveQualityChangedPaths(before, after, false) {
		path, kind, _ := strings.Cut(change, " (")
		if !serveLFSAllowedWrite(path) {
			t.Errorf("NFR-004: the LFS upload changed %s outside state/repos/r/lfs/ and lfs.lock", change)
			continue
		}
		if kind == "removed)" {
			continue
		}
		entry := after[path]
		want := fs.FileMode(0o600)
		if entry.Type.IsDir() {
			want = 0o700
		}
		if entry.Mode != want {
			t.Errorf("NFR-008: %s has mode %04o, want %04o", path, entry.Mode, want)
		}
		sawObject = sawObject || path == object
	}
	if !sawObject {
		t.Fatalf("the audit did not see the stored object %s", object)
	}
	client.Git(work, "push", "-q", "origin", "HEAD:refs/heads/lfs-data")

	download := f.cloneWithLFS("B", "-b", "lfs-data")
	before = serveQualitySnapshot(t, h.Home)
	f.clients["B"].Git(download, "lfs", "pull")
	if changed := serveQualityChangedPaths(before, serveQualitySnapshot(t, h.Home), true); len(changed) > 0 {
		t.Errorf("the LFS download changed the server HOME:\n%s", strings.Join(changed, "\n"))
	}
	if got, err := os.ReadFile(filepath.Join(download, "audit.bin")); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("the downloaded object differs from the upload (%v)", err)
	}
	if changed := serveQualityChangedPaths(sentinelBefore, serveQualitySnapshot(t, sentinel), true); len(changed) > 0 {
		t.Errorf("NFR-004: server-side processes wrote to the sentinel TMPDIR:\n%s", strings.Join(changed, "\n"))
	}
	if problems := serveQualityModeProblems(t, h.Home); len(problems) > 0 {
		t.Errorf("NFR-008: unexpected modes after the LFS round trip:\n%s", strings.Join(problems, "\n"))
	}
}

// NFR-004 and NFR-008, snapshot evidence for LFS transfers.
func TestServeLFSWrittenPathsAndModes(t *testing.T) {
	requireServeLFSClient(t)
	serveLFSAuditedRoundTrip(t, newServeHarness(t))
}

// NFR-004, enforced evidence for LFS transfers: the same round trip with
// every server-side process inside WP13's bubblewrap boundary, which allows
// writes only below $HOME/.hn-serve and $HOME/.ssh. It skips with the reason
// under the same conditions as TestServeQualityEnforcedWriteBoundary.
func TestServeLFSEnforcedWriteBoundary(t *testing.T) {
	requireServeLFSClient(t)
	h := newServeHarness(t)
	wrap := h.EnforceWriteBoundary()
	serveLFSAuditedRoundTrip(t, h)
	t.Logf("NFR-004: LFS round trip passed inside %s", strings.Join(wrap, " "))
}

// The guide's lfs-quota example is the real output of the Git LFS 3.7.1
// client when a push would exceed the quota, which also pins the client's
// visible behavior for the refusal: git push exits 1, no ref moves, and the
// batch status and the server's rejection line are shown. Raising the quota
// lets the same push through.
func TestServeLFSDocumentedExample(t *testing.T) {
	requireServeLFSClient(t)
	documented := serveLFSGuideExample(t, "<!-- example: lfs-quota -->")
	block := strings.Join(documented, "\n")
	if len(documented) != 2 {
		t.Fatalf("example has %d lines, want the batch error and Git's push error", len(documented))
	}
	const prefix = "LFS: batch response: status 507 from server ("
	inner, ok := strings.CutPrefix(documented[0], prefix)
	if !ok || !strings.HasSuffix(inner, ")") {
		t.Fatalf("documented line %q is not a batch 507 error", documented[0])
	}
	rejection, err := parseServeRejectionLine(strings.TrimSuffix(inner, ")"))
	if err != nil {
		t.Fatalf("documented rejection does not parse: %v", err)
	}
	if rejection.Code != serveReasonLFSQuota || rejection.Limit != 100 || rejection.Measured != 150 {
		t.Fatalf("documented rejection %+v, want lfs-quota with limit 100 and measured 150", rejection)
	}

	f := newServeLFSFixture(t, newServeHarness(t))
	f.h.Serve("serve", "repo", "set", "r", "--lfs-quota", "100")
	client := f.clients["A"]
	work := f.cloneWithLFS("A")
	client.Git(work, "lfs", "track", "*.bin")
	if err := os.WriteFile(filepath.Join(work, "big.bin"), serveLFSRandom(t, 150), 0o644); err != nil {
		t.Fatal(err)
	}
	client.Git(work, "add", ".gitattributes", "big.bin")
	client.Git(work, "commit", "-q", "-m", "too big")
	output, status := client.exec(work, "git", "push", "origin", "HEAD:refs/heads/lfs-data")
	if status != 1 {
		t.Fatalf("git push over the quota exited %d, want 1:\n%s", status, output)
	}
	replace := strings.NewReplacer(rejection.Actor, f.actors["A"], "ssh://server.example/demo.git", "ssh://localhost/r.git")
	lines := strings.Split(output, "\n")
	next := 0
	for _, line := range lines {
		if next < len(documented) && line == replace.Replace(documented[next]) {
			next++
		}
	}
	if next != len(documented) {
		t.Fatalf("git push output does not contain the documented lines in order\nwant:\n%s\ngot:\n%s", replace.Replace(block), output)
	}
	if tip := serveHarnessGit(t, f.h.Layout().RepoGitDir("r"), "for-each-ref", "refs/heads/lfs-data"); tip != "" {
		t.Fatalf("the refused push created %s", tip)
	}
	if stored := f.lfsStored(); stored != 0 {
		t.Fatalf("lfs-stored = %d after the refused push", stored)
	}

	f.h.Serve("serve", "repo", "set", "r", "--lfs-quota", "150")
	client.Git(work, "push", "-q", "origin", "HEAD:refs/heads/lfs-data")
	if stored := f.lfsStored(); stored != 150 {
		t.Fatalf("lfs-stored = %d after raising the quota to 150, want 150", stored)
	}
}

// FR-020: an LFS session passes the same key and repository checks as a Git
// session before it sends any protocol byte, and git-lfs-authenticate and
// operations other than upload and download stay refused.
func TestServeLFSSessionChecksKeyAndRepository(t *testing.T) {
	f := newServeLFSFixture(t, newServeHarness(t))
	before := serveSessionSnapshot(t, f.h)
	requireServeSessionRefusal(t, "unknown key", serveSessionDirect(t, f.h, newServeTestKey(t, "").Fingerprint, serveSessionRequest("git-lfs-transfer 'r.git' upload"), nil),
		serveReasonUnknownKey, "", serveSessionUnknownKeySentence)
	requireServeSessionRefusal(t, "unhosted repository", serveSessionDirect(t, f.h, f.keys["A"].Fingerprint, serveSessionRequest("git-lfs-transfer 'missing.git' download"), nil),
		serveReasonUnhostedRepository, f.actors["A"], "repository missing is not hosted here.")
	for _, request := range []string{"git-lfs-transfer 'r.git' sideways", "git-lfs-transfer 'r.git'", "git-lfs-authenticate 'r.git' upload"} {
		requireServeSessionRefusal(t, request, serveSessionDirect(t, f.h, f.keys["A"].Fingerprint, &request, nil),
			serveReasonUnsupportedCommand, f.actors["A"], serveSessionUnsupportedSentence)
	}
	requireServeSessionUnchanged(t, "refused LFS sessions", before, serveSessionSnapshot(t, f.h))
}
