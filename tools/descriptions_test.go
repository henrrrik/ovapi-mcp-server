package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// These tests pin the shipped tool-description strings against the acceptance
// criteria in the spec. A caller LLM reads these descriptions to pick tools
// and build arguments; regressions here are silent at compile time but
// meaningfully degrade caller behavior.

func findToolParameterDescription(t *testing.T, schema json.RawMessage, key string) string {
	t.Helper()
	var parsed struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	prop, ok := parsed.Properties[key]
	if !ok {
		t.Fatalf("parameter %q not in tool schema", key)
	}
	return prop.Description
}

func TestToolDescription_LinesLineID_ExampleAndFormat(t *testing.T) {
	tool, _ := LinesTool(newMockDoer("{}"))
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	desc := findToolParameterDescription(t, raw, "line_id")
	for _, want := range []string{"GVB_17_1", "{owner}_{public_number}_{direction}"} {
		if !strings.Contains(desc, want) {
			t.Errorf("line_id description missing %q\nhave: %s", want, desc)
		}
	}
}

func TestToolDescription_SearchStops_PairedWith(t *testing.T) {
	tool, _ := SearchStopsTool(&mockStopSearcher{})
	if !strings.Contains(tool.Description, "paired_with") {
		t.Errorf("search_stops description missing paired_with\nhave: %s", tool.Description)
	}
}

func TestToolDescription_GetDepartures_StatusVocabulary(t *testing.T) {
	tool, _ := DeparturesTool(newMockDoer("{}"), &mockStopSearcher{})
	// Spec asks for at minimum PLANNED, DRIVING, ARRIVED, PASSED, CANCEL.
	// OFFROUTE is also documented since the server can emit it.
	for _, status := range []string{"PLANNED", "DRIVING", "ARRIVED", "PASSED", "CANCEL"} {
		if !strings.Contains(tool.Description, status) {
			t.Errorf("get_departures description missing status %q\nhave: %s",
				status, tool.Description)
		}
	}
}
