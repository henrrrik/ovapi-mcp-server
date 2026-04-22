package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/henrrrik/ovapi-mcp-server/db"
)

// fixedTime pins timeNow for deterministic filter tests.
// The live fixture's earliest departure is 2026-04-21T23:48:29 Amsterdam;
// pinning to 23:45 local puts that ~3m in the future.
func fixedTime(t *testing.T) func() {
	t.Helper()
	loc, _ := time.LoadLocation("Europe/Amsterdam")
	now := time.Date(2026, 4, 21, 23, 45, 0, 0, loc)
	prev := timeNow
	timeNow = func() time.Time { return now }
	return func() { timeNow = prev }
}

func TestDeparturesTool_LegacyFixtureRoundTrips(t *testing.T) {
	body := loadTestData(t, "tpc.json")
	mockHTTP := newMockDoer(body)
	mockSearch := &mockStopSearcher{
		results: []db.Stop{
			{TPCCode: "30005011", Name: "Centraal Station"},
			{TPCCode: "30005020", Name: "Centraal Station"},
		},
	}

	_, handler := DeparturesTool(mockHTTP, mockSearch)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"stop_name": "Centraal Station",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result")
	}
	if mockSearch.lastQ != "Centraal Station" {
		t.Errorf("expected search query 'Centraal Station', got %q", mockSearch.lastQ)
	}
	// get_departures now over-fetches and re-ranks the same way search_stops
	// does, so the DB sees default limit (3) * fanout.
	if want := 3 * searchCandidateFanout; mockSearch.lastLim != want {
		t.Errorf("expected DB limit %d, got %d", want, mockSearch.lastLim)
	}
	url := mockHTTP.lastReq.URL.String()
	if !strings.Contains(url, "/tpc/30005011,30005020") {
		t.Errorf("unexpected URL: %s", url)
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "Utrecht Centraal") {
		t.Error("result should contain fixture stop name")
	}
}

func TestDeparturesTool_MissingParam(t *testing.T) {
	mockHTTP := newMockDoer("{}")
	mockSearch := &mockStopSearcher{}

	_, handler := DeparturesTool(mockHTTP, mockSearch)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result when both stop_name and tpc_code missing")
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "tpc_code") {
		t.Errorf("expected error to mention tpc_code option, got %q", text)
	}
}

func TestDeparturesTool_NoResults(t *testing.T) {
	mockHTTP := newMockDoer("{}")
	mockSearch := &mockStopSearcher{results: []db.Stop{}}

	_, handler := DeparturesTool(mockHTTP, mockSearch)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"stop_name": "Nonexistent Stop",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result when no stops found")
	}
}

func TestDeparturesTool_SearchError(t *testing.T) {
	mockHTTP := newMockDoer("{}")
	mockSearch := &mockStopSearcher{err: errors.New("db connection failed")}

	_, handler := DeparturesTool(mockHTTP, mockSearch)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"stop_name": "Amsterdam",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result on search failure")
	}
}

func TestDeparturesTool_LimitClamping(t *testing.T) {
	mockHTTP := newMockDoer("{}")
	mockSearch := &mockStopSearcher{results: []db.Stop{
		{TPCCode: "30005011", Name: "Test"},
	}}

	_, handler := DeparturesTool(mockHTTP, mockSearch)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"stop_name": "test",
		"limit":     float64(100),
	}

	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := 10 * searchCandidateFanout; mockSearch.lastLim != want {
		t.Errorf("expected DB limit %d (clamped 10 * fanout), got %d", want, mockSearch.lastLim)
	}
}

func runLean(t *testing.T, args map[string]any) LeanResponse {
	t.Helper()
	body := loadTestData(t, "tpc_live.json")
	mockHTTP := newMockDoer(body)

	_, handler := DeparturesTool(mockHTTP, nil)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = args

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content[0].(mcp.TextContent).Text)
	}
	text := result.Content[0].(mcp.TextContent).Text
	var parsed LeanResponse
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("response is not valid LeanResponse JSON: %v\nbody: %s", err, text)
	}
	return parsed
}

func findStop(t *testing.T, resp LeanResponse, tpc string) *LeanStop {
	t.Helper()
	for i := range resp.Stops {
		if resp.Stops[i].TPCCode == tpc {
			return &resp.Stops[i]
		}
	}
	t.Fatalf("stop %s missing from response", tpc)
	return nil
}

func TestDeparturesTool_LeanShape_StopFields(t *testing.T) {
	defer fixedTime(t)()
	resp := runLean(t, map[string]any{"tpc_code": "30006018,30006014"})

	if len(resp.Stops) != 2 {
		t.Fatalf("expected 2 stops, got %d", len(resp.Stops))
	}
	nb := findStop(t, resp, "30006018")
	if nb.Name != "Nicolaas Beetsstraat" {
		t.Errorf("name = %q", nb.Name)
	}
	if nb.Coord[0] == 0 || nb.Coord[1] == 0 {
		t.Errorf("coord = %v", nb.Coord)
	}
	// Fixture town is "unknown" upstream, so lean shape should omit it.
	if nb.Town != "" {
		t.Errorf("expected town to be omitted when upstream is 'unknown', got %q", nb.Town)
	}
}

func TestDeparturesTool_LeanShape_DepartureFields(t *testing.T) {
	defer fixedTime(t)()
	resp := runLean(t, map[string]any{"tpc_code": "30006018"})

	nb := findStop(t, resp, "30006018")
	if len(nb.Departures) == 0 {
		t.Fatal("expected departures")
	}
	d := nb.Departures[0]
	if d.Line == "" {
		t.Error("line empty")
	}
	if d.Mode != strings.ToLower(d.Mode) || d.Mode == "" {
		t.Errorf("mode = %q (expected non-empty lowercase)", d.Mode)
	}
	if d.Destination == "" {
		t.Error("destination empty")
	}
	if d.JourneyID == "" {
		t.Error("journey_id empty")
	}
	if !strings.Contains(d.Planned, "+02:00") && !strings.Contains(d.Planned, "+01:00") {
		t.Errorf("planned = %q (expected Amsterdam offset)", d.Planned)
	}
	if d.Status == "" {
		t.Error("status empty")
	}
}

func TestDeparturesTool_LeanShape_SortedByPlanned(t *testing.T) {
	defer fixedTime(t)()
	resp := runLean(t, map[string]any{"tpc_code": "30006018"})
	nb := findStop(t, resp, "30006018")
	for i := 1; i < len(nb.Departures); i++ {
		if nb.Departures[i-1].Planned > nb.Departures[i].Planned {
			t.Fatalf("not sorted at index %d: %q > %q",
				i, nb.Departures[i-1].Planned, nb.Departures[i].Planned)
		}
	}
}

func TestDeparturesTool_ResponseSize(t *testing.T) {
	defer fixedTime(t)()

	body := loadTestData(t, "tpc_live.json")
	mockHTTP := newMockDoer(body)

	_, handler := DeparturesTool(mockHTTP, nil)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_code": "30006018,30006014",
	}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	// Wishlist target: < 8 KB per stop; raw fixture is ~22 KB for two stops.
	// Expect the lean shape to be well under the raw size.
	if len(text) >= len(body) {
		t.Errorf("lean shape not smaller than raw: lean=%d raw=%d", len(text), len(body))
	}
	if len(text) > 16000 {
		t.Errorf("lean response too large: %d bytes", len(text))
	}
}

func TestDeparturesTool_Verbose(t *testing.T) {
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
	// Verbose passes through upstream fields like JourneyPatternCode.
	if !strings.Contains(text, "JourneyPatternCode") {
		t.Error("verbose response should include raw upstream fields")
	}
}

func TestDeparturesTool_TPCCodeSkipsSearch(t *testing.T) {
	body := loadTestData(t, "tpc_live.json")
	mockHTTP := newMockDoer(body)
	mockSearch := &mockStopSearcher{}

	_, handler := DeparturesTool(mockHTTP, mockSearch)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_code": "30006018",
	}

	if _, err := handler(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mockSearch.lastQ != "" {
		t.Error("search should not be invoked when tpc_code is provided")
	}
	url := mockHTTP.lastReq.URL.String()
	if !strings.Contains(url, "/tpc/30006018") {
		t.Errorf("unexpected URL: %s", url)
	}
}

func TestDeparturesTool_LineFilter(t *testing.T) {
	defer fixedTime(t)()

	body := loadTestData(t, "tpc_live.json")
	mockHTTP := newMockDoer(body)

	_, handler := DeparturesTool(mockHTTP, nil)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_code": "30006018,30006014",
		"line":     "17",
	}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var parsed LeanResponse
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	total := 0
	for _, s := range parsed.Stops {
		for _, d := range s.Departures {
			if d.Line != "17" {
				t.Errorf("expected only line 17 departures, got %q", d.Line)
			}
			total++
		}
	}
	if total == 0 {
		t.Fatal("expected at least one line-17 departure in fixture")
	}
}

func TestDeparturesTool_DirectionFilter(t *testing.T) {
	defer fixedTime(t)()

	body := loadTestData(t, "tpc_live.json")
	mockHTTP := newMockDoer(body)

	_, handler := DeparturesTool(mockHTTP, nil)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_code":  "30006018,30006014",
		"direction": "centraal",
	}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var parsed LeanResponse
	_ = json.Unmarshal([]byte(text), &parsed)
	total := 0
	for _, s := range parsed.Stops {
		for _, d := range s.Departures {
			if !strings.Contains(strings.ToLower(d.Destination), "centraal") {
				t.Errorf("expected destination to contain 'centraal', got %q", d.Destination)
			}
			total++
		}
	}
	if total == 0 {
		t.Fatal("expected at least one matching departure")
	}
}

func TestDeparturesTool_TimeWindowFilter(t *testing.T) {
	defer fixedTime(t)()

	body := loadTestData(t, "tpc_live.json")
	mockHTTP := newMockDoer(body)

	_, handler := DeparturesTool(mockHTTP, nil)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_code":            "30006018,30006014",
		"time_window_minutes": float64(10),
	}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var parsed LeanResponse
	_ = json.Unmarshal([]byte(text), &parsed)

	// With fixed time 23:45 and 10m window, only departures up to 23:55 qualify.
	// Fixture has a pass at 23:48:29 which should be included.
	found := false
	for _, s := range parsed.Stops {
		for _, d := range s.Departures {
			if d.Planned > "2026-04-21T23:55:00" {
				t.Errorf("departure outside time window: %q", d.Planned)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("expected at least one departure in 10m window")
	}
}

func TestDeparturesTool_MaxDepartures(t *testing.T) {
	defer fixedTime(t)()

	body := loadTestData(t, "tpc_live.json")
	mockHTTP := newMockDoer(body)

	_, handler := DeparturesTool(mockHTTP, nil)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_code":       "30006018,30006014",
		"max_departures": float64(2),
	}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text

	var parsed LeanResponse
	_ = json.Unmarshal([]byte(text), &parsed)
	for _, s := range parsed.Stops {
		if len(s.Departures) > 2 {
			t.Errorf("stop %s: expected at most 2 departures, got %d", s.TPCCode, len(s.Departures))
		}
	}
}

func TestDeparturesTool_TPCCodeAndStopNameBothBlank(t *testing.T) {
	mockHTTP := newMockDoer("{}")
	mockSearch := &mockStopSearcher{}

	_, handler := DeparturesTool(mockHTTP, mockSearch)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_code": ",  ,",
	}

	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected error when tpc_code is only whitespace/commas")
	}
}

func TestTransformPass_RealtimeFlag(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"DRIVING", true},
		{"PASSED", true},
		{"ARRIVED", true},
		{"PLANNED", false},
		{"", false},
	}
	for _, tc := range cases {
		dep, ok := transformPass("id", rawPass{
			LinePublicNumber:    "1",
			TargetDepartureTime: "2026-04-21T23:48:29",
			TripStopStatus:      tc.status,
		}, departureFilters{}, time.Now())
		if !ok {
			t.Fatalf("unexpected drop for status %q", tc.status)
		}
		if dep.Realtime != tc.want {
			t.Errorf("status %q: realtime = %v, want %v", tc.status, dep.Realtime, tc.want)
		}
	}
}

func TestDeparturesTool_PairedWith(t *testing.T) {
	defer fixedTime(t)()

	body := loadTestData(t, "tpc_live.json")
	mockHTTP := newMockDoer(body)
	mockSearch := &mockStopSearcher{
		pairs: map[string][]string{
			"30006018": {"30006014"},
			"30006014": {"30006018"},
		},
	}

	_, handler := DeparturesTool(mockHTTP, mockSearch)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_code": "30006018,30006014",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(mcp.TextContent).Text)
	}
	text := result.Content[0].(mcp.TextContent).Text

	var parsed LeanResponse
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	byCode := map[string]LeanStop{}
	for _, s := range parsed.Stops {
		byCode[s.TPCCode] = s
	}
	if got := byCode["30006018"].PairedWith; len(got) != 1 || got[0] != "30006014" {
		t.Errorf("stop 30006018: expected paired_with=[30006014], got %v", got)
	}
	if got := byCode["30006014"].PairedWith; len(got) != 1 || got[0] != "30006018" {
		t.Errorf("stop 30006014: expected paired_with=[30006018], got %v", got)
	}
	if len(mockSearch.lastPairReq) != 2 {
		t.Errorf("expected 2 codes sent to PairedStopsByCode, got %v", mockSearch.lastPairReq)
	}
}

func TestDeparturesTool_NilSearcher_SkipsPairing(t *testing.T) {
	defer fixedTime(t)()

	body := loadTestData(t, "tpc_live.json")
	mockHTTP := newMockDoer(body)

	_, handler := DeparturesTool(mockHTTP, nil)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_code": "30006018",
	}

	result, _ := handler(context.Background(), req)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(mcp.TextContent).Text)
	}
	text := result.Content[0].(mcp.TextContent).Text
	if strings.Contains(text, "paired_with") {
		t.Errorf("expected no paired_with when searcher is nil, got %s", text)
	}
}

// TestDeparturesTool_StopNameGoesThroughRanker guards against the regression
// where /get_departures?stop_name=Schiphol resolved to "Schipholweg" (a
// Leiden bus stop) because the handler bypassed the rank logic and used raw
// pg_trgm ordering. The rank-aware resolver should pick the hub.
func TestDeparturesTool_StopNameGoesThroughRanker(t *testing.T) {
	mockHTTP := newMockDoer("{}")
	mockSearch := &mockStopSearcher{
		// Order intentionally reflects raw pg_trgm ranking — Schipholweg first.
		results: []db.Stop{
			{TPCCode: "weg", Name: "Schipholweg"},
			{TPCCode: "airport", Name: "Schiphol, Airport", StopAreaCode: ptrString("schns")},
		},
	}

	_, handler := DeparturesTool(mockHTTP, mockSearch)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"stop_name": "Schiphol", "limit": float64(1)}

	if _, err := handler(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	url := mockHTTP.lastReq.URL.String()
	if !strings.HasSuffix(url, "/tpc/airport") {
		t.Errorf("expected /tpc/airport (hub), got %s", url)
	}
}

func TestTransformPass_DelaySeconds(t *testing.T) {
	dep, _ := transformPass("id", rawPass{
		LinePublicNumber:      "1",
		TargetDepartureTime:   "2026-04-21T23:48:29",
		ExpectedDepartureTime: "2026-04-21T23:49:59",
		TripStopStatus:        "DRIVING",
	}, departureFilters{}, time.Now())
	if dep.DelaySeconds != 90 {
		t.Errorf("expected 90s delay, got %d", dep.DelaySeconds)
	}
}
