# Summary Output Format Modes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the operator pick the LLM summary format (`markdown` default, or `json`) via an env var, and emit a `summaryFormat` marker on the Run so kato-bot can render per channel.

**Architecture:** A `KATO_SUMMARY_FORMAT` env var selects the mode. In markdown mode behavior is unchanged. In json mode the summarizer instructs a block-schema JSON reply, validates it, and — because the LLM is not 100% reliable — downgrades invalid JSON to markdown with a warning. `summaryFormat` therefore always truthfully describes `summary`. The marker threads through the existing `engine.SummaryOutput` → `engine.Result` → `RunStatus` → HTTP API path.

**Tech Stack:** Go 1.25, controller-runtime, controller-gen (deepcopy + CRD manifests), OpenAI-compatible `/chat/completions`.

**Spec:** `docs/superpowers/specs/2026-09-09-summary-format-modes-design.md`

## Global Constraints

- Module path: `github.com/gopaytech/kato` (imports use this).
- Default `KATO_SUMMARY_FORMAT=markdown` MUST reproduce current behavior byte-for-byte (same request body, same parsing).
- `summaryFormat` CRD field is `+optional` / `omitempty`; enum `markdown;json`.
- Do NOT rename the CRD API group `kato.zufardhiyaulhaq.com`.
- Run tests with `make test` (`go test ./... -count=1`); build with `go build ./...`.
- `config/crd/bases/*.yaml` and `charts/kato/crds/*.yaml` are identical copies — regenerate the former, then copy to the latter.

---

### Task 1: Config — `KATO_SUMMARY_FORMAT`

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: existing `getEnv(key, def string) string` helper.
- Produces: `Config.SummaryFormat string` (value is always `"markdown"` or `"json"`, normalized).

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestSummaryFormat(t *testing.T) {
	cases := map[string]string{
		"":         "markdown", // unset -> default
		"markdown": "markdown",
		"json":     "json",
		"JSON":     "json",     // case-insensitive
		"yaml":     "markdown", // unrecognized -> default
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			t.Setenv("KATO_SUMMARY_FORMAT", in)
			if got := Load().SummaryFormat; got != want {
				t.Errorf("SummaryFormat = %q, want %q", got, want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestSummaryFormat -v`
Expected: FAIL — `Config` has no field `SummaryFormat`.

- [ ] **Step 3: Write minimal implementation**

In `internal/config/config.go`, add the field to the `Config` struct (near `MaxEvidenceBytes`):

```go
	// SummaryFormat selects the LLM summary output format: "markdown" (default)
	// or "json". Unrecognized/empty values fall back to "markdown".
	SummaryFormat string
```

Add to the `Config{...}` literal in `Load()`:

```go
		SummaryFormat: getSummaryFormat("KATO_SUMMARY_FORMAT", "markdown"),
```

Add the helper at the bottom of the file (near the other `get*` helpers):

```go
// getSummaryFormat normalizes KATO_SUMMARY_FORMAT to "markdown" or "json",
// case-insensitively; any other value (including empty) yields the default.
func getSummaryFormat(k, def string) string {
	switch strings.ToLower(os.Getenv(k)) {
	case "markdown":
		return "markdown"
	case "json":
		return "json"
	default:
		return def
	}
}
```

Add `"strings"` to the import block.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestSummaryFormat -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: add KATO_SUMMARY_FORMAT config (markdown|json)"
```

---

### Task 2: `RunStatus.SummaryFormat` field + CRD regen

**Files:**
- Modify: `api/v1alpha1/run_types.go` (RunStatus struct, near `ModelConfig` at line ~70)
- Modify (regen): `api/v1alpha1/zz_generated.deepcopy.go`, `config/crd/bases/kato.zufardhiyaulhaq.com_runs.yaml`, `charts/kato/crds/kato.zufardhiyaulhaq.com_runs.yaml`
- Test: `api/v1alpha1/run_types_test.go` (create)

**Interfaces:**
- Produces: `RunStatus.SummaryFormat string` with json tag `summaryFormat,omitempty`.

- [ ] **Step 1: Write the failing test**

Create `api/v1alpha1/run_types_test.go`:

```go
package v1alpha1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRunStatusSummaryFormatJSONTag(t *testing.T) {
	b, err := json.Marshal(RunStatus{SummaryFormat: "json"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"summaryFormat":"json"`) {
		t.Errorf("expected summaryFormat json tag, got %s", b)
	}
	// omitempty: absent when empty
	b2, _ := json.Marshal(RunStatus{})
	if strings.Contains(string(b2), "summaryFormat") {
		t.Errorf("summaryFormat should be omitted when empty, got %s", b2)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./api/v1alpha1/ -run TestRunStatusSummaryFormatJSONTag -v`
Expected: FAIL — `RunStatus` has no field `SummaryFormat`.

- [ ] **Step 3: Write minimal implementation**

In `api/v1alpha1/run_types.go`, add to `RunStatus` immediately after the `ModelConfig` field:

```go
	// SummaryFormat tells consumers how to interpret Summary: "markdown"
	// (default) or "json" (a structured block document). Always set on a
	// produced summary; empty on older Runs (treat as markdown).
	// +optional
	// +kubebuilder:validation:Enum=markdown;json
	SummaryFormat string `json:"summaryFormat,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./api/v1alpha1/ -run TestRunStatusSummaryFormatJSONTag -v`
Expected: PASS.

- [ ] **Step 5: Regenerate deepcopy + CRD manifests and sync the chart**

Run:

```bash
make generate
make manifests
cp config/crd/bases/kato.zufardhiyaulhaq.com_runs.yaml charts/kato/crds/kato.zufardhiyaulhaq.com_runs.yaml
```

Verify the field landed in both CRDs with the enum:

```bash
grep -A3 "summaryFormat:" config/crd/bases/kato.zufardhiyaulhaq.com_runs.yaml
grep -A3 "summaryFormat:" charts/kato/crds/kato.zufardhiyaulhaq.com_runs.yaml
```

Expected: both show `summaryFormat:` with `enum:` listing `markdown` and `json`.

- [ ] **Step 6: Run the full api build/tests**

Run: `go build ./... && go test ./api/... -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add api/v1alpha1/run_types.go api/v1alpha1/run_types_test.go api/v1alpha1/zz_generated.deepcopy.go config/crd/bases/kato.zufardhiyaulhaq.com_runs.yaml charts/kato/crds/kato.zufardhiyaulhaq.com_runs.yaml
git commit -m "feat: add optional summaryFormat field to Run status + regen CRD"
```

---

### Task 3: Thread `Format`/`Warning` through engine

**Files:**
- Modify: `internal/engine/engine.go` (`SummaryOutput` ~line 60, `Result` ~line 48, copy block ~line 107)
- Test: `internal/engine/engine_test.go`

**Interfaces:**
- Consumes: `SummarizeFn` returning `SummaryOutput`.
- Produces: `SummaryOutput.Format string`, `SummaryOutput.Warning string`; `Result.SummaryFormat string`. On summarizer success, `engine` sets `res.SummaryFormat = out.Format` and, when `out.Warning != ""`, `res.Warning = out.Warning`.

- [ ] **Step 1: Write the failing test**

Add to `internal/engine/engine_test.go`:

```go
func TestExecuteThreadsSummaryFormatAndWarning(t *testing.T) {
	client := fake.NewSimpleClientset()
	withFmt := func(_ context.Context, _ *v1alpha1.UseCase, _ []StepResult) (SummaryOutput, error) {
		return SummaryOutput{Summary: "s", Format: "json", Warning: "downgraded"}, nil
	}
	e := newEngine(client, withFmt)
	uc := &v1alpha1.UseCase{Spec: v1alpha1.UseCaseSpec{
		Summary: v1alpha1.SummarySpec{Prompt: "x"},
	}}
	res, err := e.Execute(context.Background(), uc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.SummaryFormat != "json" {
		t.Errorf("SummaryFormat = %q, want json", res.SummaryFormat)
	}
	if res.Warning != "downgraded" {
		t.Errorf("Warning = %q, want downgraded", res.Warning)
	}
}
```

(If `Execute`'s signature in this repo differs, mirror the call already used by `TestExecute*` tests in this file — the point is: run a UseCase whose summarizer returns `Format:"json", Warning:"downgraded"` and assert both reach `Result`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestExecuteThreadsSummaryFormatAndWarning -v`
Expected: FAIL — `SummaryOutput` has no field `Format`/`Warning`; `Result` has no `SummaryFormat`.

- [ ] **Step 3: Write minimal implementation**

In `internal/engine/engine.go`, add to the `Result` struct (after `ModelConfig`):

```go
	SummaryFormat string // "markdown" or "json"; how Summary should be read
```

Add to the `SummaryOutput` struct (after `ModelConfig`):

```go
	// Format is "markdown" or "json" — how Summary should be interpreted.
	Format string
	// Warning, if non-empty, records a non-fatal summary issue (e.g. a json
	// downgrade). It does not prevent the summary from being stored.
	Warning string
```

In the success branch (currently lines ~107-110, after `res.ModelConfig = out.ModelConfig`), add:

```go
	res.SummaryFormat = out.Format
	if out.Warning != "" {
		res.Warning = out.Warning
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run TestExecuteThreadsSummaryFormatAndWarning -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/engine.go internal/engine/engine_test.go
git commit -m "feat: carry summary Format and Warning through engine Result"
```

---

### Task 4: Persist `SummaryFormat` in the store

**Files:**
- Modify: `internal/store/store.go` (status mapping, ~line 96)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `engine.Result.SummaryFormat` (Task 3), `RunStatus.SummaryFormat` (Task 2).
- Produces: `RunStatus.SummaryFormat` set from `res.SummaryFormat` in `SaveRun`.

- [ ] **Step 1: Write the failing test**

Add to `internal/store/store_test.go` (follow the file's existing `SaveRun` test setup for the fake client and `Store`):

```go
func TestSaveRunPersistsSummaryFormat(t *testing.T) {
	s := newTestStore(t) // reuse this file's existing store constructor helper
	res := engine.Result{Phase: "Succeeded", Summary: "{}", SummaryFormat: "json"}
	run, err := s.SaveRun(context.Background(), "uc", nil, res, time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if run.Status.SummaryFormat != "json" {
		t.Errorf("SummaryFormat = %q, want json", run.Status.SummaryFormat)
	}
}
```

(If this file has no `newTestStore` helper, construct the `Store` exactly as the existing tests in `store_test.go` do — same fake client, namespace, and TTL.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSaveRunPersistsSummaryFormat -v`
Expected: FAIL — `run.Status.SummaryFormat` is empty (not mapped).

- [ ] **Step 3: Write minimal implementation**

In `internal/store/store.go`, add to the `v1alpha1.RunStatus{...}` literal (after `ModelConfig: res.ModelConfig,`):

```go
		SummaryFormat: res.SummaryFormat,
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestSaveRunPersistsSummaryFormat -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat: persist summaryFormat to Run status"
```

---

### Task 5: `jsonMode` plumbing — Completer interface + OpenAI `response_format` + `Summarizer.Format`

This task changes the `Completer.Complete` signature to carry a `jsonMode bool`, wires `response_format` in `OpenAIClient`, and adds the `Summarizer.Format` field plus a Markdown-subset pin. Behavior in the default (markdown) mode is unchanged.

**Files:**
- Modify: `internal/summarizer/summarizer.go` (`Completer` interface ~line 19; `Summarizer` struct ~line 96; the `Complete` call ~line 137)
- Modify: `internal/summarizer/verdict.go` (append one sentence to `verdictInstruction`)
- Modify: `internal/summarizer/openai.go` (`chatRequest`, `Complete`)
- Modify: `internal/controller/resolver.go` (`summarizerClient.Complete` ~line 54)
- Test: `internal/summarizer/openai_test.go`, `internal/summarizer/summarizer_test.go`

**Interfaces:**
- Produces:
  - `Completer.Complete(ctx context.Context, system, user string, jsonMode bool) (string, error)`
  - `OpenAIClient.Complete(ctx, system, user string, jsonMode bool) (string, error)` — sets `response_format: {"type":"json_object"}` iff `jsonMode`.
  - `Summarizer.Format string` (`"markdown"` default; `""` treated as markdown).

- [ ] **Step 1: Write the failing test (OpenAI response_format)**

In `internal/summarizer/openai_test.go`, add a test that captures the request body. Add a small `http.HandlerFunc` server that records the body, then:

```go
func TestOpenAIResponseFormat(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()
	c := &OpenAIClient{BaseURL: srv.URL, Model: "m"}

	if _, err := c.Complete(context.Background(), "s", "u", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, `"response_format":{"type":"json_object"}`) {
		t.Errorf("jsonMode: expected response_format, got %s", gotBody)
	}

	if _, err := c.Complete(context.Background(), "s", "u", false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotBody, "response_format") {
		t.Errorf("markdown mode: response_format must be absent, got %s", gotBody)
	}
}
```

Add imports as needed (`net/http`, `net/http/httptest`, `io`, `strings`, `context`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/summarizer/ -run TestOpenAIResponseFormat -v`
Expected: FAIL — `Complete` takes 3 args, not 4.

- [ ] **Step 3: Change the interface, client, wrapper, and callsite**

In `internal/summarizer/summarizer.go`, change the interface:

```go
// Completer is the minimal LLM capability the summarizer needs. jsonMode asks
// an OpenAI-compatible endpoint for a JSON object response where supported.
type Completer interface {
	Complete(ctx context.Context, system, user string, jsonMode bool) (string, error)
}
```

Add the `Format` field to the `Summarizer` struct (after `DebugLog`):

```go
	// Format selects the summary output format: "markdown" (default) or "json".
	// Empty is treated as "markdown".
	Format string
```

Update the call in `Summarize` (currently `out, err := completer.Complete(ctx, system, user)`):

```go
	out, err := completer.Complete(ctx, system, user, s.Format == "json")
```

In `internal/summarizer/openai.go`, add the response-format type and field:

```go
type responseFormat struct {
	Type string `json:"type"`
}
```

Add to `chatRequest`:

```go
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
```

Change `Complete`'s signature and set the field:

```go
func (c *OpenAIClient) Complete(ctx context.Context, system, user string, jsonMode bool) (string, error) {
	reqBody := chatRequest{
		Model:       c.Model,
		MaxTokens:   c.MaxTokens,
		Temperature: c.Temperature,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}
	if jsonMode {
		reqBody.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	// ...unchanged from here down...
```

In `internal/controller/resolver.go`, update the wrapper:

```go
func (s *summarizerClient) Complete(ctx context.Context, system, user string, jsonMode bool) (string, error) {
	return s.client.Complete(ctx, system, user, jsonMode)
}
```

- [ ] **Step 4: Fix the existing test fakes and callsites**

In `internal/summarizer/summarizer_test.go`, update both fake completers to the new signature:

```go
func (f fakeCompleter) Complete(ctx context.Context, system, user string, jsonMode bool) (string, error) {
	// existing body unchanged
}
```

```go
func (c systemCapturingCompleter) Complete(ctx context.Context, system, user string, jsonMode bool) (string, error) {
	// existing body unchanged
}
```

In `internal/summarizer/openai_test.go`, update the existing call `c.Complete(context.Background(), "system prompt", "user evidence")` to pass a fourth arg `false`.

- [ ] **Step 5: Add the Markdown-subset pin to the markdown instruction**

In `internal/summarizer/verdict.go`, append one sentence to the end of the `verdictInstruction` constant string (after the existing example), so markdown output is predictable:

```
Format your analysis as GitHub-flavored Markdown (headings, bullet lists, and fenced code blocks only).
```

- [ ] **Step 6: Run the summarizer + controller tests**

Run: `go test ./internal/summarizer/ ./internal/controller/ -count=1`
Expected: PASS — including the existing markdown tests (behavior preserved) and the new `TestOpenAIResponseFormat`.

- [ ] **Step 7: Commit**

```bash
git add internal/summarizer/summarizer.go internal/summarizer/openai.go internal/summarizer/verdict.go internal/summarizer/openai_test.go internal/summarizer/summarizer_test.go internal/controller/resolver.go
git commit -m "feat: plumb jsonMode through Completer and set OpenAI response_format"
```

---

### Task 6: JSON mode — instruction, validation, downgrade + wiring

Adds the json-mode system instruction, JSON parsing/validation, the downgrade-to-markdown fallback, and the `cmd/kato` wiring that makes the env var reachable.

**Files:**
- Create: `internal/summarizer/jsonmode.go`
- Modify: `internal/summarizer/summarizer.go` (`Summarize` — branch on `s.Format`)
- Modify: `internal/summarizer/verdict.go` (extract `truncateHeadline`, reused by json mode)
- Modify: `cmd/kato/main.go` (set `sum.Format = cfg.SummaryFormat`)
- Test: `internal/summarizer/jsonmode_test.go`

**Interfaces:**
- Consumes: `Summarizer.Format` (Task 5), `engine.SummaryOutput.Format`/`.Warning` (Task 3), `Config.SummaryFormat` (Task 1).
- Produces:
  - `func parseJSONSummary(raw string) (clean string, healthy *bool, headline string, ok bool)` — cleans fences/prose, validates `{verdict, headline, blocks[]}`, returns `ok=false` on any failure.
  - `func truncateHeadline(s string) string` — shared 120-rune truncation (moved out of `parseVerdict`).

- [ ] **Step 1: Write the failing tests**

Create `internal/summarizer/jsonmode_test.go`:

```go
package summarizer

import "testing"

func TestParseJSONSummary_Valid(t *testing.T) {
	raw := "```json\n{\"verdict\":\"unhealthy\",\"headline\":\"bad image\",\"blocks\":[{\"type\":\"paragraph\",\"text\":\"x\"}]}\n```"
	clean, healthy, headline, ok := parseJSONSummary(raw)
	if !ok {
		t.Fatal("expected ok")
	}
	if healthy == nil || *healthy != false {
		t.Errorf("healthy = %v, want false", healthy)
	}
	if headline != "bad image" {
		t.Errorf("headline = %q", headline)
	}
	if clean[0] != '{' || clean[len(clean)-1] != '}' {
		t.Errorf("clean must be bare JSON, got %q", clean)
	}
}

func TestParseJSONSummary_Invalid(t *testing.T) {
	for _, raw := range []string{
		"not json at all",
		`{"verdict":"healthy"}`,                       // no blocks
		`{"verdict":"healthy","blocks":"nope"}`,       // blocks not an array
		`{"verdict":"healthy","blocks":[]}`,           // empty blocks
	} {
		if _, _, _, ok := parseJSONSummary(raw); ok {
			t.Errorf("expected ok=false for %q", raw)
		}
	}
}

func TestParseJSONSummary_UnknownVerdict(t *testing.T) {
	_, healthy, _, ok := parseJSONSummary(`{"verdict":"unknown","blocks":[{"type":"paragraph","text":"x"}]}`)
	if !ok {
		t.Fatal("expected ok")
	}
	if healthy != nil {
		t.Errorf("healthy = %v, want nil for unknown", healthy)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/summarizer/ -run TestParseJSONSummary -v`
Expected: FAIL — `parseJSONSummary` undefined.

- [ ] **Step 3: Implement the json-mode parser**

Create `internal/summarizer/jsonmode.go`:

```go
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
```

- [ ] **Step 4: Extract `truncateHeadline` from `parseVerdict`**

In `internal/summarizer/verdict.go`, replace the inline truncation inside `parseVerdict` with a call to a new shared helper, and add the helper:

```go
// truncateHeadline caps a headline at headlineMaxRunes, appending an ellipsis.
func truncateHeadline(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= headlineMaxRunes {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:headlineMaxRunes-1])) + "…"
}
```

In `parseVerdict`, replace the block that currently does the `utf8.RuneCountInString(headline) > headlineMaxRunes` truncation with:

```go
	headline = truncateHeadline(m[2])
```

- [ ] **Step 5: Branch `Summarize` on `s.Format`**

In `internal/summarizer/summarizer.go`, replace the system-prompt assembly and the post-call parse. Where it currently builds `system := systemPrompt + "\n\n" + verdictInstruction` and later `summary, healthy, headline := parseVerdict(out)`, make both depend on `s.Format`:

```go
	instruction := verdictInstruction
	if s.Format == "json" {
		instruction = jsonInstruction
	}
	system := systemPrompt + "\n\n" + instruction
```

Then, after the `completer.Complete(...)` call returns `out`:

```go
	format := "markdown"
	summary, healthy, headline := parseVerdict(out)
	warning := ""
	if s.Format == "json" {
		if clean, h, hl, ok := parseJSONSummary(out); ok {
			format, summary, healthy, headline = "json", clean, h, hl
		} else {
			warning = "requested json summary but model returned invalid json; served as markdown"
		}
	}
	return engine.SummaryOutput{
		Summary:     summary,
		Healthy:     healthy,
		Headline:    headline,
		ModelConfig: model,
		Format:      format,
		Warning:     warning,
	}, nil
```

(In the downgrade path, `parseVerdict(out)` already ran on the raw text, so `summary` holds the raw model output and `format` stays `"markdown"`.)

- [ ] **Step 6: Wire the env var in cmd/kato**

In `cmd/kato/main.go`, set the field on the existing `sum := &summarizer.Summarizer{...}` literal:

```go
		Format:           cfg.SummaryFormat,
```

- [ ] **Step 7: Run the full suite + build**

Run: `go build ./... && make test`
Expected: PASS — existing markdown tests unchanged, new json tests pass.

- [ ] **Step 8: Add a summarizer-level json success + downgrade test**

Add to `internal/summarizer/summarizer_test.go` (reuse the existing fake-completer + `Summarizer{Resolve: ...}` setup used by other tests in the file):

```go
func TestSummarize_JSONMode(t *testing.T) {
	uc := &v1alpha1.UseCase{Spec: v1alpha1.UseCaseSpec{Summary: v1alpha1.SummarySpec{Prompt: "x"}}}

	// valid json -> Format json, healthy parsed
	valid := `{"verdict":"unhealthy","headline":"bad","blocks":[{"type":"paragraph","text":"x"}]}`
	s := &Summarizer{Format: "json", Resolve: staticResolver(fakeCompleter{reply: valid})}
	out, err := s.Summarize(context.Background(), uc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Format != "json" || out.Healthy == nil || *out.Healthy != false || out.Warning != "" {
		t.Errorf("valid json: got %+v", out)
	}

	// invalid json -> downgrade to markdown + warning
	s2 := &Summarizer{Format: "json", Resolve: staticResolver(fakeCompleter{reply: "oops not json"})}
	out2, err := s2.Summarize(context.Background(), uc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out2.Format != "markdown" || out2.Warning == "" || out2.Summary != "oops not json" {
		t.Errorf("downgrade: got %+v", out2)
	}
}
```

Match `fakeCompleter`'s actual field/constructor and the `Resolve` helper already used in this test file (adapt `reply`/`staticResolver` names to whatever the file defines — do NOT invent new helpers if equivalents exist).

- [ ] **Step 9: Run the new test**

Run: `go test ./internal/summarizer/ -run TestSummarize_JSONMode -v`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/summarizer/jsonmode.go internal/summarizer/jsonmode_test.go internal/summarizer/summarizer.go internal/summarizer/summarizer_test.go internal/summarizer/verdict.go cmd/kato/main.go
git commit -m "feat: json summary mode with validation and markdown downgrade"
```

---

## Self-Review

**1. Spec coverage:**
- Env var (default markdown) → Task 1. ✓
- `summaryFormat` API/CRD field, optional, regen → Task 2. ✓
- Invariant / downgrade / warning → Task 3 (thread Warning) + Task 6 (downgrade). ✓
- markdown mode unchanged + subset pin → Task 5 (pin) + Task 6 (branch keeps `parseVerdict`). ✓
- json mode instruction + schema + verdict-from-fields → Task 6. ✓
- `response_format: json_object` → Task 5. ✓
- No retry (YAGNI) → not implemented, matches spec. ✓
- Data-model thread (SummaryOutput → Result → RunStatus → API) → Tasks 3, 4, 2. ✓
- Backward compat (default byte-for-byte) → Global Constraints + Task 5 keeps markdown request identical when jsonMode=false. ✓
- kato-bot rendering / inline spans out of scope → not in plan. ✓

**2. Placeholder scan:** No TBD/TODO. Each code step has concrete code. Test-helper adaptation notes point at existing, named helpers rather than inventing them.

**3. Type consistency:** `Complete(ctx, system, user string, jsonMode bool)` used identically in the interface, `OpenAIClient`, `summarizerClient`, and both fakes. `SummaryOutput.Format`/`.Warning` (Task 3) match their reads (Task 6) and copies (engine). `Result.SummaryFormat` (Task 3) matches store mapping (Task 4) and `RunStatus.SummaryFormat` (Task 2). `parseJSONSummary`/`truncateHeadline`/`healthyFromVerdict` signatures match their call sites.
