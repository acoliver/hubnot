package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const protocolVersion = "hn/0"

type Event struct {
	Protocol     string   `json:"protocol"`
	Kind         string   `json:"kind"`
	Actor        string   `json:"actor"`
	ActorName    string   `json:"actorName"`
	PublicKey    string   `json:"publicKey"`
	Sequence     uint64   `json:"sequence"`
	Timestamp    string   `json:"timestamp"`
	Previous     string   `json:"previous,omitempty"`
	Subject      string   `json:"subject,omitempty"`
	Relationship string   `json:"relationship,omitempty"`
	TargetActor  string   `json:"targetActor,omitempty"`
	TargetKey    string   `json:"targetKey,omitempty"`
	Title        string   `json:"title,omitempty"`
	Body         string   `json:"body,omitempty"`
	Base         string   `json:"base,omitempty"`
	Head         string   `json:"head,omitempty"`
	Verdict      string   `json:"verdict,omitempty"`
	Pipeline     string   `json:"pipeline,omitempty"`
	Definition   string   `json:"definition,omitempty"`
	Commit       string   `json:"commit,omitempty"`
	Outcome      string   `json:"outcome,omitempty"`
	ExitCode     int      `json:"exitCode,omitempty"`
	DurationMS   int64    `json:"durationMs,omitempty"`
	Log          string   `json:"log,omitempty"`
	Backend      string   `json:"backend,omitempty"`
	Platform     string   `json:"platform,omitempty"`
	Runner       string   `json:"runner,omitempty"`
	Policy       string   `json:"policy,omitempty"`
	Evidence     []string `json:"evidence,omitempty"`
	// Rule is a canonical rule claim of a proposal.merged event. It is set
	// only by verifyEvent from the signed bytes and only written by
	// encodeAndSign, never by encoding/json, so legacy payloads keep their
	// meaning (decision 01M5VSEHF1G2YQDQW7HPVBF95K).
	Rule string `json:"-"`

	Issue     *IssueState `json:"issue,omitempty"`
	Parents   []string    `json:"parents,omitempty"`
	Operation string      `json:"operation,omitempty"`
	Request   string      `json:"request,omitempty"`
	Intent    string      `json:"intent,omitempty"`
}

type StoredEvent struct {
	ID          string
	Commit      string
	Event       Event
	Payload     []byte
	Signature   []byte
	Attachments map[string][]byte
}

func eventID(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newEvent(identity *Identity, kind string, sequence uint64, previous string) Event {
	return Event{
		Protocol:  protocolVersion,
		Kind:      kind,
		Actor:     identity.Actor,
		ActorName: identity.Name,
		PublicKey: identity.PublicKey,
		Sequence:  sequence,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Previous:  previous,
	}
}

func encodeAndSign(event Event, identity *Identity) ([]byte, []byte, error) {
	if event.Protocol != protocolVersion || event.Actor != identity.Actor || event.PublicKey != identity.PublicKey {
		return nil, nil, fmt.Errorf("event identity does not match its signer")
	}
	if err := validateEventContent(event); err != nil {
		return nil, nil, err
	}
	payload, err := marshalEvent(event)
	if err != nil {
		return nil, nil, err
	}
	privateKey, err := identity.privateKey()
	if err != nil {
		return nil, nil, err
	}
	signature := ed25519.Sign(privateKey, payload)
	return payload, signature, nil
}

// marshalEvent encodes an event exactly as json.Marshal does and, only when
// a rule claim is set, appends the canonical "rule" key after every other
// field.
func marshalEvent(event Event) ([]byte, error) {
	if event.Rule == "" {
		return json.Marshal(event)
	}
	wire := struct {
		Event
		Rule string `json:"rule"`
	}{event, event.Rule}
	return json.Marshal(wire)
}

// parseMergeRuleClaim returns the canonical rule claim of a proposal.merged
// payload: exactly one top-level key equal to "rule" under case folding,
// spelled exactly "rule" after JSON unescaping, whose value is a JSON string
// that is a valid rule name. Every other shape, including malformed JSON, is
// no claim; it is never a refusal, so payloads that verified before rule
// claims existed keep their verdict (decision 01M5VSEHEVA82N9T64ZH1P1Q2A).
//
// verifyEvent calls it on every proposal.merged after json.Unmarshal has
// accepted the payload, so it walks the top level once and skips values by
// counting depth instead of decoding them.
func parseMergeRuleClaim(payload []byte) string {
	i := skipJSONSpace(payload, 0)
	if i >= len(payload) || payload[i] != '{' {
		return ""
	}
	i = skipJSONSpace(payload, i+1)
	if i < len(payload) && payload[i] == '}' {
		return ""
	}
	matches := 0
	claim := ""
	for {
		keyEnd := skipJSONString(payload, i)
		if keyEnd < 0 {
			return ""
		}
		key := payload[i:keyEnd]
		i = skipJSONSpace(payload, keyEnd)
		if i >= len(payload) || payload[i] != ':' {
			return ""
		}
		valueStart := skipJSONSpace(payload, i+1)
		valueEnd := skipJSONValue(payload, valueStart)
		if valueEnd < 0 {
			return ""
		}
		if name, ok := jsonKeyFoldsToRule(key); ok {
			matches++
			if name == "rule" && payload[valueStart] == '"' {
				if err := json.Unmarshal(payload[valueStart:valueEnd], &claim); err != nil {
					return ""
				}
			}
		}
		i = skipJSONSpace(payload, valueEnd)
		if i >= len(payload) {
			return ""
		}
		if payload[i] == '}' {
			break
		}
		if payload[i] != ',' {
			return ""
		}
		i = skipJSONSpace(payload, i+1)
	}
	if skipJSONSpace(payload, i+1) != len(payload) || matches != 1 || !validPipelineName(claim) {
		return ""
	}
	return claim
}

// jsonKeyFoldsToRule reports whether a quoted JSON key equals "rule" under
// case folding and returns the unescaped key. Only ASCII letters fold to the
// letters of "rule", so a key without escapes is compared byte by byte.
func jsonKeyFoldsToRule(quoted []byte) (string, bool) {
	raw := quoted[1 : len(quoted)-1]
	if bytes.IndexByte(raw, '\\') < 0 {
		if len(raw) != len("rule") || !bytes.EqualFold(raw, []byte("rule")) {
			return "", false
		}
		return string(raw), true
	}
	var key string
	if err := json.Unmarshal(quoted, &key); err != nil || !strings.EqualFold(key, "rule") {
		return "", false
	}
	return key, true
}

func skipJSONSpace(data []byte, i int) int {
	for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\n' || data[i] == '\r') {
		i++
	}
	return i
}

// skipJSONString returns the index after the string starting at i, or -1.
func skipJSONString(data []byte, i int) int {
	if i >= len(data) || data[i] != '"' {
		return -1
	}
	for i++; i < len(data); i++ {
		switch data[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return -1
}

// skipJSONValue returns the index after the value starting at i, or -1. It
// skips nested objects and arrays by depth; it does not validate them.
func skipJSONValue(data []byte, i int) int {
	if i >= len(data) {
		return -1
	}
	switch data[i] {
	case '"':
		return skipJSONString(data, i)
	case '{', '[':
		depth := 0
		for i < len(data) {
			switch data[i] {
			case '"':
				i = skipJSONString(data, i)
				if i < 0 {
					return -1
				}
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
			i++
		}
		return -1
	}
	start := i
	for i < len(data) && data[i] != ',' && data[i] != '}' && data[i] != ']' && data[i] != ' ' && data[i] != '\t' && data[i] != '\n' && data[i] != '\r' {
		i++
	}
	if i == start {
		return -1
	}
	return i
}

func verifyEvent(payload, signature []byte) (Event, string, error) {
	var event Event
	if err := json.Unmarshal(payload, &event); err != nil {
		return Event{}, "", fmt.Errorf("invalid event JSON: %w", err)
	}
	if isResourceKind(event.Kind) {
		if err := strictResourceJSON(payload, &event, resourcePayloadLimit(resourceFamily(event.Kind))); err != nil {
			return Event{}, "", fmt.Errorf("invalid resource event JSON: %w", err)
		}
	}
	// Bound the authenticated bytes, not a re-encoding which can omit unknown
	// fields and whitespace. Legacy signed issues retain their original bounds.
	if len(payload) > maxIssuePayload && isIssueKind(event.Kind) && (event.Kind == "issue.revise" || hasIssueFields(event)) {
		return Event{}, "", fmt.Errorf("issue payload exceeds %d bytes", maxIssuePayload)
	}
	if event.Protocol != protocolVersion {
		return Event{}, "", fmt.Errorf("unsupported protocol %q", event.Protocol)
	}
	publicBytes, err := base64.RawStdEncoding.DecodeString(event.PublicKey)
	if err != nil || len(publicBytes) != ed25519.PublicKeySize {
		return Event{}, "", fmt.Errorf("invalid event public key")
	}
	publicKey := ed25519.PublicKey(publicBytes)
	if actorForPublicKey(publicKey) != event.Actor {
		return Event{}, "", fmt.Errorf("event actor does not match public key")
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		return Event{}, "", fmt.Errorf("invalid event signature")
	}
	if event.Sequence == 0 {
		return Event{}, "", fmt.Errorf("event sequence must be positive")
	}
	if _, err := time.Parse(time.RFC3339Nano, event.Timestamp); err != nil {
		return Event{}, "", fmt.Errorf("invalid event timestamp")
	}
	if err := validateEventContent(event); err != nil {
		return Event{}, "", err
	}
	if event.Kind == "proposal.merged" {
		event.Rule = parseMergeRuleClaim(payload)
	}
	return event, eventID(payload), nil
}

func validateEventContent(event Event) error {
	// Only an event being built for signing reaches the rule checks with a
	// rule set: verifyEvent sets a valid rule on proposal.merged after this
	// validation, so they bind what Hubnot signs, never what verifiers accept.
	if event.Rule != "" && event.Kind != "proposal.merged" {
		return fmt.Errorf("rule is only valid on proposal.merged")
	}
	if isResourceKind(event.Kind) {
		if event.Previous != "" && !validEventID(event.Previous) {
			return fmt.Errorf("invalid previous event ID")
		}
		return validateResourceEvent(event)
	}
	if err := validateIssueEvent(event); err != nil {
		return err
	}
	if event.Previous != "" && !validEventID(event.Previous) {
		return fmt.Errorf("invalid previous event ID")
	}
	switch event.Kind {
	case "issue.open":
		if strings.TrimSpace(event.Title) == "" {
			return fmt.Errorf("issue title cannot be empty")
		}
	case "issue.revise":
		// The complete issue-specific shape is validated above.
	case "issue.comment":
		if !validEventID(event.Subject) || strings.TrimSpace(event.Body) == "" {
			return fmt.Errorf("issue comment requires a subject and body")
		}
	case "proposal.open":
		if strings.TrimSpace(event.Title) == "" || !validGitOID(event.Base) || !validGitOID(event.Head) || event.Base == event.Head {
			return fmt.Errorf("proposal requires a title and distinct Git base/head commits")
		}
	case "proposal.revise":
		if !validEventID(event.Subject) || !validGitOID(event.Base) || !validGitOID(event.Head) || event.Base == event.Head {
			return fmt.Errorf("proposal revision requires a predecessor and distinct Git base/head commits")
		}
		if event.Title != "" {
			return fmt.Errorf("proposal revision inherits its title from its predecessor")
		}
	case "review.submit":
		if !validEventID(event.Subject) || (event.Verdict != "approve" && event.Verdict != "request-changes") {
			return fmt.Errorf("review requires a proposal subject and valid verdict")
		}
	case "run.request":
		if !validEventID(event.Subject) || !validPipelineName(event.Pipeline) || !validEventID(event.Definition) || !validGitOID(event.Commit) {
			return fmt.Errorf("run request requires a proposal, pipeline digest, and commit")
		}
	case "run.result":
		if !validEventID(event.Subject) || !validPipelineName(event.Pipeline) || !validEventID(event.Definition) || !validGitOID(event.Commit) {
			return fmt.Errorf("run result does not match a valid request shape")
		}
		if (event.Outcome != "passed" && event.Outcome != "failed") || !validEventID(event.Log) || event.DurationMS < 0 {
			return fmt.Errorf("run result requires an outcome, duration, and log digest")
		}
		if (event.Backend != "sandbox" && event.Backend != "host") || strings.TrimSpace(event.Platform) == "" || strings.TrimSpace(event.Runner) == "" {
			return fmt.Errorf("run result requires a supported backend, platform, and runner")
		}
	case "proposal.decision":
		if !validEventID(event.Subject) || !validEventID(event.Policy) || (event.Verdict != "accept" && event.Verdict != "reject") {
			return fmt.Errorf("proposal decision requires a proposal, policy digest, and verdict")
		}
		if event.Verdict == "reject" && strings.TrimSpace(event.Body) == "" {
			return fmt.Errorf("proposal rejection requires an explanation")
		}
		if !validEvidenceIDs(event.Evidence) {
			return fmt.Errorf("proposal decision has invalid evidence IDs")
		}
	case "proposal.merged":
		if !validEventID(event.Subject) || !validEventID(event.Policy) || !validGitOID(event.Commit) || !validGitOID(event.Head) || !validEvidenceIDs(event.Evidence) {
			return fmt.Errorf("proposal merge requires a proposal, policy, head, commit, and decision evidence")
		}
		if event.Rule != "" && !validPipelineName(event.Rule) {
			return fmt.Errorf("proposal merge rule must be a valid rule name")
		}
	case "identity.authorize":
		if event.Relationship != identityRelationshipDevice && event.Relationship != identityRelationshipSuccessor {
			return fmt.Errorf("identity authorization requires relationship device or successor")
		}
		if !validActorFingerprint(event.TargetActor) {
			return fmt.Errorf("identity authorization requires a full target actor")
		}
		targetKey, err := base64.RawStdEncoding.DecodeString(event.TargetKey)
		if err != nil || len(targetKey) != ed25519.PublicKeySize {
			return fmt.Errorf("identity authorization has an invalid target public key")
		}
		if actorForPublicKey(ed25519.PublicKey(targetKey)) != event.TargetActor {
			return fmt.Errorf("identity authorization target actor does not match target public key")
		}
		if event.TargetActor == event.Actor {
			return fmt.Errorf("identity authorization cannot target its signer")
		}
	case "identity.accept":
		if !validEventID(event.Subject) {
			return fmt.Errorf("identity acceptance requires a full authorization event ID")
		}
	default:
		return fmt.Errorf("unsupported event kind %q", event.Kind)
	}
	return nil
}

func isProposalKind(kind string) bool {
	return kind == "proposal.open" || kind == "proposal.revise"
}

func validEvidenceIDs(evidence []string) bool {
	seen := make(map[string]bool)
	for _, id := range evidence {
		if !validEventID(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func validEventID(id string) bool {
	if !strings.HasPrefix(id, "sha256:") {
		return false
	}
	encoded := strings.TrimPrefix(id, "sha256:")
	decoded, err := hex.DecodeString(encoded)
	return err == nil && len(decoded) == sha256.Size
}

func validGitOID(id string) bool {
	decoded, err := hex.DecodeString(id)
	return err == nil && (len(decoded) == 20 || len(decoded) == 32)
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
