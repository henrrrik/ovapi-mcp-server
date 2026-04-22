package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/henrrrik/ovapi-mcp-server/db"
)

func ptrString(s string) *string { return &s }

// runSearchTool is a thin helper that invokes SearchStopsTool with a given
// mock searcher and returns the parsed SearchResponse.
func runSearchTool(t *testing.T, mock StopSearcher, args map[string]any) SearchResponse {
	t.Helper()
	_, handler := SearchStopsTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content[0].(mcp.TextContent).Text)
	}
	var parsed SearchResponse
	text := result.Content[0].(mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse: %v\nbody: %s", err, text)
	}
	return parsed
}

func TestSearchScore_MinQueryLength(t *testing.T) {
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "X", Name: "ut"}, {TPCCode: "Y", Name: "UT/De Zul"},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "ut"})
	if len(resp.Stops) != 0 {
		t.Errorf("expected empty results for 2-char query, got %d", len(resp.Stops))
	}
	// DB should not have been called.
	if mock.lastQ != "" {
		t.Errorf("expected DB untouched for sub-3-char query, got lastQ=%q", mock.lastQ)
	}
}

func TestSearchScore_NonsenseQueryFiltered(t *testing.T) {
	// Simulate DB returning loose trigram matches for a nonsense query.
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "A", Name: "Asldk Plaza"},
		{TPCCode: "B", Name: "Kfja Station"},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "asldkfjalsdkfj"})
	if len(resp.Stops) != 0 {
		t.Errorf("expected empty list for nonsense query, got %d", len(resp.Stops))
	}
}

func TestSearchScore_ExactFullMatchTopsResults(t *testing.T) {
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "A", Name: "Utrecht Centraal Museum"},
		{TPCCode: "B", Name: "Utrecht Centraal"},
		{TPCCode: "C", Name: "Utrecht CS"},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "Utrecht Centraal"})
	if len(resp.Stops) == 0 {
		t.Fatal("expected results")
	}
	if resp.Stops[0].TPCCode != "B" {
		t.Errorf("expected exact-match 'Utrecht Centraal' (B) first, got %q", resp.Stops[0].Name)
	}
	if resp.Stops[0].Score != scoreExactFullMatch {
		t.Errorf("expected score %d for exact match, got %d",
			scoreExactFullMatch, resp.Stops[0].Score)
	}
}

func TestSearchScore_TokenBoundaryBeatsSubstring(t *testing.T) {
	// For query "Schiphol", "Schiphol, Airport" has the token; "Schipholweg"
	// only matches as a substring. Token-boundary must win.
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "W", Name: "Schipholweg"},
		{TPCCode: "S", Name: "Schiphol, Airport"},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "Schiphol"})
	if len(resp.Stops) < 2 {
		t.Fatalf("expected both candidates, got %d", len(resp.Stops))
	}
	if resp.Stops[0].TPCCode != "S" {
		t.Errorf("expected 'Schiphol, Airport' first, got %q", resp.Stops[0].Name)
	}
}

func TestSearchScore_MultiTokenRejectsPartialMatches(t *testing.T) {
	// Query "Amsterdam Centraal" must NOT return "Rotterdam Centraal" among
	// top results — it lacks the "amsterdam" token.
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "R", Name: "Rotterdam Centraal"},
		{TPCCode: "A", Name: "Amsterdam, Centraal Station"},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "Amsterdam Centraal"})
	if len(resp.Stops) == 0 {
		t.Fatal("expected at least one result")
	}
	if resp.Stops[0].TPCCode != "A" {
		t.Errorf("expected Amsterdam first, got %q", resp.Stops[0].Name)
	}
	// Rotterdam scores as "some tokens match" (just "centraal"); with a
	// floor of 200, score 400 keeps it around — but it must not outrank
	// Amsterdam's all-tokens-boundary hit.
	for _, s := range resp.Stops {
		if s.TPCCode == "R" && s.Score >= resp.Stops[0].Score {
			t.Errorf("Rotterdam score %d should be below Amsterdam score %d",
				s.Score, resp.Stops[0].Score)
		}
	}
}

func TestSearchScore_HubBoostFromStopAreaCode(t *testing.T) {
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "P", Name: "Plaza", StopAreaCode: ptrString("AREA1")},
		{TPCCode: "Q", Name: "Plaza"},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "Plaza"})
	if len(resp.Stops) < 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Stops))
	}
	// P and Q have the same name but P has a stop_area_code → higher score.
	var pScore, qScore int
	for _, s := range resp.Stops {
		if s.TPCCode == "P" {
			pScore = s.Score
		} else {
			qScore = s.Score
		}
	}
	if pScore <= qScore {
		t.Errorf("expected hub P score > Q; got %d vs %d", pScore, qScore)
	}
}

func TestSearchScore_HubBoostFromMultiplePairs(t *testing.T) {
	mock := &mockStopSearcher{
		results: []db.Stop{
			{TPCCode: "H", Name: "Station"},
			{TPCCode: "S", Name: "Station"},
		},
		pairs: map[string][]string{
			"H": {"H2", "H3"}, // 2+ pairs → hub
			"S": {"S2"},
		},
	}
	resp := runSearchTool(t, mock, map[string]any{"query": "Station"})
	if len(resp.Stops) < 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Stops))
	}
	var hScore, sScore int
	for _, st := range resp.Stops {
		if st.TPCCode == "H" {
			hScore = st.Score
		} else if st.TPCCode == "S" {
			sScore = st.Score
		}
	}
	if hScore <= sScore {
		t.Errorf("expected H (3 paired) score > S (1 paired); got %d vs %d", hScore, sScore)
	}
}

func TestSearchScore_CoordNullForSentinel(t *testing.T) {
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "X", Name: "Nowhere Halt", Latitude: 47.974766, Longitude: 3.3135424},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "Nowhere"})
	if len(resp.Stops) != 1 {
		t.Fatalf("expected 1 result")
	}
	if resp.Stops[0].Coord != nil {
		t.Errorf("expected coord:null for sentinel, got %v", resp.Stops[0].Coord)
	}
	// Confirm the JSON output carries null.
	text := mustJSON(t, resp)
	if !strings.Contains(text, `"coord":null`) {
		t.Errorf("expected coord:null in JSON, got: %s", text)
	}
}

func TestSearchScore_TownInferredFromStopNamePrefix(t *testing.T) {
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "X", Name: "Amsterdam, Leidseplein", Town: "unknown"},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "Leidseplein"})
	if len(resp.Stops) != 1 {
		t.Fatalf("expected 1 result")
	}
	if resp.Stops[0].Town != "Amsterdam" {
		t.Errorf("expected inferred town Amsterdam, got %q", resp.Stops[0].Town)
	}
}

func TestSearchScore_TownOmittedWhenUnknownAndNoPrefix(t *testing.T) {
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "X", Name: "Somewhere", Town: "unknown"},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "Somewhere"})
	if len(resp.Stops) != 1 {
		t.Fatalf("expected 1 result")
	}
	if resp.Stops[0].Town != "" {
		t.Errorf("expected town omitted, got %q", resp.Stops[0].Town)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
