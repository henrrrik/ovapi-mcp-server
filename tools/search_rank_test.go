package tools

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/henrrrik/ovapi-mcp-server/db"
)

// expandHubAliases used to append aliases in Go map-iteration order, so the
// positional hasTokenPrefix check awarded its +30 at random between
// identical requests. Aliases must follow the token order and the
// hubAliases slice order.
func TestExpandHubAliases_Deterministic(t *testing.T) {
	want := []string{"den", "haag", "cs", "centraal", "station"}
	for i := 0; i < 200; i++ {
		got := expandHubAliases([]string{"den", "haag", "cs"})
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d: expandHubAliases = %v, want %v", i, got, want)
		}
	}
	// An alias already present as a token is not appended twice.
	got := expandHubAliases([]string{"utrecht", "centraal", "station"})
	if want := []string{"utrecht", "centraal", "station", "cs"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expandHubAliases = %v, want %v", got, want)
	}
}

// The token-prefix bonus is the only thing separating these two: neither
// has pairs, a stop_area_code or a canonical hub name.
func TestSearchScore_TokenPrefixBonusDecides(t *testing.T) {
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "knoop", Name: "Knooppunt Schiphol Nrd"},
		{TPCCode: "plaza", Name: "Schiphol, Plaza"},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "Schiphol"})
	if len(resp.Stops) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Stops))
	}
	if resp.Stops[0].TPCCode != "plaza" {
		t.Fatalf("expected 'Schiphol, Plaza' first, got %q", resp.Stops[0].Name)
	}
	if gap := resp.Stops[0].Score - resp.Stops[1].Score; gap != hubBoostTokenPrefix {
		t.Errorf("expected the prefix bonus (%d) to be the whole gap, got %d (%d vs %d)",
			hubBoostTokenPrefix, gap, resp.Stops[0].Score, resp.Stops[1].Score)
	}
}

// Query one token longer than the name, name with two aliases: this is the
// exact shape that flipped between runs before alias order was pinned.
func TestSearchScore_StableAcrossRuns(t *testing.T) {
	candidates := []db.Stop{
		{TPCCode: "cs", Name: "Den Haag, CS"},
		{TPCCode: "centraal", Name: "Den Haag, Centraal Station"},
	}
	var first []SearchResultStop
	for i := 0; i < 200; i++ {
		got := scoreAndRank("Den Haag CS Station", candidates, nil, 10)
		if first == nil {
			first = got
			continue
		}
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d ranked differently:\n first: %+v\n now:   %+v", i, first, got)
		}
	}
}

// "CS" is the Dutch shorthand for Centraal Station and marks a hub just as
// much as the spelled-out form does.
func TestSearchScore_CSNameIsCanonicalHub(t *testing.T) {
	for name, want := range map[string]bool{
		"Utrecht, CS Centrumzijde":     true,
		"Den Haag, CS":                 true,
		"Amsterdam, Centraal Station":  true,
		"Schiphol, Airport":            true,
		"Utrecht, 't Goylaan":          false,
		"Amsterdam, Pieter Calandlaan": false,
		"Acsent Business Park":         false, // "cs" inside a token is not the alias
	} {
		if got := isCanonicalHubName(name); got != want {
			t.Errorf("isCanonicalHubName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestSearchScore_Utrecht_CSBeatsMinorStopWithSameFeatures(t *testing.T) {
	pairs := map[string][]string{
		"cs":      {"a", "b", "c"},
		"goylaan": {"d", "e", "f"},
	}
	mock := &mockStopSearcher{
		results: []db.Stop{
			{TPCCode: "goylaan", Name: "Utrecht, 't Goylaan"},
			{TPCCode: "cs", Name: "Utrecht, CS Centrumzijde"},
		},
		pairs: pairs,
	}
	resp := runSearchTool(t, mock, map[string]any{"query": "Utrecht"})
	if len(resp.Stops) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Stops))
	}
	if resp.Stops[0].TPCCode != "cs" {
		t.Fatalf("expected 'Utrecht, CS Centrumzijde' first, got %q", resp.Stops[0].Name)
	}
	if gap := resp.Stops[0].Score - resp.Stops[1].Score; gap != hubBoostCanonicalName {
		t.Errorf("expected the canonical-name bonus (%d) to be the gap, got %d", hubBoostCanonicalName, gap)
	}
}

// A misspelling that pg_trgm still matches must not turn into "no stops
// found": when the tiered scorer drops every candidate, get_departures falls
// back to the raw similarity-ordered candidates, as it did before it was
// routed through the ranker. search_stops keeps its strict behaviour.
func TestDeparturesTool_StopName_TypoFallsBackToTrigramCandidates(t *testing.T) {
	mockHTTP := newMockDoer(loadTestData(t, "tpc_schiphol.json"))
	mockSearch := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "airport", Name: "Schiphol, Airport"},
	}}
	_, handler := DeparturesTool(mockHTTP, mockSearch)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"stop_name": "Schipol"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("expected departures for the trigram match, got tool error: %s",
			result.Content[0].(mcp.TextContent).Text)
	}
	if url := mockHTTP.lastReq.URL.String(); !strings.HasSuffix(url, "/tpc/airport") {
		t.Errorf("expected /tpc/airport, got %s", url)
	}
}

func TestDeparturesTool_StopName_TypoFallbackHonoursLimit(t *testing.T) {
	mockHTTP := newMockDoer(loadTestData(t, "tpc_schiphol.json"))
	mockSearch := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "c1", Name: "Schiphol, Airport"},
		{TPCCode: "c2", Name: "Schiphol, Plaza"},
		{TPCCode: "c3", Name: "Schiphol Noord"},
	}}
	_, handler := DeparturesTool(mockHTTP, mockSearch)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"stop_name": "Schipol", "limit": float64(2)}
	if _, err := handler(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if url := mockHTTP.lastReq.URL.String(); !strings.HasSuffix(url, "/tpc/c1,c2") {
		t.Errorf("expected the top 2 similarity-ordered candidates, got %s", url)
	}
}

func TestDeparturesTool_StopName_NoCandidatesStillErrors(t *testing.T) {
	mockSearch := &mockStopSearcher{results: nil}
	_, handler := DeparturesTool(newMockDoer("{}"), mockSearch)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"stop_name": "Xyzzy"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected a tool error when the database returns nothing at all")
	}
}

func TestSearchStopsTool_TypoStillFiltered(t *testing.T) {
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "airport", Name: "Schiphol, Airport"},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "Schipol"})
	if len(resp.Stops) != 0 {
		t.Errorf("search_stops keeps the score floor; got %+v", resp.Stops)
	}
}

// A one- or two-character stop_name was never searched, so "no stops found"
// was misleading; say what the caller has to change.
func TestDeparturesTool_StopName_TooShortIsExplicitError(t *testing.T) {
	mockSearch := &mockStopSearcher{lastLim: -1}
	_, handler := DeparturesTool(newMockDoer("{}"), mockSearch)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"stop_name": "CS"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected a tool error")
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "at least 3 characters") {
		t.Errorf("error should explain the minimum length, got %q", text)
	}
	if mockSearch.lastLim != -1 {
		t.Error("database should not be queried for a too-short name")
	}
}
