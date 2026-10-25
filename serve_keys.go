package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
)

// SSH-facing primitives for hn serve. OpenSSH authenticates every connection
// (C-002); hn serve only parses public key lines, computes their SHA256
// fingerprints and maintains one managed block in authorized_keys. The
// formats are specified in
// kitty-specs/hn-serve-01M5P0NP/contracts/server-state.md.

const (
	serveAuthorizedKeysBegin = "# BEGIN hn serve managed keys; edit with hn serve key"
	serveAuthorizedKeysEnd   = "# END hn serve managed keys"

	servePublicKeyLineLimit  = 16 << 10
	serveAuthorizedKeysLimit = 4 << 20
	serveActorCommentLength  = 12
)

// serveKeyTypeFields maps each supported key type to the number of
// length-prefixed strings its OpenSSH public key blob holds, counting the
// leading type name. Checking the count catches truncated blobs and trailing
// data without interpreting key material.
var serveKeyTypeFields = map[string]int{
	"ecdsa-sha2-nistp256":                3, // type, curve, point
	"ecdsa-sha2-nistp384":                3,
	"ecdsa-sha2-nistp521":                3,
	"sk-ecdsa-sha2-nistp256@openssh.com": 4, // type, curve, point, application
	"sk-ssh-ed25519@openssh.com":         3, // type, key, application
	"ssh-ed25519":                        2, // type, key
	"ssh-rsa":                            3, // type, exponent, modulus
}

// serveParsedKey is one validated OpenSSH public key.
type serveParsedKey struct {
	Type        string // e.g. "ssh-ed25519"
	Blob        []byte // decoded key blob
	Fingerprint string // "SHA256:..." as ssh-keygen -l -E sha256 prints it
	Canonical   string // "TYPE BASE64" with no options and no comment
}

func supportedServeKeyTypes() string {
	types := make([]string, 0, len(serveKeyTypeFields))
	for keyType := range serveKeyTypeFields {
		types = append(types, keyType)
	}
	sort.Strings(types)
	return strings.Join(types, ", ")
}

// parseServePublicKey accepts one OpenSSH public key line: an optional
// authorized_keys options field, the key type, the base64 blob and an
// optional comment. Options and comment never reach the result.
func parseServePublicKey(line string) (serveParsedKey, error) {
	if trimmed, ok := strings.CutSuffix(line, "\n"); ok {
		line = strings.TrimSuffix(trimmed, "\r")
	}
	if line == "" {
		return serveParsedKey{}, errors.New("SSH public key line is empty")
	}
	if len(line) > servePublicKeyLineLimit {
		return serveParsedKey{}, fmt.Errorf("SSH public key line is longer than %d bytes", servePublicKeyLineLimit)
	}
	for i := 0; i < len(line); i++ {
		if character := line[i]; character < 0x20 || character == 0x7f {
			return serveParsedKey{}, fmt.Errorf("SSH public key line contains control byte 0x%02x at offset %d; give exactly one key line with fields separated by spaces", character, i)
		}
	}
	keyType, rest := cutServeKeyField(line)
	if _, ok := serveKeyTypeFields[keyType]; !ok {
		afterOptions, err := skipServeKeyOptions(line)
		if err != nil {
			return serveParsedKey{}, err
		}
		named := keyType
		keyType, rest = cutServeKeyField(afterOptions)
		if _, ok := serveKeyTypeFields[keyType]; !ok {
			if strings.ContainsAny(named, "=,\"") && keyType != "" {
				named = keyType
			}
			return serveParsedKey{}, fmt.Errorf("unsupported SSH key type %q; supported types are %s", named, supportedServeKeyTypes())
		}
	}
	encoded, _ := cutServeKeyField(rest)
	if encoded == "" {
		return serveParsedKey{}, fmt.Errorf("SSH public key line has no base64 key after the type %s", keyType)
	}
	blob, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return serveParsedKey{}, fmt.Errorf("SSH %s key is not valid padded base64: %v", keyType, err)
	}
	if err := checkServeKeyBlob(keyType, blob); err != nil {
		return serveParsedKey{}, err
	}
	return serveParsedKey{
		Type:        keyType,
		Blob:        blob,
		Fingerprint: serveFingerprint(blob),
		Canonical:   keyType + " " + base64.StdEncoding.EncodeToString(blob),
	}, nil
}

// cutServeKeyField returns the first space-separated field of value and the
// text after it, skipping leading spaces.
func cutServeKeyField(value string) (string, string) {
	value = strings.TrimLeft(value, " ")
	field, rest, _ := strings.Cut(value, " ")
	return field, rest
}

// skipServeKeyOptions skips one authorized_keys options field the way
// OpenSSH's sshkey_advance_past_options does: a space ends the field unless
// it is inside double quotes, and \" inside the field does not toggle
// quoting.
func skipServeKeyOptions(line string) (string, error) {
	line = strings.TrimLeft(line, " ")
	quoted := false
	index := 0
	for ; index < len(line) && (quoted || line[index] != ' '); index++ {
		switch {
		case line[index] == '\\' && index+1 < len(line) && line[index+1] == '"':
			index++
		case line[index] == '"':
			quoted = !quoted
		}
	}
	if quoted {
		return "", errors.New("SSH public key line has an options field with an unterminated double quote")
	}
	return line[index:], nil
}

// checkServeKeyBlob walks the blob's uint32 big-endian length-prefixed
// strings (RFC 4253 section 6.6). The first must equal keyType, no length may
// overrun the blob, and the blob must hold exactly the strings its type
// defines.
func checkServeKeyBlob(keyType string, blob []byte) error {
	want := serveKeyTypeFields[keyType]
	fields := 0
	for offset := 0; offset < len(blob); fields++ {
		if fields == want {
			return fmt.Errorf("SSH %s key blob has %d trailing bytes after its %d fields", keyType, len(blob)-offset, want)
		}
		if len(blob)-offset < 4 {
			return fmt.Errorf("SSH %s key blob is truncated: %d bytes where a 4-byte length is required", keyType, len(blob)-offset)
		}
		length := binary.BigEndian.Uint32(blob[offset:])
		offset += 4
		if uint64(length) > uint64(len(blob)-offset) {
			return fmt.Errorf("SSH %s key blob is truncated: field %d claims %d bytes but %d remain", keyType, fields+1, length, len(blob)-offset)
		}
		if fields == 0 {
			if embedded := string(blob[offset : offset+int(length)]); embedded != keyType {
				return fmt.Errorf("SSH key type %s does not match the type %q inside its key blob", keyType, embedded)
			}
		}
		offset += int(length)
	}
	if fields != want {
		return fmt.Errorf("SSH %s key blob is truncated: %d of %d fields present", keyType, fields, want)
	}
	return nil
}

// serveFingerprint is the SHA256 fingerprint ssh-keygen -l -E sha256 prints:
// SHA256: and the unpadded standard base64 SHA-256 digest of the key blob.
func serveFingerprint(blob []byte) string {
	sum := sha256.Sum256(blob)
	return serveFingerprintPrefix + base64.RawStdEncoding.EncodeToString(sum[:])
}

// renderServeAuthorizedKeysBlock renders the managed block, markers
// included, with one restricted forced-command entry per binding in
// fingerprint order. Every stored public key is parsed again and must
// produce its binding's fingerprint, which catches an edited registry.
func renderServeAuthorizedKeysBlock(bindings []serveKeyBinding, hnPath string) ([]byte, error) {
	if _, err := checkServeExecutablePath(hnPath); err != nil {
		return nil, err
	}
	sorted := slices.Clone(bindings)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Fingerprint < sorted[j].Fingerprint })
	var block bytes.Buffer
	block.WriteString(serveAuthorizedKeysBegin + "\n")
	for index, binding := range sorted {
		if index > 0 && binding.Fingerprint == sorted[index-1].Fingerprint {
			return nil, fmt.Errorf("key registry binds fingerprint %s more than once", binding.Fingerprint)
		}
		if !validActorFingerprint(binding.Actor) {
			return nil, fmt.Errorf("fingerprint %s is bound to a malformed actor %q; want a full 64-hex actor fingerprint", binding.Fingerprint, binding.Actor)
		}
		parsed, err := parseServePublicKey(binding.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("fingerprint %s has an invalid public key: %w", binding.Fingerprint, err)
		}
		if parsed.Fingerprint != binding.Fingerprint {
			return nil, fmt.Errorf("key registry fingerprint %s does not match its public key, which has fingerprint %s", binding.Fingerprint, parsed.Fingerprint)
		}
		fmt.Fprintf(&block, "restrict,command=\"%s serve git --key %s\" %s hn-serve:%s\n",
			hnPath, binding.Fingerprint, parsed.Canonical, binding.Actor[:serveActorCommentLength])
	}
	block.WriteString(serveAuthorizedKeysEnd + "\n")
	return block.Bytes(), nil
}

// mergeServeAuthorizedKeys replaces the managed block in existing with block,
// or appends block when existing has none. Bytes outside the block are kept
// exactly, line terminators included; the only addition is the newline the
// contract requires before an appended block when existing does not end with
// one.
func mergeServeAuthorizedKeys(existing []byte, block []byte) ([]byte, error) {
	blockStart, blockStop := -1, -1
	line := 0
	for offset := 0; offset < len(existing); {
		line++
		next := len(existing)
		content := existing[offset:]
		if newline := bytes.IndexByte(content, '\n'); newline >= 0 {
			next = offset + newline + 1
			content = bytes.TrimSuffix(content[:newline], []byte("\r"))
		}
		switch string(content) {
		case serveAuthorizedKeysBegin:
			if blockStart >= 0 {
				return nil, fmt.Errorf("line %d is a second managed block begin marker", line)
			}
			blockStart = offset
		case serveAuthorizedKeysEnd:
			if blockStop >= 0 {
				return nil, fmt.Errorf("line %d is a second managed block end marker", line)
			}
			if blockStart < 0 {
				return nil, fmt.Errorf("line %d is a managed block end marker without a preceding begin marker", line)
			}
			blockStop = next
		}
		offset = next
	}
	if blockStart >= 0 && blockStop < 0 {
		return nil, errors.New("the managed block begin marker has no following end marker")
	}
	var merged bytes.Buffer
	if blockStart >= 0 {
		merged.Grow(blockStart + len(block) + len(existing) - blockStop)
		merged.Write(existing[:blockStart])
		merged.Write(block)
		merged.Write(existing[blockStop:])
		return merged.Bytes(), nil
	}
	merged.Grow(len(existing) + 1 + len(block))
	merged.Write(existing)
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		merged.WriteByte('\n')
	}
	merged.Write(block)
	return merged.Bytes(), nil
}

// writeServeAuthorizedKeys renders the managed block for bindings, merges it
// into $HOME/.ssh/authorized_keys and replaces that file atomically with mode
// 0600. Every check runs before the first write. An unsafe .ssh or
// authorized_keys is refused, never repaired, because a silent chmod would
// hide a misconfiguration sshd's StrictModes also rejects.
func writeServeAuthorizedKeys(layout serveLayout, bindings []serveKeyBinding, hnPath string) error {
	block, err := renderServeAuthorizedKeysBlock(bindings, hnPath)
	if err != nil {
		return err
	}
	sshDirExists, err := checkServeSSHDirectory(layout.SSHDir)
	if err != nil {
		return err
	}
	var existing []byte
	if sshDirExists {
		if existing, err = readServeAuthorizedKeys(layout.AuthorizedKeys); err != nil {
			return err
		}
	}
	merged, err := mergeServeAuthorizedKeys(existing, block)
	if err != nil {
		return serveDefect(layout.AuthorizedKeys, "%v; fix the managed block markers by hand", err)
	}
	if !sshDirExists {
		if err := createServeSSHDirectory(layout.SSHDir); err != nil {
			return err
		}
	}
	if err := replacePrivateFileAtomic(layout.AuthorizedKeys, merged); err != nil {
		return serveStateError(layout.AuthorizedKeys, err)
	}
	return nil
}

// checkServeSSHDirectory reports whether .ssh exists and refuses one that is
// a symlink, not a directory, or writable by group or others.
func checkServeSSHDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, serveDefect(path, "cannot inspect: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, serveDefect(path, "is a symbolic link; replace it with a real directory of mode 0700")
	}
	if !info.IsDir() {
		return false, serveDefect(path, "is not a directory")
	}
	if perm := info.Mode().Perm(); perm&0o022 != 0 {
		return false, serveDefect(path, "directory mode %04o is writable by group or others; run chmod 700 %s", perm, path)
	}
	return true, nil
}

// createServeSSHDirectory creates .ssh with mode 0700. A directory created
// concurrently by another command is checked like an existing one.
func createServeSSHDirectory(path string) error {
	err := os.Mkdir(path, 0o700)
	if errors.Is(err, os.ErrExist) {
		_, err := checkServeSSHDirectory(path)
		return err
	}
	if err != nil {
		return serveStateError(path, err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return serveStateError(path, err)
	}
	return nil
}

// readServeAuthorizedKeys returns the current file, or nil when it does not
// exist. It refuses a symlink, a non-regular file, a file writable by group
// or others and a file above serveAuthorizedKeysLimit, and confirms the file
// it opened is the one it inspected.
func readServeAuthorizedKeys(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, serveDefect(path, "cannot inspect: %v", err)
	}
	if err := checkServeAuthorizedKeysInfo(path, before); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, serveStateError(path, err)
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return nil, serveStateError(path, err)
	}
	if !os.SameFile(before, after) {
		return nil, serveDefect(path, "changed while it was being opened")
	}
	contents, err := io.ReadAll(io.LimitReader(file, serveAuthorizedKeysLimit+1))
	if err != nil {
		return nil, serveStateError(path, err)
	}
	if len(contents) > serveAuthorizedKeysLimit {
		return nil, serveDefect(path, "file is larger than %d bytes", serveAuthorizedKeysLimit)
	}
	return contents, nil
}

func checkServeAuthorizedKeysInfo(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return serveDefect(path, "is a symbolic link; replace it with a regular file of mode 0600")
	}
	if !info.Mode().IsRegular() {
		return serveDefect(path, "is not a regular file")
	}
	if perm := info.Mode().Perm(); perm&0o022 != 0 {
		return serveDefect(path, "file mode %04o is writable by group or others; run chmod 600 %s", perm, path)
	}
	if info.Size() > serveAuthorizedKeysLimit {
		return serveDefect(path, "file is larger than %d bytes", serveAuthorizedKeysLimit)
	}
	return nil
}
