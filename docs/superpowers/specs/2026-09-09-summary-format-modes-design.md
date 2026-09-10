# Summary output format modes (markdown | json)

**Date:** 2026-09-09
**Status:** Design approved, pending spec review

## Problem

kato's summarizer sends collected evidence to an LLM and stores the model's reply
in `Run.status.summary`. Downstream, kato-bot forwards that text to Lark, Slack,
Telegram, etc. Today the reply is Markdown (the model's trained default, reinforced
by the Markdown-shaped evidence kato feeds it), but nothing declares the format.
Telegram in particular does not accept CommonMark — its `MarkdownV2` is a stricter
dialect — so kato-bot has no reliable way to know how to render `summary`.

kato never *instructs* an output format today; it relies on the model's default.
That is unreliable, especially on smaller/local models.

## Goals

- Let the operator choose the summary output format via an environment variable.
- Emit a `summaryFormat` marker on the Run so kato-bot knows how to interpret
  `summary` without guessing.
- Guarantee to downstream consumers that `summaryFormat` **accurately describes**
  `summary`, even though the LLM's output is not 100% reliable.
- Preserve today's behavior byte-for-byte when the feature is left at its default.

## Non-goals

- Rendering to specific channels (Lark/Slack/Telegram/HTML). That lives in kato-bot,
  a separate repository. This change only defines what kato **emits**.
- Renaming the CRD API group `kato.zufardhiyaulhaq.com` (explicitly out of scope).
- Retrying the LLM on malformed JSON (deferred; see Error handling).

## Core invariant

> The LLM is not trustworthy about format; **kato is the validation boundary**.
> `summaryFormat` always describes `summary` correctly because kato verifies the
> output before setting it. If json was requested but the model returned invalid
> JSON, kato does not claim `json` — it downgrades to `markdown` and records a warning.

This lets kato-bot trust `summaryFormat` unconditionally while the LLM stays <100%.

## Configuration

New environment variable, read in `internal/config/config.go`:

- `KATO_SUMMARY_FORMAT` — `markdown` (default) or `json`.
- Parsing is case-insensitive. An empty or unrecognized value falls back to
  `markdown`. `cmd/kato` logs the effective format at startup; an unrecognized
  value is logged as a warning.

`Config` gains `SummaryFormat string`.

## Two summarizer modes

`summarizer.Summarizer` gains a `Format string` field (`"markdown"` | `"json"`;
empty is treated as `"markdown"`), wired from `cfg.SummaryFormat` in `cmd/kato`.
Same `systemPrompt`; the appended instruction and the reply parsing differ per mode.

### markdown mode (default) — unchanged behavior

- System prompt = `systemPrompt` + the existing `verdictInstruction`
  (`VERDICT: …` line, blank line, then analysis). One sentence is added pinning the
  reply to GitHub-flavored Markdown so the format is predictable.
- Reply parsing = today's tolerant `parseVerdict` ([verdict.go:31]): the first
  `VERDICT:` line yields `Healthy`/`Headline`; the remaining text is `Summary`
  unchanged. A missing verdict line leaves `Healthy=nil` and the text intact.
- `SummaryOutput.Format = "markdown"`.

This mode cannot meaningfully "fail" — any text is valid-enough Markdown.

### json mode

- System prompt = `systemPrompt` + a new `jsonInstruction` describing the schema
  below and requiring the reply to be **only** the JSON document (no prose, no code
  fences).
- After the call, kato normalizes and validates:
  1. Strip a leading/trailing ```` ```json ```` / ```` ``` ```` fence if present.
  2. Trim to the outermost `{ … }`.
  3. `json.Unmarshal` and check the shape has `verdict` and `blocks`.
- **On success:** `Summary` = the cleaned, re-serialized JSON string;
  `Healthy` from `verdict` (`healthy`→true, `unhealthy`→false, `unknown`/absent→nil);
  `Headline` from `headline` (truncated to 120 runes, matching markdown mode);
  `Format = "json"`.
- **On failure:** downgrade — `Summary` = the raw model text, `Healthy=nil`,
  `Headline=""`, `Format = "markdown"`, and
  `Warning = "requested json summary but model returned invalid json; served as markdown"`.

### JSON contract (json mode payload)

```json
{
  "verdict": "healthy | unhealthy | unknown",
  "headline": "one short line",
  "blocks": [
    { "type": "heading",   "text": "Diagnosis" },
    { "type": "paragraph", "text": "The deployment web has 0/3 ready replicas." },
    { "type": "list",      "items": ["image tag :v2 not found", "3 pods CrashLoopBackOff"] },
    { "type": "code",      "text": "kubectl get pods -n web", "language": "shell" }
  ]
}
```

- Block types: `heading`, `paragraph`, `list`, `code`. `text` / `items` are plain
  strings (no inline emphasis/links in v1 — deferred; see Future work).
- kato only reads `verdict` and `headline`; `blocks` is an opaque passthrough that
  kato-bot renders. kato validates that `blocks` is a JSON array but does not
  enforce per-block types (forward-compatible with kato-bot adding block types).

## Data model changes

Thread `summaryFormat` and the downgrade warning through the existing path
(`SummaryOutput` → `engine.Result` → `RunStatus` → HTTP API):

- `summarizer` `SummaryOutput`: add `Format string` and `Warning string`.
- `engine.SummaryOutput` (mirror): add `Format string` and `Warning string`.
- `engine.go` success branch: `res.SummaryFormat = out.Format`; and
  `if out.Warning != "" { res.Warning = out.Warning }` (does not clobber the
  existing "no summarizer" / "summary unavailable" warnings, which return early).
- `engine.Result`: add `SummaryFormat string`.
- `internal/store/store.go` status mapping: add `SummaryFormat: res.SummaryFormat`.
- `api/v1alpha1/run_types.go` `RunStatus`: add

  ```go
  // SummaryFormat tells consumers how to interpret Summary: "markdown" (default)
  // or "json" (a structured block document). Always set on a produced summary.
  // +optional
  // +kubebuilder:validation:Enum=markdown;json
  SummaryFormat string `json:"summaryFormat,omitempty"`
  ```

  The field is optional and additive. Because `Run.status` is a **structural**
  subresource (every field typed; no `preserve-unknown-fields` at the status level),
  the API server prunes unknown status fields — so the CRD must be regenerated for
  the field to persist.

## OpenAI client change

`internal/summarizer/openai.go` `chatRequest` gains an optional
`ResponseFormat *responseFormat json:"response_format,omitempty"`. In json mode the
summarizer sets `{"type":"json_object"}`. Endpoints that ignore it (some
OpenAI-compatible servers) are still covered by the validate-and-downgrade fallback.
In markdown mode the field is nil and the request is byte-for-byte unchanged.

## Wiring

`cmd/kato/main.go`: set `sum.Format = cfg.SummaryFormat` on the existing
`&summarizer.Summarizer{…}` construction, and pass `cfg` through as it already does.

## Backward compatibility

- Default `KATO_SUMMARY_FORMAT=markdown` reproduces current behavior exactly: same
  system prompt (plus one clarifying sentence), same `parseVerdict`, same request body.
- `summaryFormat` is `omitempty` and additive; existing kato-bot code that ignores it
  is unaffected. Older Runs without the field are read as empty ⇒ treat as markdown.

## Error handling

- LLM call error or no summarizer: unchanged — `res.Warning` set, no summary
  (early return in `engine.go`).
- json mode, invalid or non-conforming JSON (missing `verdict`/`blocks`, unparseable):
  downgrade to markdown + warning (above). kato-bot always has a renderable `summary`.
- No LLM retry in v1 (YAGNI): the downgrade already keeps the pipeline working. Add a
  single strict-reminder retry later only if json failures prove common in practice.

## Testing

- `internal/config`: `KATO_SUMMARY_FORMAT` parsing — default, `json`, mixed case,
  invalid → markdown.
- `internal/summarizer`:
  - markdown mode: system prompt carries the verdict instruction; `parseVerdict`
    behavior preserved (existing tests still pass).
  - json mode success: valid JSON → `Format="json"`, `Summary` is the cleaned JSON,
    `Healthy`/`Headline` extracted from fields; fenced ```` ```json ```` and
    surrounding prose are stripped.
  - json mode downgrade: invalid JSON → `Format="markdown"`, raw text preserved,
    `Warning` set, `Healthy=nil`.
  - `response_format` present only in json mode.
- `internal/engine`: `SummaryOutput.Format`/`Warning` propagate to `Result`; a
  non-empty summarizer warning does not clobber the early-return warnings.
- `internal/store`: `SummaryFormat` written to `RunStatus`.
- Regenerate `make generate` (deepcopy) and `make manifests` (CRD), then sync
  `config/crd/bases` → `charts/kato/crds/`; confirm `runs` CRD status lists
  `summaryFormat` with the enum.

## Files touched

- `internal/config/config.go` (+ test)
- `internal/summarizer/summarizer.go`, `verdict.go` (or a new `json.go` for the
  json-mode instruction + parser) (+ tests)
- `internal/summarizer/openai.go` (+ test)
- `internal/engine/engine.go` (SummaryOutput mirror, Result field, copy) (+ test)
- `internal/store/store.go` (status mapping) (+ test)
- `api/v1alpha1/run_types.go` (RunStatus field)
- `cmd/kato/main.go` (wiring)
- `api/v1alpha1/zz_generated.deepcopy.go` (regen), `config/crd/bases/*runs*.yaml`
  (regen), `charts/kato/crds/*runs*.yaml` (sync)

## Future work (out of scope)

- Inline spans (bold, links) inside `text`, as `{"text":"...","bold":true}` /
  `{"text":"docs","href":"..."}` arrays — add when a channel needs richer emphasis.
- One strict-reminder LLM retry before downgrade.
- Per-UseCase override of the format (via UseCase spec) if a single global env var
  proves too coarse.
