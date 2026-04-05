package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/henrrrik/ovapi-mcp-server/db"
)

func TestDeparturesTool(t *testing.T) {
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

	// Verify search was called
	if mockSearch.lastQ != "Centraal Station" {
		t.Errorf("expected search query 'Centraal Station', got %q", mockSearch.lastQ)
	}
	if mockSearch.lastLim != 3 {
		t.Errorf("expected default limit 3, got %d", mockSearch.lastLim)
	}

	// Verify TPC codes were joined in the URL
	url := mockHTTP.lastReq.URL.String()
	if !strings.Contains(url, "/tpc/30005011,30005020") {
		t.Errorf("unexpected URL: %s", url)
	}

	// Verify result contains fixture data
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "Utrecht Centraal") {
		t.Error("result should contain fixture data")
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
		t.Error("expected error result when stop_name is missing")
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

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "Nonexistent Stop") {
		t.Errorf("expected error to mention stop name, got %q", text)
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

	if mockSearch.lastLim != 10 {
		t.Errorf("expected clamped limit 10, got %d", mockSearch.lastLim)
	}
}
