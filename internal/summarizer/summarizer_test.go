package summarizer

import (
	"context"
	"strings"
	"testing"

	"github.com/gopaytech/kato/api/v1alpha1"
	"github.com/gopaytech/kato/internal/engine"
	"github.com/gopaytech/kato/internal/methods"
)

type fakeCompleter struct{ out string }

func (f fakeCompleter) Complete(ctx context.Context, system, user string, jsonMode bool) (string, error) {
	if f.out != "" {
		return f.out, nil
	}
	return "diagnosis", nil
}

func TestSummarizeDebugLogDumpsFullRequest(t *testing.T) {
	uc := &v1alpha1.UseCase{Spec: v1alpha1.UseCaseSpec{
		Steps:   []v1alpha1.Step{{Name: "node", Method: "check_node_status"}},
		Summary: v1alpha1.SummarySpec{Prompt: "diagnose the cluster"},
	}}
	steps := []engine.StepResult{{Name: "node", Outcome: "completed",
		Outputs: methods.Outputs{"ready": true}}}

	var messages string
	called := false
	s := &Summarizer{
		Resolve: func(*v1alpha1.UseCase) (Completer, string, error) { return fakeCompleter{}, "gpt-test", nil },
		DebugLog: func(msg string, kv ...any) {
			called = true
			for i := 0; i+1 < len(kv); i += 2 {
				if kv[i] == "messages" {
					messages, _ = kv[i+1].(string)
				}
			}
		},
	}
	if _, err := s.Summarize(context.Background(), uc, steps); err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if !called {
		t.Fatal("DebugLog was not invoked")
	}
	for _, want := range []string{`"role": "system"`, `"role": "user"`, systemPrompt, "diagnose the cluster", "ready"} {
		if !strings.Contains(messages, want) {
			t.Errorf("debug dump missing %q; got: %s", want, messages)
		}
	}
}

func TestSummarizeWithoutDebugLogIsNoop(t *testing.T) {
	uc := &v1alpha1.UseCase{Spec: v1alpha1.UseCaseSpec{
		Steps:   []v1alpha1.Step{{Name: "node", Method: "check_node_status"}},
		Summary: v1alpha1.SummarySpec{Prompt: "x"},
	}}
	s := &Summarizer{Resolve: func(*v1alpha1.UseCase) (Completer, string, error) { return fakeCompleter{}, "m", nil }}
	if _, err := s.Summarize(context.Background(), uc, nil); err != nil {
		t.Fatalf("Summarize without DebugLog should not error: %v", err)
	}
}

func TestBuildEvidenceAppliesSummaryFilter(t *testing.T) {
	uc := &v1alpha1.UseCase{
		Spec: v1alpha1.UseCaseSpec{
			Steps: []v1alpha1.Step{
				{Name: "status", Method: "check_pod_status",
					SummaryFilter: []string{"phase", "restartCount"}}, // only these two
				{Name: "secret-step", Method: "check_pod_logs",
					SummaryFilter: []string{}}, // empty -> excluded entirely
				{Name: "events", Method: "check_events"}, // nil -> all outputs
			},
			Summary: v1alpha1.SummarySpec{Prompt: "diagnose the crash"},
		},
	}
	steps := []engine.StepResult{
		{Name: "status", Outcome: "completed", Outputs: methods.Outputs{
			"phase": "Running", "restartCount": int64(17), "nodeName": "node-7"}},
		{Name: "secret-step", Outcome: "completed", Outputs: methods.Outputs{"logs": "SENSITIVE"}},
		{Name: "events", Outcome: "completed", Outputs: methods.Outputs{"count": int64(3)}},
	}

	ev := BuildEvidence(uc, steps)
	if !strings.Contains(ev, "phase") || !strings.Contains(ev, "Running") {
		t.Error("filtered-in field missing")
	}
	if strings.Contains(ev, "nodeName") || strings.Contains(ev, "node-7") {
		t.Error("field not in summaryFilter leaked")
	}
	if strings.Contains(ev, "SENSITIVE") || strings.Contains(ev, "secret-step") {
		t.Error("empty summaryFilter step leaked")
	}
	if !strings.Contains(ev, "count") {
		t.Error("nil summaryFilter step should include all outputs")
	}
}

func TestBuildEvidenceIncludesOutcomes(t *testing.T) {
	uc := &v1alpha1.UseCase{Spec: v1alpha1.UseCaseSpec{
		Steps:   []v1alpha1.Step{{Name: "node", Method: "check_node_status"}},
		Summary: v1alpha1.SummarySpec{Prompt: "x"},
	}}
	steps := []engine.StepResult{{Name: "node", Outcome: "failed", Error: "nodes \"node-7\" not found"}}
	ev := BuildEvidence(uc, steps)
	if !strings.Contains(ev, "failed") || !strings.Contains(ev, "not found") {
		t.Errorf("failed step evidence missing: %q", ev)
	}
}

func TestBuildEvidenceRendersIterations(t *testing.T) {
	uc := &v1alpha1.UseCase{Spec: v1alpha1.UseCaseSpec{
		Steps: []v1alpha1.Step{
			{Name: "check", Method: "check_pod_status", ForEach: "$(steps.crashing.pods)",
				SummaryFilter: []string{"restartCount"}}, // only restartCount per iteration
		},
		Summary: v1alpha1.SummarySpec{Prompt: "x"},
	}}
	steps := []engine.StepResult{{
		Name: "check", Outcome: "completed",
		Note: "matched 3, checked 2 (worst-first); 1 not examined",
		Iterations: []engine.IterationResult{
			{Item: map[string]string{"name": "b", "namespace": "kube-system"}, Outcome: "completed",
				Outputs: methods.Outputs{"restartCount": int64(9), "phase": "Running"}},
			{Item: map[string]string{"name": "a"}, Outcome: "failed", Error: "logs unavailable"},
		},
	}}
	ev := BuildEvidence(uc, steps)
	if !strings.Contains(ev, "not examined") {
		t.Error("note missing")
	}
	if !strings.Contains(ev, `"b"`) && !strings.Contains(ev, "b") {
		t.Error("item identity missing")
	}
	if !strings.Contains(ev, "restartCount") || !strings.Contains(ev, "9") {
		t.Error("filtered iteration output missing")
	}
	if strings.Contains(ev, "phase") { // filtered out by summaryFilter
		t.Error("non-filtered field leaked into iteration evidence")
	}
	if !strings.Contains(ev, "logs unavailable") {
		t.Error("failed iteration error missing")
	}
}

func TestSummarizeReturnsVerdict(t *testing.T) {
	s := &Summarizer{
		Resolve: func(uc *v1alpha1.UseCase) (Completer, string, error) {
			return fakeCompleter{out: "VERDICT: unhealthy — pods crashlooping\n\nThe pods are crashing."}, "gpt-test", nil
		},
	}
	uc := &v1alpha1.UseCase{}
	uc.Name = "demo"
	uc.Spec.Summary.Prompt = "diagnose it"

	out, err := s.Summarize(context.Background(), uc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Healthy == nil || *out.Healthy != false {
		t.Errorf("Healthy = %v, want false", out.Healthy)
	}
	if out.Headline != "pods crashlooping" {
		t.Errorf("Headline = %q, want %q", out.Headline, "pods crashlooping")
	}
	if out.Summary != "The pods are crashing." {
		t.Errorf("Summary = %q, want prose without the verdict line", out.Summary)
	}
	if out.ModelConfig != "gpt-test" {
		t.Errorf("ModelConfig = %q, want gpt-test", out.ModelConfig)
	}
}

func TestSummarizeAppendsVerdictInstruction(t *testing.T) {
	var gotSystem string
	s := &Summarizer{
		Resolve: func(uc *v1alpha1.UseCase) (Completer, string, error) {
			return systemCapturingCompleter{capture: &gotSystem}, "m", nil
		},
	}
	uc := &v1alpha1.UseCase{}
	uc.Spec.Summary.Prompt = "x"
	if _, err := s.Summarize(context.Background(), uc, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotSystem, "VERDICT:") {
		t.Errorf("system prompt should carry the verdict instruction, got %q", gotSystem)
	}
}

type systemCapturingCompleter struct{ capture *string }

func (c systemCapturingCompleter) Complete(ctx context.Context, system, user string, jsonMode bool) (string, error) {
	*c.capture = system
	return "VERDICT: healthy — ok\n\nfine", nil
}

func TestSummarize_JSONMode(t *testing.T) {
	uc := &v1alpha1.UseCase{Spec: v1alpha1.UseCaseSpec{Summary: v1alpha1.SummarySpec{Prompt: "x"}}}

	// valid json -> Format json, healthy parsed
	valid := `{"verdict":"unhealthy","headline":"bad","blocks":[{"type":"paragraph","text":"x"}]}`
	s := &Summarizer{
		Format: "json",
		Resolve: func(*v1alpha1.UseCase) (Completer, string, error) {
			return fakeCompleter{out: valid}, "gpt-test", nil
		},
	}
	out, err := s.Summarize(context.Background(), uc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Format != "json" || out.Healthy == nil || *out.Healthy != false || out.Warning != "" {
		t.Errorf("valid json: got %+v", out)
	}

	// invalid json -> downgrade to markdown + warning
	s2 := &Summarizer{
		Format: "json",
		Resolve: func(*v1alpha1.UseCase) (Completer, string, error) {
			return fakeCompleter{out: "oops not json"}, "gpt-test", nil
		},
	}
	out2, err := s2.Summarize(context.Background(), uc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out2.Format != "markdown" || out2.Warning == "" || out2.Summary != "oops not json" {
		t.Errorf("downgrade: got %+v", out2)
	}
}

// TestSummarize_JSONMode_DowngradeIgnoresVerdictLine covers the defect where the
// downgrade path relied on parseVerdict(out), which strips a "VERDICT: ..." line
// and sets Healthy/Headline from it. When json mode is requested but the model
// replies in old markdown style, the downgrade contract requires the RAW model
// output verbatim as Summary, with Healthy=nil and Headline="" — independent of
// whatever parseVerdict would have extracted from that text.
func TestSummarize_JSONMode_DowngradeIgnoresVerdictLine(t *testing.T) {
	uc := &v1alpha1.UseCase{Spec: v1alpha1.UseCaseSpec{Summary: v1alpha1.SummarySpec{Prompt: "x"}}}

	raw := "VERDICT: healthy — looks fine\n\nEverything's fine."
	s := &Summarizer{
		Format: "json",
		Resolve: func(*v1alpha1.UseCase) (Completer, string, error) {
			return fakeCompleter{out: raw}, "gpt-test", nil
		},
	}
	out, err := s.Summarize(context.Background(), uc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Format != "markdown" {
		t.Errorf("Format = %q, want markdown", out.Format)
	}
	if out.Healthy != nil {
		t.Errorf("Healthy = %v, want nil (must not be parsed from the raw text)", *out.Healthy)
	}
	if out.Summary != raw {
		t.Errorf("Summary = %q, want raw model output %q", out.Summary, raw)
	}
	if out.Warning == "" {
		t.Error("Warning should be set on downgrade")
	}
}
