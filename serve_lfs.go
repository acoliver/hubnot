package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Git LFS transfer for hosted repositories: the git-lfs-transfer SSH
// protocol, version 1, from the Git LFS SSH adapter proposal
// (https://github.com/git-lfs/git-lfs/blob/main/docs/proposals/ssh_adapter.md),
// with uploads verified before they become downloadable and a per-repository
// byte quota. The contract is
// kitty-specs/hn-serve-01M5P0NP/contracts/lfs-transfer.md. Everything LFS
// lives in this file, so the feature can move to its own mission with one
// dispatch arm in serve_session.go (C-010).

const (
	// serveLFSMaxPacketLength is the largest pkt-line length header the
	// proposal allows, header bytes included.
	serveLFSMaxPacketLength = 65519
	serveLFSPacketHeader    = 4
	serveLFSMaxPayload      = serveLFSMaxPacketLength - serveLFSPacketHeader

	// serveLFSMaxArguments bounds the argument lines of one request,
	// serveLFSMaxBatchObjects the object lines of one batch and
	// serveLFSMaxRequestText the text of one request, so a request cannot
	// grow the server's memory without limit. Git LFS sends at most 100
	// objects per batch by default, about 9 KiB of text.
	serveLFSMaxArguments    = 64
	serveLFSMaxBatchObjects = 10_000
	serveLFSMaxRequestText  = 4 << 20

	// serveLFSStagedMaxAge is how old a staged upload must be before a new
	// session removes it as abandoned.
	serveLFSStagedMaxAge = time.Hour
	serveLFSStagedPrefix = "put-"

	// serveLFSLockWait bounds the wait for lfs.lock, which is only held for
	// the quota recheck and one rename.
	serveLFSLockWait = 30 * time.Second

	serveLFSLockingMessage = "locking is unsupported by hn serve"
)

// errServeLFSMalformed marks a request that breaks the pkt-line framing or
// the request grammar. The session answers status 400 and exits 1.
var errServeLFSMalformed = errors.New("malformed git-lfs-transfer request")

func serveLFSMalformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errServeLFSMalformed, fmt.Sprintf(format, args...))
}

// errServeLFSClosed reports that the client closed the connection, at a
// request boundary or in the middle of one. Nothing can be answered.
var errServeLFSClosed = errors.New("git-lfs-transfer connection closed")

// errServeLFSEnded reports that the session has sent its last answer and
// must end with exit status 1 without reading further, because the input is
// no longer at a request boundary: an upload sent more data than it declared.
var errServeLFSEnded = errors.New("git-lfs-transfer session ended after its answer")

// T086: pkt-line framing.

type serveLFSPacketKind int

const (
	serveLFSDataPacket serveLFSPacketKind = iota
	serveLFSFlushPacket
	serveLFSDelimPacket
)

// serveLFSPacketReader reads pkt-lines with one fixed payload buffer, so no
// length header can make it allocate. It reads its input without
// read-ahead: every byte it takes from r belongs to a packet it was asked
// for, so a caller that bounds the payload it reads also bounds the input
// consumed.
type serveLFSPacketReader struct {
	r       io.Reader
	header  [serveLFSPacketHeader]byte
	payload []byte
}

func newServeLFSPacketReader(r io.Reader) *serveLFSPacketReader {
	return &serveLFSPacketReader{r: r, payload: make([]byte, serveLFSMaxPayload)}
}

// next reads one whole packet. The returned payload is only valid until the
// next call. The length header is checked before any payload byte is read.
func (p *serveLFSPacketReader) next() (serveLFSPacketKind, []byte, error) {
	kind, payload, _, err := p.nextUpTo(serveLFSMaxPayload)
	return kind, payload, err
}

// nextUpTo reads one packet but at most limit bytes of its payload. It
// returns the payload bytes read and the payload length the header
// declared; when that length is above limit, the rest of the packet stays
// unread and the input is no longer at a packet boundary.
func (p *serveLFSPacketReader) nextUpTo(limit int) (serveLFSPacketKind, []byte, int, error) {
	if _, err := io.ReadFull(p.r, p.header[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, nil, 0, errServeLFSClosed
		}
		return 0, nil, 0, err
	}
	length := 0
	for _, digit := range p.header {
		value, ok := serveLFSHexValue(digit)
		if !ok {
			return 0, nil, 0, serveLFSMalformed("pkt-line length %q is not four hexadecimal digits", serveSanitizeText(string(p.header[:])))
		}
		length = length<<4 | value
	}
	switch {
	case length == 0:
		return serveLFSFlushPacket, nil, 0, nil
	case length == 1:
		return serveLFSDelimPacket, nil, 0, nil
	case length < serveLFSPacketHeader:
		return 0, nil, 0, serveLFSMalformed("pkt-line length %d is reserved", length)
	case length > serveLFSMaxPacketLength:
		return 0, nil, 0, serveLFSMalformed("pkt-line length %d exceeds %d", length, serveLFSMaxPacketLength)
	}
	declared := length - serveLFSPacketHeader
	payload := p.payload[:min(declared, max(limit, 0))]
	if _, err := io.ReadFull(p.r, payload); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, nil, 0, errServeLFSClosed
		}
		return 0, nil, 0, err
	}
	return serveLFSDataPacket, payload, declared, nil
}

func serveLFSHexValue(digit byte) (int, bool) {
	switch {
	case digit >= '0' && digit <= '9':
		return int(digit - '0'), true
	case digit >= 'a' && digit <= 'f':
		return int(digit-'a') + 10, true
	case digit >= 'A' && digit <= 'F':
		return int(digit-'A') + 10, true
	}
	return 0, false
}

// serveLFSQuote quotes a client-supplied value for a message line, cut to
// 64 bytes so that an echoed value always fits in one packet.
func serveLFSQuote(value string) string {
	if len(value) > 64 {
		value = value[:64] + "..."
	}
	return strconv.Quote(value)
}

// serveLFSText is a text packet's payload with one trailing newline removed.
func serveLFSText(payload []byte) string {
	return strings.TrimSuffix(string(payload), "\n")
}

// serveLFSPacketWriter buffers packets and sends them at every flush-pkt.
type serveLFSPacketWriter struct {
	w *bufio.Writer
}

func newServeLFSPacketWriter(w io.Writer) *serveLFSPacketWriter {
	return &serveLFSPacketWriter{w: bufio.NewWriterSize(w, serveLFSMaxPacketLength)}
}

func (p *serveLFSPacketWriter) packet(payload []byte) error {
	if len(payload) > serveLFSMaxPayload {
		return fmt.Errorf("hn serve: pkt-line payload of %d bytes exceeds %d", len(payload), serveLFSMaxPayload)
	}
	if _, err := fmt.Fprintf(p.w, "%04x", len(payload)+serveLFSPacketHeader); err != nil {
		return err
	}
	_, err := p.w.Write(payload)
	return err
}

// text writes one text packet with a trailing newline.
func (p *serveLFSPacketWriter) text(line string) error {
	return p.packet([]byte(line + "\n"))
}

// data writes binary data as packets of at most the maximum payload.
func (p *serveLFSPacketWriter) data(payload []byte) error {
	for len(payload) > 0 {
		chunk := payload[:min(len(payload), serveLFSMaxPayload)]
		if err := p.packet(chunk); err != nil {
			return err
		}
		payload = payload[len(chunk):]
	}
	return nil
}

func (p *serveLFSPacketWriter) delim() error {
	_, err := p.w.WriteString("0001")
	return err
}

func (p *serveLFSPacketWriter) flush() error {
	if _, err := p.w.WriteString("0000"); err != nil {
		return err
	}
	return p.w.Flush()
}

// T087: the session.

// serveLFSTransfer is one git-lfs-transfer connection for one repository and
// one operation, after the key and repository checks have passed.
type serveLFSTransfer struct {
	layout    serveLayout
	actor     string
	repo      string
	operation string
	in        *serveLFSPacketReader
	out       *serveLFSPacketWriter
}

// serveLFSRequest is one request after its command line: the argument lines
// and, after a delim-pkt, the body lines.
type serveLFSRequest struct {
	args  []string
	lines []string
}

// runServeLFSSession serves git-lfs-transfer on in and out and returns the
// exit status: 0 after quit, 1 after a malformed request, an upload that
// sent more data than it declared, or a lost connection. An error is an
// internal failure that main reports.
func runServeLFSSession(layout serveLayout, binding serveKeyBinding, repo, operation string, in io.Reader, out io.Writer) (int, error) {
	session := &serveLFSTransfer{
		layout:    layout,
		actor:     binding.Actor,
		repo:      repo,
		operation: operation,
		in:        newServeLFSPacketReader(in),
		out:       newServeLFSPacketWriter(out),
	}
	if err := session.prepare(); err != nil {
		return 0, err
	}
	// The capability advertisement. locking is never advertised (FR-021).
	if err := session.out.text("version=1"); err != nil {
		return 0, err
	}
	if err := session.out.flush(); err != nil {
		return 0, err
	}
	for {
		done, err := session.serveOne()
		switch {
		case errors.Is(err, errServeLFSClosed), errors.Is(err, errServeLFSEnded):
			return 1, nil
		case errors.Is(err, errServeLFSMalformed):
			_ = session.respond(400, nil, []string{serveSanitizeText(err.Error())})
			return 1, nil
		case err != nil:
			_ = session.respond(500, nil, []string{"hn serve could not complete the request"})
			return 0, fmt.Errorf("hn serve git-lfs-transfer %s: %w", repo, err)
		case done:
			return 0, nil
		}
	}
}

// prepare creates lfs/, lfs/incoming/ and lfs/objects/ when absent and
// removes staged uploads older than serveLFSStagedMaxAge.
func (s *serveLFSTransfer) prepare() error {
	for _, directory := range []string{s.layout.RepoLFSDir(s.repo), s.layout.RepoLFSIncoming(s.repo), s.layout.RepoLFSObjects(s.repo)} {
		if err := ensureServeLFSDirectory(directory); err != nil {
			return err
		}
	}
	return s.removeAbandonedUploads(time.Now().Add(-serveLFSStagedMaxAge))
}

// ensureServeLFSDirectory creates directory with mode 0700, whatever the
// umask, or checks that an existing one is a real 0700 directory.
func ensureServeLFSDirectory(directory string) error {
	err := os.Mkdir(directory, 0o700)
	switch {
	case err == nil:
		if err := os.Chmod(directory, 0o700); err != nil {
			return serveStateError(directory, err)
		}
		return nil
	case errors.Is(err, os.ErrExist):
		return requireServeDirectory(directory)
	default:
		return serveStateError(directory, err)
	}
}

// removeAbandonedUploads removes regular files in lfs/incoming/ last
// modified before cutoff. Younger files may belong to an upload in progress
// in another session.
func (s *serveLFSTransfer) removeAbandonedUploads(cutoff time.Time) error {
	incoming := s.layout.RepoLFSIncoming(s.repo)
	entries, err := os.ReadDir(incoming)
	if err != nil {
		return serveStateError(incoming, err)
	}
	var abandoned []string
	for _, entry := range entries {
		path := filepath.Join(incoming, entry.Name())
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return serveStateError(path, err)
		}
		if info.Mode().IsRegular() && info.ModTime().Before(cutoff) {
			abandoned = append(abandoned, path)
		}
	}
	if len(abandoned) == 0 {
		return nil
	}
	return s.removeStaged(abandoned...)
}

// serveOne reads and answers one request. done is true after quit.
func (s *serveLFSTransfer) serveOne() (done bool, err error) {
	kind, payload, err := s.in.next()
	if err != nil {
		return false, err
	}
	if kind != serveLFSDataPacket {
		return false, serveLFSMalformed("expected a command, got a flush or delim packet")
	}
	command, argument, _ := strings.Cut(serveLFSText(payload), " ")
	switch command {
	case "version":
		return false, s.version(argument)
	case "batch":
		return false, s.batch(argument)
	case "put-object":
		if err := s.requireOperation(command, serveLFSOperationUpload); err != nil {
			return false, err
		}
		return false, s.putObject(argument)
	case "verify-object":
		if err := s.requireOperation(command, serveLFSOperationUpload); err != nil {
			return false, err
		}
		return false, s.verifyObject(argument)
	case "get-object":
		if err := s.requireOperation(command, serveLFSOperationDownload); err != nil {
			return false, err
		}
		return false, s.getObject(argument)
	case "lock", "unlock", "list-lock":
		if _, err := s.readRequest(true); err != nil {
			return false, err
		}
		return false, s.respond(501, nil, []string{serveLFSLockingMessage})
	case "quit":
		if _, err := s.readRequest(false); err != nil {
			return false, err
		}
		return true, s.respond(200, nil, nil)
	default:
		return false, serveLFSMalformed("unknown command %s", serveLFSQuote(command))
	}
}

func (s *serveLFSTransfer) requireOperation(command, operation string) error {
	if s.operation != operation {
		return serveLFSMalformed("%s is only allowed in %s sessions", command, operation)
	}
	return nil
}

// readRequest reads argument lines up to a flush-pkt. With body, a delim-pkt
// may follow the arguments, and the text lines after it, up to the flush,
// are returned as the body.
func (s *serveLFSTransfer) readRequest(body bool) (serveLFSRequest, error) {
	var request serveLFSRequest
	inBody := false
	text := 0
	for {
		kind, payload, err := s.in.next()
		if err != nil {
			return request, err
		}
		if text += len(payload); text > serveLFSMaxRequestText {
			return request, serveLFSMalformed("more than %d bytes of text in one request", serveLFSMaxRequestText)
		}
		switch {
		case kind == serveLFSFlushPacket:
			return request, nil
		case kind == serveLFSDelimPacket && (inBody || !body):
			return request, serveLFSMalformed("unexpected delim packet")
		case kind == serveLFSDelimPacket:
			inBody = true
		case inBody:
			if len(request.lines) >= serveLFSMaxBatchObjects {
				return request, serveLFSMalformed("more than %d lines in one request", serveLFSMaxBatchObjects)
			}
			request.lines = append(request.lines, serveLFSText(payload))
		default:
			if len(request.args) >= serveLFSMaxArguments {
				return request, serveLFSMalformed("more than %d arguments in one request", serveLFSMaxArguments)
			}
			request.args = append(request.args, serveLFSText(payload))
		}
	}
}

// readArgumentsBeforeData reads the argument lines of a put-object request,
// which end with the delim-pkt before the object data.
func (s *serveLFSTransfer) readArgumentsBeforeData() ([]string, error) {
	var args []string
	for {
		kind, payload, err := s.in.next()
		if err != nil {
			return nil, err
		}
		switch kind {
		case serveLFSDelimPacket:
			return args, nil
		case serveLFSFlushPacket:
			return nil, serveLFSMalformed("put-object without a delim packet before its data")
		}
		if len(args) >= serveLFSMaxArguments {
			return nil, serveLFSMalformed("more than %d arguments in one request", serveLFSMaxArguments)
		}
		args = append(args, serveLFSText(payload))
	}
}

// respond writes a status, its arguments and, when there are any, a
// delim-pkt and the lines, then a flush-pkt.
func (s *serveLFSTransfer) respond(status int, args, lines []string) error {
	if err := s.out.text("status " + strconv.Itoa(status)); err != nil {
		return err
	}
	for _, arg := range args {
		if err := s.out.text(arg); err != nil {
			return err
		}
	}
	if len(lines) > 0 {
		if err := s.out.delim(); err != nil {
			return err
		}
		for _, line := range lines {
			if err := s.out.text(line); err != nil {
				return err
			}
		}
	}
	return s.out.flush()
}

func (s *serveLFSTransfer) version(argument string) error {
	if _, err := s.readRequest(false); err != nil {
		return err
	}
	if argument != "1" {
		return s.respond(400, nil, []string{"hn serve supports git-lfs-transfer version 1 only"})
	}
	return s.respond(200, nil, nil)
}

// serveLFSObjectRequest is one "oid size" line of a batch.
type serveLFSObjectRequest struct {
	oid  string
	size int64
}

func (s *serveLFSTransfer) batch(argument string) error {
	request, err := s.readRequest(true)
	if err != nil {
		return err
	}
	if argument != "" {
		return s.respond(400, nil, []string{"batch takes its parameters as argument lines"})
	}
	// transfer and refname need no answer, and the proposal requires
	// unknown arguments to be ignored.
	for _, arg := range request.args {
		if value, ok := strings.CutPrefix(arg, "hash-algo="); ok && value != "sha256" {
			return s.respond(400, nil, []string{"hash-algo " + serveLFSQuote(value) + " is not supported; hn serve uses sha256"})
		}
	}
	objects := make([]serveLFSObjectRequest, 0, len(request.lines))
	for index, line := range request.lines {
		fields := strings.Split(line, " ")
		size, sizeOK := parseServeLFSSize(fieldOrEmpty(fields, 1))
		if !validServeLFSOID(fields[0]) || !sizeOK {
			return s.respond(400, nil, []string{fmt.Sprintf("batch line %d is not a 64-hex lowercase OID and a decimal size", index+1)})
		}
		objects = append(objects, serveLFSObjectRequest{oid: fields[0], size: size})
	}
	lines := make([]string, 0, len(objects))
	if s.operation == serveLFSOperationDownload {
		for _, object := range objects {
			lines = append(lines, fmt.Sprintf("%s %d download", object.oid, object.size))
		}
		return s.respond(200, nil, lines)
	}
	stored := make(map[string]bool, len(objects))
	counted := make(map[string]bool, len(objects))
	var additional int64
	for _, object := range objects {
		if _, seen := stored[object.oid]; !seen {
			present, err := s.objectStored(object.oid)
			if err != nil {
				return err
			}
			stored[object.oid] = present
		}
		action := "noop"
		if !stored[object.oid] {
			action = "upload"
			if !counted[object.oid] {
				counted[object.oid] = true
				additional = serveLFSAddBytes(additional, object.size)
			}
		}
		lines = append(lines, fmt.Sprintf("%s %d %s", object.oid, object.size, action))
	}
	if additional > 0 {
		refused, err := s.checkQuota(additional)
		if err != nil || refused {
			return err
		}
	}
	return s.respond(200, nil, lines)
}

func fieldOrEmpty(fields []string, index int) string {
	if index < len(fields) {
		return fields[index]
	}
	return ""
}

// validServeLFSOID accepts exactly 64 lowercase hexadecimal digits, so an
// OID is always safe to use as a path component.
func validServeLFSOID(oid string) bool {
	if len(oid) != sha256.Size*2 {
		return false
	}
	for index := 0; index < len(oid); index++ {
		character := oid[index]
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// parseServeLFSSize accepts a non-empty run of decimal digits that fits in
// an int64.
func parseServeLFSSize(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return 0, false
		}
	}
	size, err := strconv.ParseInt(value, 10, 64)
	return size, err == nil
}

// serveLFSSizeArgument returns the value of the single size= argument.
func serveLFSSizeArgument(args []string) (int64, bool) {
	size, found := int64(0), false
	for _, arg := range args {
		value, ok := strings.CutPrefix(arg, "size=")
		if !ok {
			continue
		}
		parsed, valid := parseServeLFSSize(value)
		if found || !valid {
			return 0, false
		}
		size, found = parsed, true
	}
	return size, found
}

// serveLFSAddBytes adds two non-negative byte counts, saturating at the
// int64 maximum so a hostile declared size cannot wrap below the quota.
func serveLFSAddBytes(a, b int64) int64 {
	if b > math.MaxInt64-a {
		return math.MaxInt64
	}
	return a + b
}

// T088: staged uploads, verification, verify-object and get-object.

func (s *serveLFSTransfer) objectPath(oid string) string {
	return filepath.Join(s.layout.RepoLFSObjects(s.repo), oid[0:2], oid[2:4], oid)
}

// storedObject returns the Lstat information of a stored object, or false
// when no regular file is stored under oid.
func (s *serveLFSTransfer) storedObject(oid string) (os.FileInfo, bool, error) {
	path := s.objectPath(oid)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, serveStateError(path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, serveDefect(path, "is not a regular file")
	}
	return info, true, nil
}

func (s *serveLFSTransfer) objectStored(oid string) (bool, error) {
	_, present, err := s.storedObject(oid)
	return present, err
}

func (s *serveLFSTransfer) putObject(oid string) error {
	args, err := s.readArgumentsBeforeData()
	if err != nil {
		return err
	}
	size, sizeOK := serveLFSSizeArgument(args)
	if !validServeLFSOID(oid) || !sizeOK {
		// Without a valid size the data has no bound, so it is not read.
		return serveLFSMalformed("put-object needs a 64-hex lowercase OID and one decimal size= argument")
	}
	stored, present, err := s.storedObject(oid)
	if err != nil {
		return err
	}
	if present {
		return s.recheckStored(oid, size, stored.Size())
	}
	limit, measured, err := s.quotaAfter(size)
	if err != nil {
		return err
	}
	if measured > limit {
		return s.discardData(oid, size, limit, measured)
	}
	staged, err := s.receiveStaged(oid, size)
	if err != nil || staged == "" {
		return err
	}
	return s.commitStaged(staged, oid, size)
}

// recheckStored answers a put-object for an object that is already stored.
// The stored object is never replaced and nothing is staged: the data is
// hashed in memory and dropped, so a known OID cannot make the server write
// a declared size of the client's choosing to disk outside the quota. The
// server reads at most the smaller of the declared and stored sizes plus one
// byte, because data of any other size cannot hash to oid. A retry that
// matches the stored object answers status 200 and costs no quota; any
// other data gets the lfs-object-mismatch answer, and data past that bound
// also ends the session.
func (s *serveLFSTransfer) recheckStored(oid string, size, storedSize int64) error {
	hash := sha256.New()
	received, overran, err := s.readObjectData(min(size, storedSize), func(payload []byte) error {
		_, err := hash.Write(payload)
		return err
	})
	switch {
	case err != nil:
		return err
	case overran:
		return s.respondOverran(oid)
	case size != storedSize || received != size || hex.EncodeToString(hash.Sum(nil)) != oid:
		return s.respondMismatch(oid)
	}
	return s.respond(200, nil, nil)
}

// readObjectData passes the data packets of one put-object request, up to
// its flush-pkt, to write. It reads no more than size+1 bytes of object
// data in total (T088): each packet's payload is read only up to the bytes
// still allowed, so when a packet would take the total past size, the
// server reads at most one byte beyond size from it, reports overran and
// leaves the rest of the body unread. The bytes past size are never
// written. The session must end after its answer because the input is no
// longer at a request boundary.
func (s *serveLFSTransfer) readObjectData(size int64, write func([]byte) error) (received int64, overran bool, err error) {
	for {
		remaining := size - received
		allowed := serveLFSMaxPayload
		if remaining < serveLFSMaxPayload {
			allowed = int(remaining) + 1
		}
		kind, payload, declared, err := s.in.nextUpTo(allowed)
		if err != nil {
			return received, false, err
		}
		switch kind {
		case serveLFSFlushPacket:
			return received, false, nil
		case serveLFSDelimPacket:
			return received, false, serveLFSMalformed("unexpected delim packet in object data")
		}
		if int64(declared) > remaining {
			return received, true, nil
		}
		if err := write(payload); err != nil {
			return received, false, err
		}
		received += int64(len(payload))
	}
}

// discardData reads and drops the data of an upload refused by the quota,
// then answers with the quota refusal. Data beyond the declared size gets
// the lfs-object-mismatch answer instead and ends the session.
func (s *serveLFSTransfer) discardData(oid string, size, limit, measured int64) error {
	_, overran, err := s.readObjectData(size, func([]byte) error { return nil })
	if err != nil {
		return err
	}
	if overran {
		return s.respondOverran(oid)
	}
	return s.respondQuota(limit, measured)
}

// serveLFSUpload is the outcome of streaming one upload into staging.
type serveLFSUpload int

const (
	// serveLFSUploadMatched: the byte count and SHA-256 match the request.
	serveLFSUploadMatched serveLFSUpload = iota
	// serveLFSUploadMismatched: the data ended at a flush-pkt but its byte
	// count or SHA-256 differs.
	serveLFSUploadMismatched
	// serveLFSUploadOverran: the data went past the declared size and was
	// not read to its end.
	serveLFSUploadOverran
)

// receiveStaged streams the object data into a new 0600 file in
// lfs/incoming/ and returns its path when the content matches oid and size.
// On a mismatch it removes the file, answers status 422 and returns "";
// when the data went past the declared size it also ends the session. On
// any error, a lost connection included, it removes the file too.
func (s *serveLFSTransfer) receiveStaged(oid string, size int64) (string, error) {
	file, path, err := s.createStaged()
	if err != nil {
		return "", err
	}
	upload, err := s.streamStaged(file, path, oid, size)
	if closeErr := file.Close(); closeErr != nil && err == nil {
		err = serveStateError(path, closeErr)
	}
	if err != nil {
		_ = s.removeStaged(path)
		return "", err
	}
	if upload == serveLFSUploadMatched {
		return path, nil
	}
	if err := s.removeStaged(path); err != nil {
		return "", err
	}
	if upload == serveLFSUploadOverran {
		return "", s.respondOverran(oid)
	}
	return "", s.respondMismatch(oid)
}

// streamStaged copies the object data into file while hashing it, reading
// at most size bytes (see readObjectData). It reports whether the byte count
// equals size and the SHA-256 equals oid, and syncs the file when they do.
func (s *serveLFSTransfer) streamStaged(file *os.File, path, oid string, size int64) (serveLFSUpload, error) {
	hash := sha256.New()
	writer := io.MultiWriter(file, hash)
	received, overran, err := s.readObjectData(size, func(payload []byte) error {
		if _, err := writer.Write(payload); err != nil {
			return serveStateError(path, err)
		}
		return nil
	})
	switch {
	case err != nil:
		return serveLFSUploadMismatched, err
	case overran:
		return serveLFSUploadOverran, nil
	case received != size || hex.EncodeToString(hash.Sum(nil)) != oid:
		return serveLFSUploadMismatched, nil
	}
	if err := file.Sync(); err != nil {
		return serveLFSUploadMismatched, serveStateError(path, err)
	}
	return serveLFSUploadMatched, nil
}

// respondMismatch answers status 422 with the lfs-object-mismatch line.
func (s *serveLFSTransfer) respondMismatch(oid string) error {
	return s.respondRejection(422, serveRejection{
		Ref:   "-",
		Actor: s.actor,
		Code:  serveReasonLFSObjectMismatch,
		IDs:   []string{oid},
		Next:  serveReasonDefaultNext(serveReasonLFSObjectMismatch),
	})
}

// respondOverran answers an upload whose data went past its declared size
// with the lfs-object-mismatch line and ends the session, whose input still
// holds the unread rest of the object.
func (s *serveLFSTransfer) respondOverran(oid string) error {
	if err := s.respondMismatch(oid); err != nil {
		return err
	}
	return errServeLFSEnded
}

// createStaged creates a new 0600 file with a random name in lfs/incoming/.
func (s *serveLFSTransfer) createStaged() (*os.File, string, error) {
	incoming := s.layout.RepoLFSIncoming(s.repo)
	for attempt := 0; attempt < 3; attempt++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, "", err
		}
		path := filepath.Join(incoming, serveLFSStagedPrefix+hex.EncodeToString(random[:]))
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", serveStateError(path, err)
		}
		return file, path, nil
	}
	return nil, "", serveStateError(incoming, errors.New("cannot create a unique staged upload file"))
}

// commitStaged makes a verified staged upload downloadable. Under lfs.lock
// it rechecks the quota against the stored bytes at that moment and renames
// the file into lfs/objects/ only when the object still fits, so two
// concurrent uploads can never exceed the quota together.
func (s *serveLFSTransfer) commitStaged(staged, oid string, size int64) error {
	release, err := acquireServeLock(s.layout.RepoLFSLock(s.repo), serveLFSLockWait)
	if errors.Is(err, errServeLockBusy) {
		if err := s.removeStaged(staged); err != nil {
			return err
		}
		return s.respond(503, nil, []string{"the LFS store of " + s.repo + " stayed busy; retry the upload"})
	}
	if err != nil {
		_ = s.removeStaged(staged)
		return err
	}
	refusal, commitErr := s.commitStagedLocked(staged, oid, size)
	if releaseErr := release(); releaseErr != nil && commitErr == nil {
		commitErr = serveStateError(s.layout.RepoLFSLock(s.repo), releaseErr)
	}
	if commitErr != nil {
		_ = s.removeStaged(staged)
		return commitErr
	}
	if refusal != nil {
		return s.respondQuota(refusal.limit, refusal.measured)
	}
	return s.respond(200, nil, nil)
}

// serveLFSQuotaRefusal carries the numbers of an lfs-quota rejection.
type serveLFSQuotaRefusal struct {
	limit, measured int64
}

// commitStagedLocked runs while lfs.lock is held. It stores the object, or
// finds it already stored and removes the staged copy, and returns nil; or
// it removes the staged file and returns the quota refusal.
func (s *serveLFSTransfer) commitStagedLocked(staged, oid string, size int64) (*serveLFSQuotaRefusal, error) {
	present, err := s.objectStored(oid)
	if err != nil {
		return nil, err
	}
	if present {
		return nil, s.removeStaged(staged)
	}
	limit, measured, err := s.quotaAfter(size)
	if err != nil {
		return nil, err
	}
	if measured > limit {
		return &serveLFSQuotaRefusal{limit: limit, measured: measured}, s.removeStaged(staged)
	}
	objects := s.layout.RepoLFSObjects(s.repo)
	first := filepath.Join(objects, oid[0:2])
	second := filepath.Join(first, oid[2:4])
	for _, directory := range []string{first, second} {
		if err := ensureServeLFSDirectory(directory); err != nil {
			return nil, err
		}
	}
	target := filepath.Join(second, oid)
	if err := s.withLayoutLock(func() error {
		if err := os.Rename(staged, target); err != nil {
			return serveStateError(target, err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	for _, directory := range []string{second, first, objects} {
		if err := syncServeLFSDirectory(directory); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// removeStaged removes staged files that may have disappeared already.
func (s *serveLFSTransfer) removeStaged(paths ...string) error {
	return s.withLayoutLock(func() error {
		for _, path := range paths {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return serveStateError(path, err)
			}
		}
		return nil
	})
}

// withLayoutLock runs change while holding the layout lock exclusively.
// Session validation walks lfs/incoming/ under the shared layout lock, and
// a file renamed or removed between its directory listing and its Lstat
// would otherwise read as a layout defect in a concurrent session.
func (s *serveLFSTransfer) withLayoutLock(change func() error) error {
	release, err := lockServeLayout(s.layout, serveLockExclusive)
	if err != nil {
		return err
	}
	changeErr := change()
	if releaseErr := release(); releaseErr != nil && changeErr == nil {
		changeErr = serveStateError(s.layout.Root, releaseErr)
	}
	return changeErr
}

func syncServeLFSDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return serveStateError(directory, err)
	}
	syncErr := handle.Sync()
	if closeErr := handle.Close(); closeErr != nil && syncErr == nil {
		syncErr = closeErr
	}
	if syncErr != nil {
		return serveStateError(directory, syncErr)
	}
	return nil
}

func (s *serveLFSTransfer) verifyObject(oid string) error {
	request, err := s.readRequest(false)
	if err != nil {
		return err
	}
	size, sizeOK := serveLFSSizeArgument(request.args)
	if !validServeLFSOID(oid) || !sizeOK {
		return s.respond(400, nil, []string{"verify-object needs a 64-hex lowercase OID and one decimal size= argument"})
	}
	info, present, err := s.storedObject(oid)
	if err != nil {
		return err
	}
	if !present || info.Size() != size {
		return s.respond(404, nil, []string{fmt.Sprintf("object %s with size %d is not stored in %s", oid, size, s.repo)})
	}
	return s.respond(200, nil, nil)
}

func (s *serveLFSTransfer) getObject(oid string) error {
	if _, err := s.readRequest(false); err != nil {
		return err
	}
	if !validServeLFSOID(oid) {
		return s.respond(400, nil, []string{"get-object needs a 64-hex lowercase OID"})
	}
	file, size, err := s.openObject(oid)
	if err != nil {
		return err
	}
	if file == nil {
		return s.respond(404, nil, []string{"object " + oid + " is not stored in " + s.repo})
	}
	defer file.Close()
	if err := s.out.text("status 200"); err != nil {
		return err
	}
	if err := s.out.text("size=" + strconv.FormatInt(size, 10)); err != nil {
		return err
	}
	if err := s.out.delim(); err != nil {
		return err
	}
	buffer := make([]byte, serveLFSMaxPayload)
	remaining := size
	for remaining > 0 {
		chunk := buffer[:min(int64(len(buffer)), remaining)]
		if _, err := io.ReadFull(file, chunk); err != nil {
			return serveStateError(s.objectPath(oid), err)
		}
		if err := s.out.data(chunk); err != nil {
			return err
		}
		remaining -= int64(len(chunk))
	}
	return s.out.flush()
}

// openObject opens a stored object without following a symbolic link: the
// path is checked with Lstat before the open and compared with Fstat after
// it. It returns a nil file when nothing is stored under oid.
func (s *serveLFSTransfer) openObject(oid string) (*os.File, int64, error) {
	before, present, err := s.storedObject(oid)
	if err != nil || !present {
		return nil, 0, err
	}
	path := s.objectPath(oid)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, serveStateError(path, err)
	}
	after, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, serveStateError(path, err)
	}
	if !os.SameFile(before, after) || !after.Mode().IsRegular() {
		_ = file.Close()
		return nil, 0, serveDefect(path, "changed while it was being opened")
	}
	return file, after.Size(), nil
}

// T089: quota.

// quotaAfter returns the repository's quota and the stored bytes that
// adding the given bytes would produce. Staged files never count.
func (s *serveLFSTransfer) quotaAfter(additional int64) (limit, measured int64, err error) {
	config, err := loadServeRepoConfig(s.layout, s.repo)
	if err != nil {
		return 0, 0, err
	}
	stored, err := serveLFSStoredBytes(s.layout, s.repo)
	if err != nil {
		return 0, 0, err
	}
	return config.LFSQuotaBytes, serveLFSAddBytes(stored, additional), nil
}

// checkQuota answers status 507 and returns true when adding the given bytes
// would exceed the inclusive quota.
func (s *serveLFSTransfer) checkQuota(additional int64) (bool, error) {
	limit, measured, err := s.quotaAfter(additional)
	if err != nil {
		return false, err
	}
	if measured <= limit {
		return false, nil
	}
	return true, s.respondQuota(limit, measured)
}

func (s *serveLFSTransfer) respondQuota(limit, measured int64) error {
	return s.respondRejection(507, serveRejection{
		Ref:      "-",
		Actor:    s.actor,
		Code:     serveReasonLFSQuota,
		Budget:   serveReasonLFSQuota,
		Limit:    limit,
		Measured: measured,
		Next:     serveReasonDefaultNext(serveReasonLFSQuota),
	})
}

// respondRejection answers status with the first line of the hn-serve/1
// rejection, the one a client can parse.
func (s *serveLFSTransfer) respondRejection(status int, rejection serveRejection) error {
	line, _, _ := strings.Cut(renderServeRejection(rejection), "\n")
	return s.respond(status, nil, []string{line})
}
