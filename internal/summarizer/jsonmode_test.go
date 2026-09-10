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
		`{"verdict":"healthy"}`,                 // no blocks
		`{"verdict":"healthy","blocks":"nope"}`, // blocks not an array
		`{"verdict":"healthy","blocks":[]}`,     // empty blocks
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
