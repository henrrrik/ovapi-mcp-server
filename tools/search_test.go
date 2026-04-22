package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/henrrrik/ovapi-mcp-server/db"
)

type mockStopSearcher struct {
	results     []db.Stop
	err         error
	lastQ       string
	lastLim     int
	pairs       map[string][]string
	pairsErr    error
	lastPairReq []string

	bboxResults []db.Stop
	bboxErr     error
	lastBBox    [4]float64
	lastBBoxLim int
}

func (m *mockStopSearcher) SearchStops(_ context.Context, query string, limit int) ([]db.Stop, error) {
	m.lastQ = query
	m.lastLim = limit
	return m.results, m.err
}

func (m *mockStopSearcher) PairedStopsByCode(_ context.Context, codes []string) (map[string][]string, error) {
	m.lastPairReq = append([]string{}, codes...)
	if m.pairs == nil {
		return map[string][]string{}, m.pairsErr
	}
	return m.pairs, m.pairsErr
}

func (m *mockStopSearcher) StopsInBBox(_ context.Context, minLat, maxLat, minLng, maxLng float64, limit int) ([]db.Stop, error) {
	m.lastBBox = [4]float64{minLat, maxLat, minLng, maxLng}
	m.lastBBoxLim = limit
	return m.bboxResults, m.bboxErr
}

func TestSearchStopsTool(t *testing.T) {
	mock := &mockStopSearcher{
		results: []db.Stop{
			{TPCCode: "30005011", Name: "Centraal Station", Latitude: 52.37811, Longitude: 4.899218},
		},
	}

	_, handler := SearchStopsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"query": "Centraal",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatal("expected non-error result")
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "30005011") {
		t.Error("result should contain TPC code")
	}
	if !strings.Contains(text, "Centraal Station") {
		t.Error("result should contain stop name")
	}
	if mock.lastQ != "Centraal" {
		t.Errorf("expected query 'Centraal', got %q", mock.lastQ)
	}
	// The tool over-fetches candidates before re-ranking, so the DB sees
	// default limit (10) * fanout.
	if want := 10 * searchCandidateFanout; mock.lastLim != want {
		t.Errorf("expected DB limit %d, got %d", want, mock.lastLim)
	}
}

func TestSearchStopsTool_MissingQuery(t *testing.T) {
	mock := &mockStopSearcher{}

	_, handler := SearchStopsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result when query is missing")
	}
}

func TestSearchStopsTool_LimitClamping(t *testing.T) {
	mock := &mockStopSearcher{results: []db.Stop{}}

	_, handler := SearchStopsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"query": "test",
		"limit": float64(100),
	}

	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if want := 50 * searchCandidateFanout; mock.lastLim != want {
		t.Errorf("expected DB limit %d (clamped 50 * fanout), got %d", want, mock.lastLim)
	}
}

func TestSearchStopsTool_SearchError(t *testing.T) {
	mock := &mockStopSearcher{err: errors.New("db connection failed")}

	_, handler := SearchStopsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"query": "Amsterdam",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result on search failure")
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "db connection failed") {
		t.Errorf("expected error message, got %q", text)
	}
}

func TestSearchStopsTool_IncludesPairedWith(t *testing.T) {
	mock := &mockStopSearcher{
		results: []db.Stop{
			{TPCCode: "30006018", Name: "Nicolaas Beetsstraat", Latitude: 52.365417, Longitude: 4.8653097},
			{TPCCode: "30006014", Name: "Nicolaas Beetsstraat", Latitude: 52.365543, Longitude: 4.8656464},
		},
		pairs: map[string][]string{
			"30006018": {"30006014"},
			"30006014": {"30006018"},
		},
	}

	_, handler := SearchStopsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "Nicolaas"}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(mcp.TextContent).Text)
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, `"paired_with":["30006014"]`) {
		t.Errorf("expected paired_with for 30006018, got %s", text)
	}
	if len(mock.lastPairReq) != 2 {
		t.Errorf("expected 2 codes sent to PairedStopsByCode, got %v", mock.lastPairReq)
	}
}

func TestSearchStopsTool_PairingErrorSurfaces(t *testing.T) {
	mock := &mockStopSearcher{
		results: []db.Stop{
			{TPCCode: "30006018", Name: "Nicolaas Beetsstraat"},
		},
		pairsErr: errors.New("pair query failed"),
	}

	_, handler := SearchStopsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "Nicolaas"}

	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Fatal("expected error result when pairing fails")
	}
	if !strings.Contains(result.Content[0].(mcp.TextContent).Text, "pair query failed") {
		t.Errorf("error text should surface pairing failure, got %q",
			result.Content[0].(mcp.TextContent).Text)
	}
}

func TestSearchStopsTool_OmitPairedWithWhenNone(t *testing.T) {
	mock := &mockStopSearcher{
		results: []db.Stop{
			{TPCCode: "30005011", Name: "Loners Lane"},
		},
		pairs: map[string][]string{},
	}

	_, handler := SearchStopsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "Loners"}

	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text
	if strings.Contains(text, "paired_with") {
		t.Errorf("expected paired_with to be omitted when empty, got %s", text)
	}
}
