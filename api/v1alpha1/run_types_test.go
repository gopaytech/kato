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
