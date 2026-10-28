package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// hn-serve/1 rejection vocabulary shared by the server hooks, the forced
// command and the clients that read git push stderr. docs/protocol-v0.md
// ("hn serve wire surface") documents every code in two tables; tests keep
// them equal to serveReasonCatalogue and serveRuleReasonCatalogue.

const (
	serveReasonUnknownKey                 = "unknown-key"
	serveReasonUnhostedRepository         = "unhosted-repository"
	serveReasonUnsupportedCommand         = "unsupported-command"
	serveReasonRepositoryBusy             = "repository-busy"
	serveReasonActorKeyMismatch           = "actor-key-mismatch"
	serveReasonActorChainInvalid          = "actor-chain-invalid"
	serveReasonActorChainRewrite          = "actor-chain-rewrite"
	serveReasonDependencyMissing          = "dependency-missing"
	serveReasonProtectedNoProposal        = "protected-no-proposal"
	serveReasonProtectedNotReady          = "protected-not-ready"
	serveReasonProtectedDestinationPolicy = "protected-destination-policy"
	serveReasonProtectedNotFastForward    = "protected-not-fast-forward"
	serveReasonProtectedStaleTip          = "protected-stale-tip"
	serveReasonProtectedBlocked           = "protected-blocked"
	serveReasonProtectedPolicyInvalid     = "protected-policy-invalid"
	serveReasonProtectedDeletion          = "protected-deletion"
	serveReasonAdvanceNotMaintainer       = "advance-not-maintainer"
	serveReasonProposalRefMismatch        = "proposal-ref-mismatch"
	serveReasonProposalRefImmutable       = "proposal-ref-immutable"
	serveReasonMemoryKeyMismatch          = "memory-key-mismatch"
	serveReasonMemoryInvalid              = "memory-invalid"
	serveReasonReservedRef                = "reserved-ref"
	serveReasonTagImmutable               = "tag-immutable"
	serveReasonBranchNotOwner             = "branch-not-owner"
	serveReasonOverBudget                 = "over-budget"
	serveReasonLFSQuota                   = "lfs-quota"
	serveReasonLFSObjectMismatch          = "lfs-object-mismatch"
)

// Rule advancement codes. They live in serveRuleReasonCatalogue, which
// docs/protocol-v0.md documents in its second reason table.
const (
	serveReasonAdvanceNotRuleExecutor    = "advance-not-rule-executor"
	serveReasonProtectedRuleNotSatisfied = "protected-rule-not-satisfied"
	serveReasonProtectedRuleVeto         = "protected-rule-veto"
)

// Evidence kinds for the missing= field, in their fixed emission order.
const (
	serveEvidenceProposal   = "proposal"
	serveEvidenceReview     = "review"
	serveEvidenceCI         = "ci"
	serveEvidenceAcceptance = "acceptance"
)

var serveEvidenceKinds = []string{serveEvidenceProposal, serveEvidenceReview, serveEvidenceCI, serveEvidenceAcceptance}

const (
	serveRejectionPrefix = "hn-serve/1 reject "
	serveSentencePrefix  = "hn-serve: "
)

type serveReason struct {
	Code    string
	Meaning string
	// Next is a template: placeholders such as <commit> are substituted by
	// the caller before it sets serveRejection.Next.
	Next string
}

var serveReasonCatalogue = []serveReason{
	{serveReasonUnknownKey, "The authenticating key is not in the registry", "ask the administrator to run hn serve key add"},
	{serveReasonUnhostedRepository, "The requested repository is not hosted or its path is unsafe", "check the clone URL"},
	{serveReasonUnsupportedCommand, "The SSH request, push option set or advancement shape is not supported", "use git fetch, git push, hn merge --remote or git lfs"},
	{serveReasonRepositoryBusy, "Another push held the repository lock for more than 120 seconds", "retry the push"},
	{serveReasonActorKeyMismatch, "The pushed actor chain belongs to another actor", "push only refs/hn/actors/<your actor>"},
	{serveReasonActorChainInvalid, "An event fails signature, sequence, shape or relationship validation", "inspect the named event with hn log"},
	{serveReasonActorChainRewrite, "The push deletes or rewrites hosted actor history", "append new events instead"},
	{serveReasonDependencyMissing, "A referenced event or Git object is not hosted and not in the push", "publish the named fact or commit first"},
	{serveReasonProtectedNoProposal, "No proposal has the pushed commit as its signed head", "open a proposal for <commit>"},
	{serveReasonProtectedNotReady, "Review, CI or acceptance is unmet under the base policy", "run hn proposal status <candidate>"},
	{serveReasonProtectedDestinationPolicy, "A threshold of the destination policy is unmet", "gather the evidence the current policy requires"},
	{serveReasonProtectedNotFastForward, "The head does not descend from the current tip", "revise the proposal onto <tip> with hn proposal revise"},
	{serveReasonProtectedStaleTip, "The branch moved since the client read it", "fetch, then retry or revise"},
	{serveReasonProtectedBlocked, "The candidate is superseded, its lineage is closed or conflicted, it is merged, or a maintainer rejected it", "run hn proposal status <candidate>"},
	{serveReasonProtectedPolicyInvalid, "Base or destination policy is missing or malformed, or the push creates the branch", "ask the administrator; see recovery in the serve guide"},
	{serveReasonProtectedDeletion, "The push deletes a protected branch", "protected branches cannot be deleted"},
	{serveReasonAdvanceNotMaintainer, "The advancement requester is not a maintainer under both policies", "ask a maintainer to run hn merge --remote"},
	{serveReasonProposalRefMismatch, "A proposal ref differs from its candidate's signed head or does not descend from its base", "push the signed head"},
	{serveReasonProposalRefImmutable, "The push moves or deletes a hosted proposal ref", "create a revision instead"},
	{serveReasonMemoryKeyMismatch, "The memory stream belongs to another actor", "push only your own memory refs"},
	{serveReasonMemoryInvalid, "The memory stream fails hn-memory/0 validation or is rewritten", "inspect the stream with hn memory show"},
	{serveReasonReservedRef, "The ref is outside every supported class", "push branches, tags and Hubnot refs only"},
	{serveReasonTagImmutable, "The push moves or deletes an existing tag", "create a new tag"},
	{serveReasonBranchNotOwner, "A forced update or deletion of another actor's branch", "ask the owner or a maintainer"},
	{serveReasonOverBudget, "A pushed Hubnot ref exceeds a server budget", "split the history or ask the administrator"},
	{serveReasonLFSQuota, "An LFS upload would exceed the repository quota", "ask the administrator to raise the quota"},
	{serveReasonLFSObjectMismatch, "Uploaded LFS content does not match its object ID or size", "retry the upload"},
}

// serveRuleReasonCatalogue holds the codes of protected advancement under a
// named automerge rule. It shares no code with serveReasonCatalogue.
var serveRuleReasonCatalogue = []serveReason{
	{serveReasonAdvanceNotRuleExecutor, "The requester is not an executing actor of the named rule in the base policy", "ask an executing actor of the rule to run hn automerge --remote"},
	{serveReasonProtectedRuleNotSatisfied, "The named rule does not exist, is not a standing rule, or the candidate does not satisfy it", "run hn automerge <candidate> --rule <rule>"},
	{serveReasonProtectedRuleVeto, "A current trusted change request or trusted failure blocks the rule", "resolve the named events, then retry"},
}

// serveReasonLookup searches serveReasonCatalogue and then
// serveRuleReasonCatalogue. Every validation of a code goes through it, so
// the renderer, the line parser, the push log and the client accept both.
func serveReasonLookup(code string) (serveReason, bool) {
	for _, catalogue := range [][]serveReason{serveReasonCatalogue, serveRuleReasonCatalogue} {
		for _, reason := range catalogue {
			if reason.Code == code {
				return reason, true
			}
		}
	}
	return serveReason{}, false
}

func serveReasonKnown(code string) bool {
	_, ok := serveReasonLookup(code)
	return ok
}

// serveReasonDefaultNext returns the catalogue's next-action template, or ""
// for an unknown code.
func serveReasonDefaultNext(code string) string {
	reason, _ := serveReasonLookup(code)
	return reason.Next
}

// serveReasonCarriesBudget reports whether budget=, limit= and measured= may
// appear with code.
func serveReasonCarriesBudget(code string) bool {
	return code == serveReasonOverBudget || code == serveReasonLFSQuota
}

func serveEvidenceKindRank(kind string) int {
	for index, known := range serveEvidenceKinds {
		if known == kind {
			return index
		}
	}
	return -1
}

// serveOrderedMissing returns the evidence kinds in emission order with
// duplicates removed. An unknown kind is an error.
func serveOrderedMissing(kinds []string) ([]string, error) {
	present := make([]bool, len(serveEvidenceKinds))
	for _, kind := range kinds {
		rank := serveEvidenceKindRank(kind)
		if rank < 0 {
			return nil, fmt.Errorf("unknown evidence kind %q", kind)
		}
		present[rank] = true
	}
	var ordered []string
	for rank, ok := range present {
		if ok {
			ordered = append(ordered, serveEvidenceKinds[rank])
		}
	}
	return ordered, nil
}

// serveRejection is one rejected ref update, or a session refusal when Ref is
// "-". The renderer sanitizes every value; the parser returns the sanitized
// values it read.
type serveRejection struct {
	Ref      string   // full ref name, or "-" for session refusals
	Actor    string   // full 64-hex actor, or "-" when the key is unknown
	Code     string   // a code serveReasonLookup knows
	IDs      []string // full sha256: event IDs and full commit IDs
	Missing  []string // subset of proposal, review, ci, acceptance
	Budget   string   // only with over-budget and lfs-quota
	Limit    int64
	Measured int64
	Next     string
	Sentence string
}

// serveSanitizeText keeps printable ASCII and replaces every control byte and
// every non-ASCII byte with '?'. It works on bytes, so one multi-byte UTF-8
// character becomes several '?'.
func serveSanitizeText(value string) string {
	return serveSanitize(value, "")
}

// serveSanitizeToken additionally replaces the field separators space, '='
// and ',' so a value cannot split or forge a field.
func serveSanitizeToken(value string) string {
	return serveSanitize(value, " =,")
}

// serveSanitizeNext additionally replaces '"' and '\' so the quoted next=
// value cannot be closed early or escaped.
func serveSanitizeNext(value string) string {
	return serveSanitize(value, "\"\\")
}

func serveSanitize(value, extra string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character < 0x20 || character >= 0x7f || strings.IndexByte(extra, character) >= 0 {
			builder.WriteByte('?')
			continue
		}
		builder.WriteByte(character)
	}
	return builder.String()
}

// serveRejectionField renders a token value; an empty value becomes
// fallback so the field stays parseable.
func serveRejectionField(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return serveSanitizeToken(value)
}

// renderServeRejection returns the hn-serve/1 rejection line and its sentence
// line, each terminated by "\n".
//
// An empty Ref or Actor renders as "-", an empty ID renders as "?", and a
// Sentence that is empty after sanitizing and trimming trailing spaces renders
// the catalogue meaning. Inputs that the contract
// forbids are programming errors and panic, so a typo can never reach a
// client: an unknown Code, an unknown Missing kind, budget fields on a code
// other than over-budget and lfs-quota, Limit or Measured without Budget, and
// a negative Limit or Measured.
func renderServeRejection(r serveRejection) string {
	reason, ok := serveReasonLookup(r.Code)
	if !ok {
		panic(fmt.Sprintf("hn-serve/1: refusing to render unknown reason code %q", r.Code))
	}
	missing, err := serveOrderedMissing(r.Missing)
	if err != nil {
		panic("hn-serve/1: " + err.Error())
	}
	hasBudget := r.Budget != "" || r.Limit != 0 || r.Measured != 0
	if hasBudget && !serveReasonCarriesBudget(r.Code) {
		panic(fmt.Sprintf("hn-serve/1: budget fields are not allowed with code %s", r.Code))
	}
	if hasBudget && r.Budget == "" {
		panic("hn-serve/1: limit and measured require a budget name")
	}
	if r.Limit < 0 || r.Measured < 0 {
		panic("hn-serve/1: limit and measured must not be negative")
	}

	var line strings.Builder
	line.WriteString(serveRejectionPrefix)
	line.WriteString("ref=" + serveRejectionField(r.Ref, "-"))
	line.WriteString(" actor=" + serveRejectionField(r.Actor, "-"))
	line.WriteString(" code=" + r.Code)
	if len(r.IDs) > 0 {
		ids := make([]string, len(r.IDs))
		for index, id := range r.IDs {
			ids[index] = serveRejectionField(id, "?")
		}
		line.WriteString(" ids=" + strings.Join(ids, ","))
	}
	if len(missing) > 0 {
		line.WriteString(" missing=" + strings.Join(missing, ","))
	}
	if hasBudget {
		line.WriteString(" budget=" + serveSanitizeToken(r.Budget))
		line.WriteString(" limit=" + strconv.FormatInt(r.Limit, 10))
		line.WriteString(" measured=" + strconv.FormatInt(r.Measured, 10))
	}
	line.WriteString(` next="` + serveSanitizeNext(r.Next) + `"`)
	line.WriteString("\n")

	// Trailing spaces are dropped because clients strip Git's trailing padding.
	sentence := strings.TrimRight(serveSanitizeText(r.Sentence), " ")
	if sentence == "" {
		sentence = reason.Meaning
	}
	line.WriteString(serveSentencePrefix + sentence + "\n")
	return line.String()
}

// renderServeRejections renders every rejection in byte order of the ref name
// as given, before sanitizing, keeping the input order for equal refs. Sanitizing
// can change the order ('?' sorts after ',' and digits), and the contract
// orders lines by ref name. An empty Ref sorts as "-", the value it renders as.
func renderServeRejections(list []serveRejection) string {
	ordered := make([]serveRejection, len(list))
	copy(ordered, list)
	sortKey := func(ref string) string {
		if ref == "" {
			return "-"
		}
		return ref
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return sortKey(ordered[i].Ref) < sortKey(ordered[j].Ref)
	})
	var output strings.Builder
	for _, rejection := range ordered {
		output.WriteString(renderServeRejection(rejection))
	}
	return output.String()
}

// serveRejectionFieldOrder lists every field after the prefix in its only
// permitted position.
var serveRejectionFieldOrder = []string{"ref", "actor", "code", "ids", "missing", "budget", "limit", "measured", "next"}

func serveRejectionFieldRank(name string) int {
	for index, known := range serveRejectionFieldOrder {
		if known == name {
			return index
		}
	}
	return -1
}

// parseServeRejectionLine accepts exactly the first line produced by
// renderServeRejection. It does not parse sentence lines.
func parseServeRejectionLine(line string) (serveRejection, error) {
	var result serveRejection
	if !strings.HasPrefix(line, serveRejectionPrefix) {
		return result, fmt.Errorf("line does not start with %q", serveRejectionPrefix)
	}
	if serveSanitizeText(line) != line {
		return result, fmt.Errorf("line contains a control or non-ASCII byte")
	}
	rest := line[len(serveRejectionPrefix):]
	seen := make(map[string]bool)
	lastRank := -1
	var hasBudget, hasLimit, hasMeasured bool
	for {
		if rest == "" {
			return result, fmt.Errorf("missing next field")
		}
		if strings.HasPrefix(rest, `next="`) {
			if err := serveCheckFieldPosition("next", seen, &lastRank); err != nil {
				return result, err
			}
			value := rest[len(`next="`):]
			closing := strings.IndexByte(value, '"')
			if closing < 0 {
				return result, fmt.Errorf("unterminated next value")
			}
			if closing != len(value)-1 {
				return result, fmt.Errorf("text after the closing quote of next")
			}
			value = value[:closing]
			if strings.IndexByte(value, '\\') >= 0 {
				return result, fmt.Errorf("next value contains a backslash")
			}
			result.Next = value
			break
		}
		token := rest
		if space := strings.IndexByte(rest, ' '); space >= 0 {
			token, rest = rest[:space], rest[space+1:]
		} else {
			rest = ""
		}
		name, value, found := strings.Cut(token, "=")
		if !found {
			return result, fmt.Errorf("malformed field %q", token)
		}
		if name == "next" {
			return result, fmt.Errorf("next value is not quoted")
		}
		if err := serveCheckFieldPosition(name, seen, &lastRank); err != nil {
			return result, err
		}
		if value == "" || strings.ContainsAny(value, "=") {
			return result, fmt.Errorf("malformed %s value %q", name, value)
		}
		switch name {
		case "ref":
			if strings.Contains(value, ",") {
				return result, fmt.Errorf("malformed ref value %q", value)
			}
			result.Ref = value
		case "actor":
			if strings.Contains(value, ",") {
				return result, fmt.Errorf("malformed actor value %q", value)
			}
			result.Actor = value
		case "code":
			if !serveReasonKnown(value) {
				return result, fmt.Errorf("unknown reason code %q", value)
			}
			result.Code = value
		case "ids":
			ids := strings.Split(value, ",")
			for _, id := range ids {
				if id == "" {
					return result, fmt.Errorf("empty value in ids")
				}
			}
			result.IDs = ids
		case "missing":
			kinds := strings.Split(value, ",")
			ordered, err := serveOrderedMissing(kinds)
			if err != nil {
				return result, err
			}
			if strings.Join(ordered, ",") != value {
				return result, fmt.Errorf("missing kinds %q are duplicated or out of order", value)
			}
			result.Missing = ordered
		case "budget":
			if strings.Contains(value, ",") {
				return result, fmt.Errorf("malformed budget value %q", value)
			}
			hasBudget = true
			result.Budget = value
		case "limit":
			number, err := serveParseDecimal(value)
			if err != nil {
				return result, fmt.Errorf("limit: %w", err)
			}
			hasLimit = true
			result.Limit = number
		case "measured":
			number, err := serveParseDecimal(value)
			if err != nil {
				return result, fmt.Errorf("measured: %w", err)
			}
			hasMeasured = true
			result.Measured = number
		}
	}
	if !seen["ref"] || !seen["actor"] || !seen["code"] {
		return result, fmt.Errorf("ref, actor and code are required")
	}
	if hasBudget || hasLimit || hasMeasured {
		if !(hasBudget && hasLimit && hasMeasured) {
			return result, fmt.Errorf("budget, limit and measured must appear together")
		}
		if !serveReasonCarriesBudget(result.Code) {
			return result, fmt.Errorf("budget fields are not allowed with code %s", result.Code)
		}
	}
	return result, nil
}

func serveCheckFieldPosition(name string, seen map[string]bool, lastRank *int) error {
	rank := serveRejectionFieldRank(name)
	if rank < 0 {
		return fmt.Errorf("unknown field %q", name)
	}
	if seen[name] {
		return fmt.Errorf("duplicate field %q", name)
	}
	if rank <= *lastRank {
		return fmt.Errorf("field %q is out of order", name)
	}
	// ref, actor and code are mandatory, so no field may skip past them.
	for _, required := range serveRejectionFieldOrder[:3] {
		if rank > serveRejectionFieldRank(required) && !seen[required] && name != required {
			return fmt.Errorf("field %q appears before %q", name, required)
		}
	}
	seen[name] = true
	*lastRank = rank
	return nil
}

// serveParseDecimal accepts the canonical decimal form strconv.FormatInt
// produces for a non-negative int64: digits only, no sign, no leading zero.
func serveParseDecimal(value string) (int64, error) {
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return 0, fmt.Errorf("%q is not a decimal number", value)
		}
	}
	if len(value) > 1 && value[0] == '0' {
		return 0, fmt.Errorf("%q has a leading zero", value)
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is out of range", value)
	}
	return number, nil
}

// serveCleanStderrLine removes one "remote: " prefix that Git adds to relayed
// hook output, and the trailing padding Git appends: spaces when stderr is
// not a terminal, or the clear-to-end-of-line sequence when it is.
func serveCleanStderrLine(line string) string {
	line = strings.TrimPrefix(line, "remote: ")
	for {
		trimmed := strings.TrimRight(line, " ")
		trimmed = strings.TrimSuffix(trimmed, "\x1b[K")
		if trimmed == line {
			return line
		}
		line = trimmed
	}
}

// extractServeRejections reads git push stderr and returns every parsed
// hn-serve/1 rejection, each paired with the sentence line that follows it,
// plus the cleaned hn-serve/1 and hn-serve: lines in their original order so
// a caller can print them verbatim. Git's own lines are ignored. Carriage
// returns separate lines, which covers CRLF and Git's progress output.
func extractServeRejections(stderr string) ([]serveRejection, []string) {
	var rejections []serveRejection
	var lines []string
	// pending is the index of the last parsed rejection still waiting for its
	// sentence, or -1.
	pending := -1
	rawLines := strings.FieldsFunc(stderr, func(character rune) bool {
		return character == '\n' || character == '\r'
	})
	for _, raw := range rawLines {
		line := serveCleanStderrLine(raw)
		switch {
		case strings.HasPrefix(line, "hn-serve/1 "):
			lines = append(lines, line)
			pending = -1
			rejection, err := parseServeRejectionLine(line)
			if err != nil {
				continue
			}
			rejections = append(rejections, rejection)
			pending = len(rejections) - 1
		case strings.HasPrefix(line, serveSentencePrefix):
			lines = append(lines, line)
			if pending >= 0 {
				rejections[pending].Sentence = line[len(serveSentencePrefix):]
				pending = -1
			}
		}
	}
	return rejections, lines
}
