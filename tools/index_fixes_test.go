package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/henrrrik/ovapi-mcp-server/db"
)

// (owner, public_number, direction) is not a total order: many planning
// numbers share one public number, and the input is map iteration, so the
// order and, under a limit, the membership of the index changed per call.
func TestLinesIndex_TieBreakByID(t *testing.T) {
	body := `{
		"NL_C_1":{"LinePublicNumber":"1","DataOwnerCode":"NL","LineName":"c","TransportType":"BUS","LineDirection":1},
		"NL_A_1":{"LinePublicNumber":"1","DataOwnerCode":"NL","LineName":"a","TransportType":"BUS","LineDirection":1},
		"NL_B_1":{"LinePublicNumber":"1","DataOwnerCode":"NL","LineName":"b","TransportType":"BUS","LineDirection":1}}`
	for i := 0; i < 30; i++ {
		resp := parseLinesIndex(t, body, linesIndexFilters{limit: 2})
		if len(resp.Lines) != 2 || resp.Lines[0].ID != "NL_A_1" || resp.Lines[1].ID != "NL_B_1" {
			t.Fatalf("run %d: expected [NL_A_1 NL_B_1], got %v", i, resp.Lines)
		}
	}
}

// name_contains also substring-matched the raw map key, so "1" matched
// every "_1" direction suffix and "gvb" matched by owner prefix.
func TestLinesIndex_NameContains_DoesNotMatchID(t *testing.T) {
	body := `{"GVB_1_1":{"LinePublicNumber":"5","DataOwnerCode":"GVB","LineName":"Zuid","TransportType":"TRAM","LineDirection":1}}`
	for needle, want := range map[string]int{"gvb": 0, "1": 0, "zuid": 1, "5": 1} {
		resp := parseLinesIndex(t, body, linesIndexFilters{nameContains: needle})
		if resp.Total != want {
			t.Errorf("name_contains=%q matched %d, want %d", needle, resp.Total, want)
		}
	}
}

// verbose used to return the entire unfiltered index (1.1 MB) before any
// filter or limit was read, although the docs promised filters still apply.
func TestLinesTool_Verbose_AppliesFiltersAndLimit(t *testing.T) {
	_, handler := LinesTool(newMockDoer(loadTestData(t, "lines_live.json")))
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"verbose": true, "owner": "GVB", "limit": float64(5)}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &raw); err != nil {
		t.Fatalf("verbose body should still be the upstream map shape: %v", err)
	}
	if len(raw) != 5 {
		t.Errorf("expected 5 entries, got %d", len(raw))
	}
	for id, e := range raw {
		if e["DataOwnerCode"] != "GVB" {
			t.Errorf("%s: owner filter leaked %v", id, e["DataOwnerCode"])
		}
		if _, ok := e["LinePlanningNumber"]; !ok {
			t.Errorf("%s: verbose entries must keep upstream field names", id)
		}
	}
}

func TestDeparturesVerbose_DropEmptyApplied(t *testing.T) {
	defer fixedTime(t)()
	parsed := runVerboseMap(t, map[string]any{"tpc_code": "30006018", "line": "99", "drop_empty": true})
	if len(parsed) != 0 {
		t.Errorf("expected every stop dropped (no line 99 passes), got %d", len(parsed))
	}
}

// include_paired is documented for tpc_code; with stop_name it silently
// exceeded 'limit' by expanding every ranked stop.
func TestDeparturesTool_IncludePaired_IgnoredWithStopName(t *testing.T) {
	mockHTTP := newMockDoer(loadTestData(t, "tpc_live.json"))
	mockSearch := &mockStopSearcher{
		results: []db.Stop{{TPCCode: "30006018", Name: "Nicolaas Beetsstraat"}},
		pairs:   map[string][]string{"30006018": {"30006014", "30006099"}},
	}
	_, handler := DeparturesTool(mockHTTP, mockSearch)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"stop_name": "Nicolaas Beetsstraat", "limit": float64(1), "include_paired": true}
	if _, err := handler(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if url := mockHTTP.lastReq.URL.String(); !strings.HasSuffix(url, "/tpc/30006018") {
		t.Errorf("expected only the ranked stop, got %s", url)
	}
}

func TestSearchStopsTool_SeparatorOnlyQueryReturnsEmptyList(t *testing.T) {
	_, handler := SearchStopsTool(&mockStopSearcher{})
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "..."}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if text := result.Content[0].(mcp.TextContent).Text; !strings.Contains(text, `"stops":[]`) {
		t.Errorf("expected an empty list, got %s", text)
	}
}

func TestSortDeparturesByPlanned_TieBreakByJourneyID(t *testing.T) {
	deps := []LeanDeparture{
		{JourneyID: "b", Planned: "2026-04-21T23:48:29+02:00"},
		{JourneyID: "a", Planned: "2026-04-21T23:48:29+02:00"},
		{JourneyID: "c", Planned: "2026-04-21T23:40:00+02:00"},
	}
	sortDeparturesByPlanned(deps)
	if deps[0].JourneyID != "c" || deps[1].JourneyID != "a" || deps[2].JourneyID != "b" {
		t.Errorf("unexpected order: %v", []string{deps[0].JourneyID, deps[1].JourneyID, deps[2].JourneyID})
	}
}

func TestFilterPassMap_TieBreakDeterministicUnderMaxDepartures(t *testing.T) {
	pass := `{"LinePublicNumber":"17","TargetDepartureTime":"2026-04-21T23:48:29"}`
	passes := map[string]json.RawMessage{"b": json.RawMessage(pass), "a": json.RawMessage(pass), "c": json.RawMessage(pass)}
	now := time.Date(2026, 4, 21, 23, 0, 0, 0, amsterdamLoc)
	for i := 0; i < 30; i++ {
		kept := filterPassMap(passes, departureFilters{maxDepartures: 2}, now)
		if _, ok := kept["a"]; !ok || len(kept) != 2 {
			t.Fatalf("run %d: expected a and b to survive, got %v", i, kept)
		}
		if _, ok := kept["b"]; !ok {
			t.Fatalf("run %d: expected a and b to survive, got %v", i, kept)
		}
	}
}
