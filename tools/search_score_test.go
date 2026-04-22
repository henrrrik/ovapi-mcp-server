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
		{TPCCode: "P", Name: "Plaza Zuid", StopAreaCode: ptrString("AREA1")},
		{TPCCode: "Q", Name: "Plaza Zuid"},
	}}
	// Query "Plaza" is a word-boundary match (not exact), so the boost applies.
	resp := runSearchTool(t, mock, map[string]any{"query": "Plaza"})
	if len(resp.Stops) < 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Stops))
	}
	var pScore, qScore int
	for _, s := range resp.Stops {
		if s.TPCCode == "P" {
			pScore = s.Score
		} else {
			qScore = s.Score
		}
	}
	if pScore-qScore != hubBoostStopAreaCode {
		t.Errorf("expected stop_area_code boost of %d; got P=%d Q=%d",
			hubBoostStopAreaCode, pScore, qScore)
	}
}

func TestSearchScore_HubBoostScalesWithPairCount(t *testing.T) {
	mock := &mockStopSearcher{
		results: []db.Stop{
			{TPCCode: "H", Name: "Zuidplein halte"},
			{TPCCode: "S", Name: "Zuidplein halte"},
		},
		pairs: map[string][]string{
			"H": {"H2", "H3"}, // 2 pairs → +20
			"S": {"S2"},       // 1 pair  → +10
		},
	}
	resp := runSearchTool(t, mock, map[string]any{"query": "Zuidplein"})
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
	if want := hubBoostPerPair; hScore-sScore != want {
		t.Errorf("expected per-pair gap of %d (2 pairs vs 1); got H=%d S=%d", want, hScore, sScore)
	}
}

func TestSearchScore_HubBoostFromCanonicalName(t *testing.T) {
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "A", Name: "Schiphol, Airport"},
		{TPCCode: "B", Name: "Schiphol Plaza"},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "Schiphol"})
	if len(resp.Stops) < 2 {
		t.Fatalf("expected 2 results")
	}
	// Schiphol Airport hits the canonical-name boost (+25); Plaza does not.
	var aScore, bScore int
	for _, s := range resp.Stops {
		switch s.TPCCode {
		case "A":
			aScore = s.Score
		case "B":
			bScore = s.Score
		}
	}
	if aScore-bScore != hubBoostCanonicalName {
		t.Errorf("expected canonical-name boost of %d; got A=%d B=%d",
			hubBoostCanonicalName, aScore, bScore)
	}
}

func TestSearchScore_Schiphol_AirportBeatsInterchange(t *testing.T) {
	// Regression: Knooppunt Schiphol Nrd scored the same as the airport.
	mock := &mockStopSearcher{
		results: []db.Stop{
			{TPCCode: "57330760", Name: "Schiphol, Airport", StopAreaCode: ptrString("schns")},
			{TPCCode: "00000001", Name: "Knooppunt Schiphol Nrd"},
		},
		pairs: map[string][]string{
			"57330760": {"a", "b", "c", "d", "e"}, // 5 platforms
			"00000001": {"a2"},                    // highway stop, lonely
		},
	}
	resp := runSearchTool(t, mock, map[string]any{"query": "Schiphol"})
	if len(resp.Stops) < 2 {
		t.Fatal("expected both results")
	}
	if resp.Stops[0].TPCCode != "57330760" {
		t.Errorf("expected airport first, got %q (%q)",
			resp.Stops[0].TPCCode, resp.Stops[0].Name)
	}
}

func TestSearchScore_ExactMatchNotBoosted(t *testing.T) {
	// Exact-match query must score exactly the cap, regardless of hub
	// features — the boost would otherwise go negative (already capped).
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "A", Name: "Amsterdam Centraal", StopAreaCode: ptrString("AREA")},
		{TPCCode: "B", Name: "Amsterdam Centraal"},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "Amsterdam Centraal"})
	if len(resp.Stops) < 2 {
		t.Fatal("expected 2 exact matches")
	}
	for _, s := range resp.Stops {
		if s.Score != scoreExactFullMatch {
			t.Errorf("expected exact score %d, got %d for %q",
				scoreExactFullMatch, s.Score, s.TPCCode)
		}
	}
}

func TestSearchScore_CSAlias_UtrechtCentraalFindsCS(t *testing.T) {
	// Regression: "Utrecht Centraal" previously scored "Utrecht, CS Centrumzijde"
	// below the floor because neither "CS" nor "Centrumzijde" matched
	// "Centraal" as tokens.
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "90000438", Name: "Utrecht, CS Centrumzijde"},
		{TPCCode: "90000439", Name: "Utrecht, CS Jaarbeurszijde"},
		{TPCCode: "ZOO", Name: "Utrecht, Centraal Museum"},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "Utrecht Centraal"})
	if len(resp.Stops) == 0 {
		t.Fatal("expected some results")
	}
	hasCS := false
	for _, s := range resp.Stops {
		if s.TPCCode == "90000438" {
			hasCS = true
			break
		}
	}
	if !hasCS {
		t.Errorf("expected CS Centrumzijde (90000438) among results, got %+v", resp.Stops)
	}
}

func TestSearchScore_CSQueryFindsCentraal(t *testing.T) {
	// Symmetric: a bare "CS" query should reach stops named only "Centraal".
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "A", Name: "Den Haag, Centraal Station"},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "Den Haag CS"})
	if len(resp.Stops) == 0 {
		t.Error("expected 'Den Haag CS' to find 'Den Haag, Centraal Station'")
	}
}

func TestSearchScore_ExactMatchStillScoresCapWithAlias(t *testing.T) {
	// Alias expansion must not destabilize exact-match detection.
	mock := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "R", Name: "Rotterdam Centraal"},
	}}
	resp := runSearchTool(t, mock, map[string]any{"query": "Rotterdam Centraal"})
	if len(resp.Stops) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Stops))
	}
	if resp.Stops[0].Score != scoreExactFullMatch {
		t.Errorf("expected exact score %d, got %d", scoreExactFullMatch, resp.Stops[0].Score)
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

// TestSearchScore_CityQueries_FavorHubs pins the behaviour called out in the
// review: when a bare city name is ambiguous between a major hub and a minor
// stop sharing a prefix, the hub must win. Before the rank boosts, queries
// like "Schiphol" would surface "Schipholweg" (a Leiden bus stop) above
// "Schiphol, Airport" because pg_trgm similarity favours closer length.
func TestSearchScore_CityQueries_FavorHubs(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		candidates []db.Stop
		wantWinner string // TPCCode of the expected top result
	}{
		{
			name:  "Schiphol picks the airport, not the Leiden bus stop",
			query: "Schiphol",
			candidates: []db.Stop{
				{TPCCode: "airport", Name: "Schiphol, Airport", StopAreaCode: ptrString("schns")},
				{TPCCode: "weg", Name: "Schipholweg"},
			},
			wantWinner: "airport",
		},
		{
			name:  "Amsterdam picks Centraal over a random street",
			query: "Amsterdam",
			candidates: []db.Stop{
				{TPCCode: "acs", Name: "Amsterdam, Centraal Station", StopAreaCode: ptrString("asdcs")},
				{TPCCode: "asw", Name: "Amsterdamsestraatweg"},
			},
			wantWinner: "acs",
		},
		{
			name:  "Utrecht picks Centraal over Utrechtseweg",
			query: "Utrecht",
			candidates: []db.Stop{
				{TPCCode: "ucs", Name: "Utrecht Centraal", StopAreaCode: ptrString("utc")},
				{TPCCode: "uw", Name: "Utrechtseweg"},
			},
			wantWinner: "ucs",
		},
		{
			name:  "Rotterdam picks Centraal over Rotterdamsestraat",
			query: "Rotterdam",
			candidates: []db.Stop{
				{TPCCode: "rcs", Name: "Rotterdam Centraal", StopAreaCode: ptrString("rtd")},
				{TPCCode: "rs", Name: "Rotterdamsestraat"},
			},
			wantWinner: "rcs",
		},
		{
			name:  "Den Haag picks Centraal Station over a tram stop",
			query: "Den Haag",
			candidates: []db.Stop{
				{TPCCode: "dhs", Name: "Den Haag, Centraal Station", StopAreaCode: ptrString("gvc")},
				{TPCCode: "dhf", Name: "Den Haag, Frederikstraat"},
			},
			wantWinner: "dhs",
		},
		{
			name:  "Leiden picks Centraal over Leidsestraat",
			query: "Leiden",
			candidates: []db.Stop{
				{TPCCode: "lcs", Name: "Leiden Centraal", StopAreaCode: ptrString("ledn")},
				{TPCCode: "ls", Name: "Leidsestraat"},
			},
			wantWinner: "lcs",
		},
		{
			name:  "Eindhoven picks the Station over a random road",
			query: "Eindhoven",
			candidates: []db.Stop{
				{TPCCode: "ecs", Name: "Eindhoven, Centraal Station", StopAreaCode: ptrString("ehv")},
				{TPCCode: "ed", Name: "Eindhovensedijk"},
			},
			wantWinner: "ecs",
		},
		{
			name:  "Amersfoort picks Centraal over Amersfoortseweg",
			query: "Amersfoort",
			candidates: []db.Stop{
				{TPCCode: "amfs", Name: "Amersfoort Centraal", StopAreaCode: ptrString("amf")},
				{TPCCode: "afw", Name: "Amersfoortseweg"},
			},
			wantWinner: "amfs",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockStopSearcher{results: tc.candidates}
			resp := runSearchTool(t, mock, map[string]any{"query": tc.query})
			if len(resp.Stops) == 0 {
				t.Fatalf("expected at least one result for query %q", tc.query)
			}
			if resp.Stops[0].TPCCode != tc.wantWinner {
				t.Errorf("query %q: want winner %q (%q), got %q (%q) with scores %+v",
					tc.query, tc.wantWinner, nameOf(tc.candidates, tc.wantWinner),
					resp.Stops[0].TPCCode, resp.Stops[0].Name, scoreSummary(resp.Stops))
			}
		})
	}
}

func nameOf(stops []db.Stop, tpc string) string {
	for _, s := range stops {
		if s.TPCCode == tpc {
			return s.Name
		}
	}
	return ""
}

func scoreSummary(stops []SearchResultStop) []string {
	out := make([]string, 0, len(stops))
	for _, s := range stops {
		out = append(out, s.TPCCode+"="+s.Name)
	}
	return out
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
