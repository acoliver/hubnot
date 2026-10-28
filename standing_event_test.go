package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode"
)

// standingEventLegacyEvent is the Event definition before rule claims
// existed (d881d9b). The legacy audit decodes every payload into it and
// requires verifyEvent to report the same fields.
type standingEventLegacyEvent struct {
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

	Issue     *IssueState `json:"issue,omitempty"`
	Parents   []string    `json:"parents,omitempty"`
	Operation string      `json:"operation,omitempty"`
	Request   string      `json:"request,omitempty"`
	Intent    string      `json:"intent,omitempty"`
}

const (
	standingEventBaseOID   = "1111111111111111111111111111111111111111"
	standingEventHeadOID   = "2222222222222222222222222222222222222222"
	standingEventResultOID = "3333333333333333333333333333333333333333"
)

// standingEventSignRaw signs raw payload bytes with the identity's key, so a
// test can build payloads that encodeAndSign would never write.
func standingEventSignRaw(t testing.TB, identity *Identity, payload []byte) []byte {
	t.Helper()
	privateKey, err := identity.privateKey()
	if err != nil {
		t.Fatal(err)
	}
	return ed25519.Sign(privateKey, payload)
}

// standingEventWithMembers appends top-level members before the closing
// brace of a signed payload.
func standingEventWithMembers(payload []byte, members string) []byte {
	trimmed := bytes.TrimRight(payload, " \t\r\n")
	raw := append([]byte{}, trimmed[:len(trimmed)-1]...)
	raw = append(raw, ',')
	raw = append(raw, members...)
	return append(raw, '}')
}

func standingEventMerge(identity *Identity) Event {
	event := newEvent(identity, "proposal.merged", 7, eventID([]byte("previous")))
	event.Subject = eventID([]byte("candidate"))
	event.Head = standingEventHeadOID
	event.Commit = standingEventResultOID
	event.Policy = eventID([]byte("policy"))
	event.Evidence = []string{eventID([]byte("approval")), eventID([]byte("run result"))}
	return event
}

func standingEventResource(t *testing.T, identity *Identity) Event {
	t.Helper()
	event := newEvent(identity, "project.open", 1, "")
	event.Intent = "open"
	event.Body = string(resourceState(t, "project", projectState()))
	return event
}

// standingEventSigned is a fixture event together with the identity that
// signs it.
type standingEventSigned struct {
	event  Event
	signer *Identity
}

// standingEventEveryKind returns one event of every non-resource kind
// Hubnot writes, with the optional fields of that kind set, and its signer.
func standingEventEveryKind(t *testing.T, identity *Identity) []standingEventSigned {
	t.Helper()
	target := testIdentity(t, "Device")
	subject := eventID([]byte("subject"))
	previous := eventID([]byte("previous"))
	var events []Event

	legacyOpen := newEvent(identity, "issue.open", 1, "")
	legacyOpen.Title = "Legacy issue"
	legacyOpen.Body = "body"
	events = append(events, legacyOpen)

	richOpen := newEvent(identity, "issue.open", 2, previous)
	richOpen.Title = "Rich issue"
	richOpen.Body = "description"
	state, err := CanonicalIssueState(IssueState{Title: richOpen.Title, Body: richOpen.Body, Status: "open", Criteria: []IssueCriterion{{ID: "c1", Text: "works"}}, Labels: []string{"b", "a"}, Assignees: []string{"human"}, Metadata: map[string]string{"example/context": "value"}})
	if err != nil {
		t.Fatal(err)
	}
	richOpen.Issue = &state
	richOpen.Intent = "open"
	richOpen.Operation = "request-open"
	if richOpen.Request, err = IssueRequestDigest(richOpen); err != nil {
		t.Fatal(err)
	}
	events = append(events, richOpen)

	revise := newEvent(identity, "issue.revise", 3, previous)
	revise.Subject = subject
	revise.Parents = []string{subject}
	revise.Intent = "revise"
	revised := state
	revised.Title = "Revised"
	revise.Issue = &revised
	revise.Operation = "request-revise"
	if revise.Request, err = IssueRequestDigest(revise); err != nil {
		t.Fatal(err)
	}
	events = append(events, revise)

	comment := newEvent(identity, "issue.comment", 4, previous)
	comment.Subject = subject
	comment.Body = "a comment"
	events = append(events, comment)

	open := newEvent(identity, "proposal.open", 5, previous)
	open.Title = "Proposal"
	open.Body = "details"
	open.Base = standingEventBaseOID
	open.Head = standingEventHeadOID
	events = append(events, open)

	proposalRevise := newEvent(identity, "proposal.revise", 6, previous)
	proposalRevise.Subject = subject
	proposalRevise.Body = "revision"
	proposalRevise.Base = standingEventBaseOID
	proposalRevise.Head = standingEventResultOID
	events = append(events, proposalRevise)

	review := newEvent(identity, "review.submit", 7, previous)
	review.Subject = subject
	review.Verdict = "approve"
	review.Body = "looks good"
	events = append(events, review)

	request := newEvent(identity, "run.request", 8, previous)
	request.Subject = subject
	request.Pipeline = "test"
	request.Definition = eventID([]byte("pipeline"))
	request.Commit = standingEventHeadOID
	events = append(events, request)

	result := newEvent(identity, "run.result", 9, previous)
	result.Subject = eventID([]byte("request"))
	result.Pipeline = "test"
	result.Definition = eventID([]byte("pipeline"))
	result.Commit = standingEventHeadOID
	result.Outcome = "failed"
	result.ExitCode = 2
	result.DurationMS = 1500
	result.Log = eventID([]byte("log"))
	result.Backend = "sandbox"
	result.Platform = "linux/amd64"
	result.Runner = "runner-1"
	events = append(events, result)

	decision := newEvent(identity, "proposal.decision", 10, previous)
	decision.Subject = subject
	decision.Policy = eventID([]byte("policy"))
	decision.Verdict = "reject"
	decision.Body = "needs work"
	decision.Evidence = []string{eventID([]byte("review"))}
	events = append(events, decision)

	merged := standingEventMerge(identity)
	merged.Body = "merged"
	events = append(events, merged)

	authorize := newEvent(identity, "identity.authorize", 12, previous)
	authorize.Relationship = identityRelationshipDevice
	authorize.TargetActor = target.Actor
	authorize.TargetKey = target.PublicKey
	events = append(events, authorize)

	signed := make([]standingEventSigned, 0, len(events)+1)
	for _, event := range events {
		signed = append(signed, standingEventSigned{event, identity})
	}
	accept := newEvent(target, "identity.accept", 1, "")
	accept.Subject = eventID([]byte("authorization"))
	return append(signed, standingEventSigned{accept, target})
}

func standingEventTopLevelKeys(t testing.TB, payload []byte) []string {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		t.Fatalf("payload is not an object: %s", payload)
	}
	var keys []string
	var skipped json.RawMessage
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, token.(string))
		if err := decoder.Decode(&skipped); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

// standingEventAssertLegacyFields requires every field of the pre-mission
// Event definition to decode to the same value in got.
func standingEventAssertLegacyFields(t testing.TB, label string, got Event, payload []byte) {
	t.Helper()
	var legacy standingEventLegacyEvent
	if err := json.Unmarshal(payload, &legacy); err != nil {
		t.Fatalf("%s: legacy decode: %v", label, err)
	}
	legacyValue := reflect.ValueOf(legacy)
	gotValue := reflect.ValueOf(got)
	for i := 0; i < legacyValue.NumField(); i++ {
		name := legacyValue.Type().Field(i).Name
		field := gotValue.FieldByName(name)
		if !field.IsValid() {
			t.Fatalf("%s: Event lost field %s", label, name)
		}
		if !reflect.DeepEqual(field.Interface(), legacyValue.Field(i).Interface()) {
			t.Fatalf("%s: field %s = %#v, legacy decode %#v", label, name, field.Interface(), legacyValue.Field(i).Interface())
		}
	}
}

func TestStandingEventFieldsUnchangedExceptRule(t *testing.T) {
	legacy := reflect.TypeOf(standingEventLegacyEvent{})
	current := reflect.TypeOf(Event{})
	if current.NumField() != legacy.NumField()+1 {
		t.Fatalf("Event has %d fields, want the %d legacy fields plus Rule", current.NumField(), legacy.NumField())
	}
	offset := 0
	for i := 0; i < current.NumField(); i++ {
		field := current.Field(i)
		if field.Name == "Rule" {
			if field.Tag.Get("json") != "-" || field.Type.Kind() != reflect.String {
				t.Fatalf("Rule must be a string tagged json:\"-\", got %s %q", field.Type, field.Tag)
			}
			offset = 1
			continue
		}
		want := legacy.Field(i - offset)
		if field.Name != want.Name || field.Type != want.Type || field.Tag != want.Tag {
			t.Fatalf("field %d = %s %s %q, want %s %s %q", i, field.Name, field.Type, field.Tag, want.Name, want.Type, want.Tag)
		}
	}
	if offset != 1 {
		t.Fatal("Event has no Rule field")
	}
}

func TestStandingEventEncodingUnchangedWithoutRule(t *testing.T) {
	identity := testIdentity(t, "Alice")
	for _, fixture := range standingEventEveryKind(t, identity) {
		event := fixture.event
		payload, signature, err := encodeAndSign(event, fixture.signer)
		if err != nil {
			t.Fatalf("%s: %v", event.Kind, err)
		}
		want, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(payload, want) {
			t.Fatalf("%s: payload changed:\n got %s\nwant %s", event.Kind, payload, want)
		}
		for _, key := range standingEventTopLevelKeys(t, payload) {
			if strings.EqualFold(key, "rule") {
				t.Fatalf("%s: payload has key %q: %s", event.Kind, key, payload)
			}
		}
		got, id, err := verifyEvent(payload, signature)
		if err != nil {
			t.Fatalf("%s: verify: %v", event.Kind, err)
		}
		if got.Rule != "" || id != eventID(payload) {
			t.Fatalf("%s: rule %q id %s", event.Kind, got.Rule, id)
		}
	}
}

func TestStandingEventEveryKindIsCovered(t *testing.T) {
	alice := testIdentity(t, "Alice")
	kinds := map[string]bool{}
	for _, fixture := range standingEventEveryKind(t, alice) {
		kinds[fixture.event.Kind] = true
	}
	for _, kind := range []string{"issue.open", "issue.revise", "issue.comment", "proposal.open", "proposal.revise", "review.submit", "run.request", "run.result", "proposal.decision", "proposal.merged", "identity.authorize", "identity.accept"} {
		if !kinds[kind] {
			t.Fatalf("fixture set lacks %s", kind)
		}
	}
}

func TestStandingEventCanonicalClaimRoundTrip(t *testing.T) {
	identity := testIdentity(t, "Executor")
	for _, name := range []string{"ordinary", strings.Repeat("r", 64), "docs-only_v1.2"} {
		event := standingEventMerge(identity)
		event.Rule = name
		payload, signature, err := encodeAndSign(event, identity)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		suffix := `,"rule":"` + name + `"}`
		if !bytes.HasSuffix(payload, []byte(suffix)) {
			t.Fatalf("%s: payload does not end with %s: %s", name, suffix, payload)
		}
		event.Rule = ""
		withoutRule, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if got := append(append([]byte{}, payload[:len(payload)-len(suffix)]...), '}'); !bytes.Equal(got, withoutRule) {
			t.Fatalf("%s: fields before rule changed:\n got %s\nwant %s", name, got, withoutRule)
		}
		got, id, err := verifyEvent(payload, signature)
		if err != nil {
			t.Fatalf("%s: verify: %v", name, err)
		}
		if got.Rule != name || id != eventID(payload) {
			t.Fatalf("%s: rule %q id %s", name, got.Rule, id)
		}
		standingEventAssertLegacyFields(t, name, got, payload)
	}
}

// standingEventNoClaimMembers are proposal.merged members that are never a
// canonical rule claim (T013 step 3).
var standingEventNoClaimMembers = []string{
	`"rule":""`,
	`"rule":null`,
	`"rule":1`,
	`"rule":true`,
	`"rule":["ordinary"]`,
	`"rule":{"x":1}`,
	`"rule":{"rule":"ordinary"}`,
	`"RULE":"ordinary"`,
	`"Rule":"ordinary"`,
	`"rule":"ordinary","RULE":"ordinary"`,
	`"RULE":"ordinary","rule":"ordinary"`,
	`"rule":"ordinary","rule":"ordinary"`,
	`"rule":"bad name"`,
	`"rule":"."`,
	`"rule":".."`,
	`"rule":"` + strings.Repeat("r", 65) + `"`,
	`"note":{"rule":"ordinary"}`,
}

func TestStandingEventNonCanonicalRuleIsNoClaim(t *testing.T) {
	identity := testIdentity(t, "Maintainer")
	base, baseSignature, err := encodeAndSign(standingEventMerge(identity), identity)
	if err != nil {
		t.Fatal(err)
	}
	want, _, err := verifyEvent(base, baseSignature)
	if err != nil {
		t.Fatal(err)
	}
	for _, members := range standingEventNoClaimMembers {
		raw := standingEventWithMembers(base, members)
		got, id, err := verifyEvent(raw, standingEventSignRaw(t, identity, raw))
		if err != nil {
			t.Fatalf("%s: refused: %v", members, err)
		}
		if id != eventID(raw) {
			t.Fatalf("%s: id %s, want %s", members, id, eventID(raw))
		}
		if got.Rule != "" {
			t.Fatalf("%s: claim %q", members, got.Rule)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: decoded fields differ from the payload without the key:\n got %#v\nwant %#v", members, got, want)
		}
	}
}

func TestStandingEventClaimRecognitionStaysAligned(t *testing.T) {
	identity := testIdentity(t, "Executor")
	base, _, err := encodeAndSign(standingEventMerge(identity), identity)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		members string
		want    string
	}{
		{`"note":[{"rule":"bad name"},["rule"]],"rule":"ordinary"`, "ordinary"},
		{`"rule":"ordinary","note":{"RULE":{"rule":"x"}}`, "ordinary"},
		{`"ru\u006ce":"ordinary"`, "ordinary"},
		{`"rule":"ordin\u0061ry"`, "ordinary"},
		{`"rule" : "ordinary" `, "ordinary"},
		{`"rule":"ordinary","note":"RULE"`, "ordinary"},
		{`"note":{"rule":"ordinary"}`, ""},
		{`"r\u0055le":"ordinary"`, ""},
	} {
		raw := standingEventWithMembers(base, test.members)
		if got := parseMergeRuleClaim(raw); got != test.want {
			t.Fatalf("%s: claim %q, want %q", test.members, got, test.want)
		}
		got, _, err := verifyEvent(raw, standingEventSignRaw(t, identity, raw))
		if err != nil {
			t.Fatalf("%s: refused: %v", test.members, err)
		}
		if got.Rule != test.want {
			t.Fatalf("%s: verified claim %q, want %q", test.members, got.Rule, test.want)
		}
	}
	for _, malformed := range []string{``, `[]`, `"rule"`, `{"rule":"ordinary"`, `{"rule":}`} {
		if got := parseMergeRuleClaim([]byte(malformed)); got != "" {
			t.Fatalf("%q: claim %q from malformed input", malformed, got)
		}
	}
}

// standingEventLegacyKindCase is a raw signed payload of another kind with
// its declared outcome from the contract's legacy history fixtures.
type standingEventLegacyKindCase struct {
	label   string
	payload []byte
	wantErr string
}

func standingEventLegacyKindCases(t *testing.T, identity *Identity) []standingEventLegacyKindCase {
	t.Helper()
	events := map[string]Event{}
	for _, fixture := range standingEventEveryKind(t, identity) {
		if _, ok := events[fixture.event.Kind]; !ok && fixture.signer == identity {
			events[fixture.event.Kind] = fixture.event
		}
	}
	signed := func(event Event) []byte {
		payload, _, err := encodeAndSign(event, identity)
		if err != nil {
			t.Fatalf("%s: %v", event.Kind, err)
		}
		return payload
	}
	resource := signed(standingEventResource(t, identity))
	return []standingEventLegacyKindCase{
		{"L01 review.submit rule annotation", standingEventWithMembers(signed(events["review.submit"]), `"rule":"annotation"`), ""},
		{"L02 run.request RULE", standingEventWithMembers(signed(events["run.request"]), `"RULE":"x"`), ""},
		{"L02 issue.comment rule number", standingEventWithMembers(signed(events["issue.comment"]), `"rule":1`), ""},
		{"review.submit canonical-looking rule", standingEventWithMembers(signed(events["review.submit"]), `"rule":"ordinary"`), ""},
		{"identity.authorize rule", standingEventWithMembers(signed(events["identity.authorize"]), `"rule":"ordinary"`), ""},
		{"L06 resource without rule", resource, ""},
		{"L06 resource with rule", standingEventWithMembers(resource, `"rule":"ordinary"`), `invalid resource event JSON: unknown field "rule"`},
	}
}

func TestStandingEventLegacyOtherKinds(t *testing.T) {
	identity := testIdentity(t, "Reviewer")
	for _, test := range standingEventLegacyKindCases(t, identity) {
		got, id, err := verifyEvent(test.payload, standingEventSignRaw(t, identity, test.payload))
		if test.wantErr != "" {
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("%s: error %v, want %q", test.label, err, test.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: refused: %v", test.label, err)
		}
		if got.Rule != "" || id != eventID(test.payload) {
			t.Fatalf("%s: rule %q id %s", test.label, got.Rule, id)
		}
		standingEventAssertLegacyFields(t, test.label, got, test.payload)
	}
}

func TestStandingEventSigningRefusesNonCanonicalRule(t *testing.T) {
	identity := testIdentity(t, "Signer")
	review := newEvent(identity, "review.submit", 1, "")
	review.Subject = eventID([]byte("proposal"))
	review.Verdict = "approve"
	review.Rule = "ordinary"
	resource := standingEventResource(t, identity)
	resource.Rule = "ordinary"
	for _, test := range []struct {
		label string
		event Event
		want  string
	}{
		{"review.submit", review, "rule is only valid on proposal.merged"},
		{"resource", resource, "rule is only valid on proposal.merged"},
	} {
		if _, _, err := encodeAndSign(test.event, identity); err == nil || err.Error() != test.want {
			t.Fatalf("%s: error %v, want %q", test.label, err, test.want)
		}
	}
	for _, name := range []string{"bad name", ".", "..", strings.Repeat("r", 65), "ordinary\n", "règle"} {
		merge := standingEventMerge(identity)
		merge.Rule = name
		if _, _, err := encodeAndSign(merge, identity); err == nil || err.Error() != "proposal merge rule must be a valid rule name" {
			t.Fatalf("%q: error %v", name, err)
		}
	}
	invalidMerge := standingEventMerge(identity)
	invalidMerge.Evidence = []string{"not an id"}
	invalidMerge.Rule = "bad name"
	if _, _, err := encodeAndSign(invalidMerge, identity); err == nil || !strings.Contains(err.Error(), "proposal merge requires") {
		t.Fatalf("existing merge checks must run first, got %v", err)
	}
}

// TestStandingEventLegacyVerdictsUnchanged audits that no payload that
// verified before rule claims changes verdict, ID or decoded fields. The
// repository commits no event fixtures under testdata/, so the corpus is one
// event of every non-resource kind with its optional fields set, the raw
// proposal.merged payloads of T013 step 3 and the other-kind payloads of
// T013 step 4. The declared errors were confirmed against d881d9b.
func TestStandingEventLegacyVerdictsUnchanged(t *testing.T) {
	identity := testIdentity(t, "Auditor")
	type auditCase struct {
		label   string
		signer  *Identity
		payload []byte
		wantErr string
	}
	var corpus []auditCase
	for _, fixture := range standingEventEveryKind(t, identity) {
		payload, _, err := encodeAndSign(fixture.event, fixture.signer)
		if err != nil {
			t.Fatalf("%s: %v", fixture.event.Kind, err)
		}
		corpus = append(corpus, auditCase{fixture.event.Kind, fixture.signer, payload, ""})
	}
	merge, _, err := encodeAndSign(standingEventMerge(identity), identity)
	if err != nil {
		t.Fatal(err)
	}
	for _, members := range standingEventNoClaimMembers {
		corpus = append(corpus, auditCase{"proposal.merged " + members, identity, standingEventWithMembers(merge, members), ""})
	}
	for _, test := range standingEventLegacyKindCases(t, identity) {
		corpus = append(corpus, auditCase{test.label, identity, test.payload, test.wantErr})
	}

	for _, test := range corpus {
		got, id, err := verifyEvent(test.payload, standingEventSignRaw(t, test.signer, test.payload))
		if test.wantErr != "" {
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("%s: error %v, want %q", test.label, err, test.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: refused: %v", test.label, err)
		}
		if id != eventID(test.payload) {
			t.Fatalf("%s: id %s, want %s", test.label, id, eventID(test.payload))
		}
		if got.Rule != "" {
			t.Fatalf("%s: legacy payload gained claim %q", test.label, got.Rule)
		}
		standingEventAssertLegacyFields(t, test.label, got, test.payload)
	}
}

// standingEventReferenceClaim is the json.Decoder form of the claim walk
// that T012 describes. The byte walk in parseMergeRuleClaim replaced it for
// cost (T014) and must agree with it on every valid JSON payload.
func standingEventReferenceClaim(payload []byte) string {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return ""
	}
	var value json.RawMessage
	matches := 0
	claim := ""
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		key, ok := token.(string)
		if !ok {
			return ""
		}
		if err := decoder.Decode(&value); err != nil {
			return ""
		}
		if !strings.EqualFold(key, "rule") {
			continue
		}
		matches++
		if key == "rule" && len(value) > 0 && value[0] == '"' {
			if err := json.Unmarshal(value, &claim); err != nil {
				return ""
			}
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return ""
	}
	if matches != 1 || !validPipelineName(claim) {
		return ""
	}
	return claim
}

// standingEventClaimSeeds are payloads for the differential claim tests.
func standingEventClaimSeeds(t testing.TB) [][]byte {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	identity := &Identity{Actor: actorForPublicKey(publicKey), Name: "Seed", PublicKey: base64.RawStdEncoding.EncodeToString(publicKey), PrivateKey: base64.RawStdEncoding.EncodeToString(privateKey)}
	merge := standingEventMerge(identity)
	merge.Body = "line\nwith \"quotes\" and \\ and \u00e9 {[rule]}"
	base, _, err := encodeAndSign(merge, identity)
	if err != nil {
		t.Fatal(err)
	}
	seeds := [][]byte{base, []byte(`{}`), []byte(` { "rule" : "ordinary" } `), []byte(`{"a":-1.5e3,"b":[true,false,null],"rule":"x"}`)}
	for _, members := range append(append([]string{}, standingEventNoClaimMembers...), `"rule":"ordinary"`, `"ru\u006ce":"ordinary"`, `"note":[{"rule":"bad name"},["rule"]],"rule":"ordinary"`, `"r\u0055le":"ordinary"`, `"rule":"ordin\u0061ry"`, `"rule":"a\"b"`, `"\\":"\\","rule":"ordinary"`) {
		seeds = append(seeds, standingEventWithMembers(base, members))
	}
	return seeds
}

func TestStandingEventClaimWalkMatchesDecoderReference(t *testing.T) {
	for _, seed := range standingEventClaimSeeds(t) {
		if got, want := parseMergeRuleClaim(seed), standingEventReferenceClaim(seed); got != want {
			t.Fatalf("%s: byte walk %q, decoder reference %q", seed, got, want)
		}
	}
}

func TestStandingEventOnlyASCIIFoldsToRule(t *testing.T) {
	for _, letter := range "rule" {
		orbit := []rune{letter}
		for folded := unicode.SimpleFold(letter); folded != letter; folded = unicode.SimpleFold(folded) {
			orbit = append(orbit, folded)
		}
		if len(orbit) != 2 || orbit[1] != unicode.ToUpper(letter) {
			t.Fatalf("%q folds to %q; jsonKeyFoldsToRule compares unescaped keys as ASCII", letter, orbit)
		}
	}
}

func FuzzStandingEventRuleClaim(f *testing.F) {
	for _, seed := range standingEventClaimSeeds(f) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, payload []byte) {
		got := parseMergeRuleClaim(payload)
		if !json.Valid(payload) {
			return
		}
		if want := standingEventReferenceClaim(payload); got != want {
			t.Fatalf("%q: byte walk %q, decoder reference %q", payload, got, want)
		}
	})
}

func standingEventBenchmarkPayloads(b *testing.B) map[string][2][]byte {
	b.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}
	identity := &Identity{
		Actor:      actorForPublicKey(publicKey),
		Name:       "Executor",
		PublicKey:  base64.RawStdEncoding.EncodeToString(publicKey),
		PrivateKey: base64.RawStdEncoding.EncodeToString(privateKey),
	}
	payloads := map[string][2][]byte{}
	for label, rule := range map[string]string{"no-claim": "", "claim": "ordinary"} {
		event := standingEventMerge(identity)
		event.Rule = rule
		payload, signature, err := encodeAndSign(event, identity)
		if err != nil {
			b.Fatal(err)
		}
		payloads[label] = [2][]byte{payload, signature}
	}
	return payloads
}

func BenchmarkStandingEventRuleClaim(b *testing.B) {
	for _, label := range []string{"no-claim", "claim"} {
		payload := standingEventBenchmarkPayloads(b)[label][0]
		b.Run(label, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				parseMergeRuleClaim(payload)
			}
		})
	}
}

func BenchmarkStandingEventVerify(b *testing.B) {
	for _, label := range []string{"no-claim", "claim"} {
		signed := standingEventBenchmarkPayloads(b)[label]
		b.Run(label, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, err := verifyEvent(signed[0], signed[1]); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
