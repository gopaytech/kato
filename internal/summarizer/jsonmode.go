package summarizer

import (
	"bytes"
	"encoding/json"
	"strings"
)

// jsonInstruction replaces verdictInstruction when Format=="json". It defines
// the block schema and requires a bare JSON reply (no prose, no code fences).
const jsonInstruction = `Reply with ONLY a single JSON object, no prose and no code fences, in exactly this shape:
{"verdict":"healthy|unhealthy|unknown","headline":"one short line","blocks":[{"type":"heading","text":"..."},{"type":"paragraph","text":"..."},{"type":"list","items":["...","..."]},{"type":"code","text":"...","language":"shell"}]}
Use "healthy" only if the evidence shows the subject is working, "unhealthy" if it shows a problem, and "unknown" if the evidence is insufficient. Every block's "type" must be one of heading, paragraph, list, code. "text"/"items" are plain text.`

type jsonSummary struct {
	Verdict  string          `json:"verdict"`
	Headline string          `json:"headline"`
	Blocks   json.RawMessage `json:"blocks"`
}

// parseJSONSummary cleans a model reply (strips a ```json fence, trims to the
// outer { … }), validates the {verdict, headline, blocks[]} shape, and returns
// the bare JSON plus the extracted verdict. ok=false on any failure, signaling
// the caller to downgrade to markdown.
func parseJSONSummary(raw string) (clean string, healthy *bool, headline string, ok bool) {
	s := stripCodeFence(strings.TrimSpace(raw))
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return "", nil, "", false
	}
	s = s[start : end+1]

	var doc jsonSummary
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		return "", nil, "", false
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(doc.Blocks, &blocks); err != nil || len(blocks) == 0 {
		return "", nil, "", false
	}

	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err != nil {
		return "", nil, "", false
	}
	return buf.String(), healthyFromVerdict(doc.Verdict), truncateHeadline(doc.Headline), true
}

// healthyFromVerdict maps the verdict keyword to the tri-state health pointer.
func healthyFromVerdict(v string) *bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "healthy":
		t := true
		return &t
	case "unhealthy":
		f := false
		return &f
	default:
		return nil
	}
}

// stripCodeFence removes a single leading ```lang line and a trailing ``` line
// if present; otherwise returns s unchanged.
func stripCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
