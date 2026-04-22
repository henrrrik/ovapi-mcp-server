package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// runVerboseMap is a helper that runs get_departures with verbose=true and
// parses the response into an upstream-shaped map (tpc_code -> stop entry).
func runVerboseMap(t *testing.T, args map[string]any) map[string]map[string]any {
	t.Helper()
	body := loadTestData(t, "tpc_live.json")
	mockHTTP := newMockDoer(body)

	_, handler := DeparturesTool(mockHTTP, nil)

	req := mcp.CallToolRequest{}
	args["verbose"] = true
	req.Params.Arguments = args

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text
	var parsed map[string]map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("could not parse verbose response: %v\nbody: %s", err, text)
	}
	return parsed
}

func countVerbosePasses(resp map[string]map[string]any) int {
	n := 0
	for _, entry := range resp {
		passes, ok := entry["Passes"].(map[string]any)
		if !ok {
			continue
		}
		n += len(passes)
	}
	return n
}

func TestDeparturesVerbose_MaxDeparturesFilterApplied(t *testing.T) {
	defer fixedTime(t)()

	resp := runVerboseMap(t, map[string]any{
		"tpc_code":       "30006018,30006014",
		"max_departures": float64(2),
	})

	for code, entry := range resp {
		passes, ok := entry["Passes"].(map[string]any)
		if !ok {
			t.Fatalf("stop %s: Passes missing or wrong type", code)
		}
		if len(passes) > 2 {
			t.Errorf("stop %s: expected <=2 verbose passes, got %d", code, len(passes))
		}
	}
}

func TestDeparturesVerbose_LineFilterApplied(t *testing.T) {
	defer fixedTime(t)()

	resp := runVerboseMap(t, map[string]any{
		"tpc_code": "30006018,30006014",
		"line":     "17",
	})
	if countVerbosePasses(resp) == 0 {
		t.Fatal("expected some line-17 passes in verbose response")
	}
	for _, entry := range resp {
		passes, _ := entry["Passes"].(map[string]any)
		for _, v := range passes {
			p, _ := v.(map[string]any)
			if ln, _ := p["LinePublicNumber"].(string); ln != "17" {
				t.Errorf("expected only line 17, got %q", ln)
			}
		}
	}
}

func TestDeparturesVerbose_DirectionFilterApplied(t *testing.T) {
	defer fixedTime(t)()

	resp := runVerboseMap(t, map[string]any{
		"tpc_code":  "30006018,30006014",
		"direction": "centraal",
	})
	if countVerbosePasses(resp) == 0 {
		t.Fatal("expected some matching passes")
	}
	for _, entry := range resp {
		passes, _ := entry["Passes"].(map[string]any)
		for _, v := range passes {
			p, _ := v.(map[string]any)
			dest, _ := p["DestinationName50"].(string)
			if !strings.Contains(strings.ToLower(dest), "centraal") {
				t.Errorf("expected destination to contain centraal, got %q", dest)
			}
		}
	}
}

func TestDeparturesVerbose_TimeWindowApplied(t *testing.T) {
	defer fixedTime(t)()

	baseline := runVerboseMap(t, map[string]any{
		"tpc_code": "30006018,30006014",
	})
	filtered := runVerboseMap(t, map[string]any{
		"tpc_code":            "30006018,30006014",
		"time_window_minutes": float64(5),
	})
	if countVerbosePasses(filtered) >= countVerbosePasses(baseline) {
		t.Errorf("expected fewer passes after time_window_minutes=5 filter (baseline=%d filtered=%d)",
			countVerbosePasses(baseline), countVerbosePasses(filtered))
	}
}

func TestDeparturesVerbose_ParityWithLean(t *testing.T) {
	defer fixedTime(t)()
	// For every filter combination, the number of kept passes should match
	// between lean and verbose modes.
	cases := []map[string]any{
		{"line": "17"},
		{"direction": "centraal"},
		{"time_window_minutes": float64(10)},
		{"max_departures": float64(3)},
		{"line": "17", "max_departures": float64(1)},
	}
	for i, extra := range cases {
		leanArgs := map[string]any{"tpc_code": "30006018,30006014"}
		verboseArgs := map[string]any{"tpc_code": "30006018,30006014"}
		for k, v := range extra {
			leanArgs[k] = v
			verboseArgs[k] = v
		}
		lean := runLeanFromFixture(t, "tpc_live.json", leanArgs)
		verbose := runVerboseMap(t, verboseArgs)

		leanCount := 0
		for _, s := range lean.Stops {
			leanCount += len(s.Departures)
		}
		verboseCount := countVerbosePasses(verbose)
		if leanCount != verboseCount {
			t.Errorf("case %d (%v): lean=%d verbose=%d departures", i, extra, leanCount, verboseCount)
		}
	}
}

func TestDeparturesVerbose_NoFilters_PassesThroughRaw(t *testing.T) {
	body := loadTestData(t, "tpc_live.json")
	mockHTTP := newMockDoer(body)

	_, handler := DeparturesTool(mockHTTP, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_code": "30006018",
		"verbose":  true,
	}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "JourneyPatternCode") {
		t.Error("unfiltered verbose should pass through raw upstream keys")
	}
}
