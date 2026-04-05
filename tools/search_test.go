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
	results []db.Stop
	err     error
	lastQ   string
	lastLim int
}

func (m *mockStopSearcher) SearchStops(_ context.Context, query string, limit int) ([]db.Stop, error) {
	m.lastQ = query
	m.lastLim = limit
	return m.results, m.err
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
	if mock.lastLim != 10 {
		t.Errorf("expected default limit 10, got %d", mock.lastLim)
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

	if mock.lastLim != 50 {
		t.Errorf("expected clamped limit 50, got %d", mock.lastLim)
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
