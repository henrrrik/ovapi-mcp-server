package tools

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// The lines index is keyed by DataOwnerCode ("NL_5302_1") and, live, none of
// those keys resolve at /line/{id}: the detail endpoint only knows
// OperatorCode-prefixed ids ("GVB_17_1", "CXX_M300_1"). Each departure pass
// carries OperatorCode, LinePlanningNumber and LineDirection, so the server
// can hand out an id that works.
func TestLineIDFor(t *testing.T) {
	cases := []struct {
		operator, owner, planning string
		direction                 int
		want                      string
	}{
		{"GVB", "NL", "17", 1, "GVB_17_1"},
		{"CXX", "NL", "M300", 1, "CXX_M300_1"},
		{"", "GVB", "17", 2, "GVB_17_2"},
		{"GVB", "GVB", "", 1, ""},
		{"GVB", "GVB", "17", 0, ""},
		{"", "", "17", 1, ""},
	}
	for _, c := range cases {
		if got := lineIDFor(c.operator, c.owner, c.planning, c.direction); got != c.want {
			t.Errorf("lineIDFor(%q,%q,%q,%d) = %q, want %q", c.operator, c.owner, c.planning, c.direction, got, c.want)
		}
	}
}

func TestDeparturesTool_EmitsResolvableLineID(t *testing.T) {
	parsed, _ := runLeanArgs(t, "tpc_schiphol.json", map[string]any{"tpc_code": "57330760"})
	if len(parsed.Stops) != 1 || len(parsed.Stops[0].Departures) == 0 {
		t.Fatal("expected departures")
	}
	// Fixture: DataOwnerCode NL, OperatorCode CXX, planning numbers M300 and
	// M198, direction 1. The owner prefix must not leak into the id.
	seen := map[string]bool{}
	for _, d := range parsed.Stops[0].Departures {
		seen[d.LineID] = true
		if !strings.HasPrefix(d.LineID, "CXX_") || !strings.HasSuffix(d.LineID, "_1") {
			t.Errorf("departure %s: line_id = %q, want CXX_{planning}_1", d.JourneyID, d.LineID)
		}
	}
	if !seen["CXX_M300_1"] {
		t.Errorf("expected CXX_M300_1 among line ids, got %v", seen)
	}
}

var offsetTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\+0[12]:00$`)

// journey and line-detail passed upstream's offset-less Amsterdam wall-clock
// strings through verbatim next to a UTC "Z" server_time, while
// get_departures normalised the same fields to "+02:00". A caller comparing
// them naively was two hours off. Every timestamp now carries the Amsterdam
// offset, server_time included.
func TestTransformJourney_LineIDAndOffsetTimes(t *testing.T) {
	lean, err := transformJourney([]byte(loadTestData(t, "journey_live.json")), "GVB_20260422_17_19206_0")
	if err != nil {
		t.Fatal(err)
	}
	if lean.LineID != "GVB_17_1" {
		t.Errorf("journey line_id = %q, want GVB_17_1", lean.LineID)
	}
	if lean.ServerTime != "2026-04-22T13:42:57+02:00" {
		t.Errorf("server_time = %q, want 2026-04-22T13:42:57+02:00 (11:42:57Z)", lean.ServerTime)
	}
	for _, s := range lean.Stops {
		for field, v := range map[string]string{
			"target_arrival": s.TargetArrival, "target_departure": s.TargetDeparture,
			"expected_arrival": s.ExpectedArrival, "expected_departure": s.ExpectedDeparture,
		} {
			if v != "" && !offsetTime.MatchString(v) {
				t.Errorf("stop %d %s = %q, want an Amsterdam-offset timestamp", s.Order, field, v)
			}
		}
	}
}

func TestTransformLineDetail_ActiveJourneysCarryTPCAndOffsetTimes(t *testing.T) {
	lean, err := transformLineDetail([]byte(loadTestData(t, "line_detail_live.json")), "GVB_17_1")
	if err != nil {
		t.Fatal(err)
	}
	if lean.ServerTime != "2026-04-22T13:42:28+02:00" {
		t.Errorf("server_time = %q, want 2026-04-22T13:42:28+02:00", lean.ServerTime)
	}
	if len(lean.ActiveJourneys) == 0 {
		t.Fatal("expected active journeys")
	}
	for _, j := range lean.ActiveJourneys {
		if j.CurrentTPCCode == "" {
			t.Errorf("journey %s: current_tpc_code missing", j.JourneyID)
		}
		if j.Expected != "" && !offsetTime.MatchString(j.Expected) {
			t.Errorf("journey %s: expected = %q, want an Amsterdam-offset timestamp", j.JourneyID, j.Expected)
		}
	}
}

// Upstream answers 200 {} for an id it does not know, which the lean shape
// rendered as a line with no service. That is a not-found.
func TestLinesTool_EmptyUpstreamBodyIsNotFound(t *testing.T) {
	_, handler := LinesTool(newMockDoer("{}"))
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"line_id": "NL_17_1"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected a tool error for an empty upstream body")
	}
	text := result.Content[0].(mcp.TextContent).Text
	for _, want := range []string{"NL_17_1", "get_departures"} {
		if !strings.Contains(text, want) {
			t.Errorf("not-found error should mention %q, got %q", want, text)
		}
	}
}

func TestToolDescription_Lines_IDFormatMatchesUpstream(t *testing.T) {
	tool, _ := LinesTool(newMockDoer("{}"))
	raw, _ := json.Marshal(tool.InputSchema)
	lineID := findToolParameterDescription(t, raw, "line_id")
	for _, stale := range []string{"{owner}_{public_number}_{direction}", "RET_B_1", "QBUZZ_301_1"} {
		if strings.Contains(tool.Description, stale) || strings.Contains(lineID, stale) {
			t.Errorf("lines documentation still contains the wrong id format/example %q", stale)
		}
	}
	for _, want := range []string{"{operator}_{planning_number}_{direction}", "get_departures", "current_tpc_code"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("lines description should mention %q", want)
		}
	}
	if !strings.Contains(lineID, "get_departures") {
		t.Errorf("line_id description should point callers at get_departures line_id, got %q", lineID)
	}
	owner := findToolParameterDescription(t, raw, "owner")
	if !strings.Contains(owner, "NL") || !strings.Contains(owner, "DELIJN") {
		t.Errorf("owner description should reflect the live values (NL, DELIJN), got %q", owner)
	}
	nameContains := findToolParameterDescription(t, raw, "name_contains")
	if strings.Contains(nameContains, "sprinter") {
		t.Errorf("name_contains example 'sprinter' matches nothing in KV78turbo: %q", nameContains)
	}
}

func TestToolDescription_Journey_StatusAndTimezone(t *testing.T) {
	tool, _ := JourneyTool(newMockDoer("{}"))
	if strings.Contains(tool.Description, "stops ahead of the vehicle are PLANNED") {
		t.Error("journey description still claims upcoming stops are PLANNED; they are DRIVING on a tracked run")
	}
	for _, want := range []string{"line_id", "+02:00"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("journey description should mention %q", want)
		}
	}
}

func TestToolDescription_GetDepartures_LineIDAndVerboseCaveats(t *testing.T) {
	tool, _ := DeparturesTool(newMockDoer("{}"), &mockStopSearcher{})
	for _, want := range []string{"line_id", "lean shape only"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("get_departures description should mention %q", want)
		}
	}
	raw, _ := json.Marshal(tool.InputSchema)
	if d := findToolParameterDescription(t, raw, "include_paired"); !strings.Contains(d, "Ignored with stop_name") {
		t.Errorf("include_paired should say it is ignored with stop_name, got %q", d)
	}
	if d := findToolParameterDescription(t, raw, "drop_empty"); !strings.Contains(d, "line_served_here") {
		t.Errorf("drop_empty should warn that it removes the stops line_served_here describes, got %q", d)
	}
}
