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

func TestToolDescription_Lines_ActiveJourneysFields(t *testing.T) {
	tool, _ := LinesTool(newMockDoer("{}"))
	// Spec: document active_journeys[].status and current_order (1-indexed).
	for _, needle := range []string{"active_journeys", "current_order", "1-indexed"} {
		if !strings.Contains(tool.Description, needle) {
			t.Errorf("lines description missing %q\nhave: %s", needle, tool.Description)
		}
	}
}

func TestToolDescription_Lines_PublicNumberFilter(t *testing.T) {
	tool, _ := LinesTool(newMockDoer("{}"))
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	desc := findToolParameterDescription(t, raw, "public_number")
	if desc == "" {
		t.Fatal("lines should expose a public_number parameter")
	}
	if !strings.Contains(desc, "exact") {
		t.Errorf("public_number description should mention exact match\nhave: %s", desc)
	}
}

func TestToolDescription_SearchStops_PairedWith(t *testing.T) {
	tool, _ := SearchStopsTool(&mockStopSearcher{})
	if !strings.Contains(tool.Description, "paired_with") {
		t.Errorf("search_stops description missing paired_with\nhave: %s", tool.Description)
	}
}

func TestToolDescription_SearchStops_IdentifierDistinction(t *testing.T) {
	tool, _ := SearchStopsTool(&mockStopSearcher{})
	// Spec: tpc_code vs stop_area_code must be explained so callers know
	// which to pass to get_departures.
	for _, needle := range []string{"tpc_code", "stop_area_code", "platform", "get_departures"} {
		if !strings.Contains(tool.Description, needle) {
			t.Errorf("search_stops description missing %q\nhave: %s", needle, tool.Description)
		}
	}
}

func TestToolDescription_Journey_StopsAndStopType(t *testing.T) {
	tool, _ := JourneyTool(newMockDoer("{}"))
	for _, needle := range []string{
		"travel order",
		"target_",
		"expected_",
		"is_timing_stop",
		"FIRST",
		"INTERMEDIATE",
		"LAST",
	} {
		if !strings.Contains(tool.Description, needle) {
			t.Errorf("journey description missing %q\nhave: %s", needle, tool.Description)
		}
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

func TestToolDescription_GetDepartures_ModeVocabulary(t *testing.T) {
	tool, _ := DeparturesTool(newMockDoer("{}"), &mockStopSearcher{})
	for _, mode := range []string{"'bus'", "'tram'", "'metro'", "'boat'"} {
		if !strings.Contains(tool.Description, mode) {
			t.Errorf("get_departures description missing mode %q\nhave: %s",
				mode, tool.Description)
		}
	}
	// NS train exclusion is the #1 follow-up question — spec requires it be stated.
	if !strings.Contains(tool.Description, "NS") {
		t.Errorf("get_departures description must mention NS exclusion\nhave: %s", tool.Description)
	}
}

func TestToolDescription_GetDepartures_NullCaveats(t *testing.T) {
	tool, _ := DeparturesTool(newMockDoer("{}"), &mockStopSearcher{})
	for _, field := range []string{"platform", "wheelchair_accessible", "number_of_coaches"} {
		if !strings.Contains(tool.Description, field) {
			t.Errorf("get_departures description missing null-caveat for %q\nhave: %s",
				field, tool.Description)
		}
	}
}

func TestToolDescription_GetDepartures_FilterSemantics(t *testing.T) {
	tool, _ := DeparturesTool(newMockDoer("{}"), &mockStopSearcher{})
	// direction is substring; line is exact; filter order matters — all three
	// are asked for in the spec's docs sweep.
	for _, needle := range []string{
		"substring match",
		"time_window_minutes",
		"max_departures",
	} {
		if !strings.Contains(tool.Description, needle) {
			t.Errorf("get_departures description missing filter semantics %q\nhave: %s",
				needle, tool.Description)
		}
	}
}
