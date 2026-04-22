package tools

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

type mockHTTPDoer struct {
	response *http.Response
	lastReq  *http.Request
}

func (m *mockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	m.lastReq = req
	return m.response, nil
}

func newMockDoer(body string) *mockHTTPDoer {
	return newMockDoerWithStatus(body, 200)
}

func newMockDoerWithStatus(body string, status int) *mockHTTPDoer {
	return &mockHTTPDoer{
		response: &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
		},
	}
}

func loadTestData(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLinesTool_NoParams(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatal("expected non-error result")
	}

	if !strings.HasSuffix(mock.lastReq.URL.String(), "/line") {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, `"lines"`) {
		t.Error("response should include lean shape with top-level 'lines' key")
	}
	if !strings.Contains(text, `"total"`) {
		t.Error("response should include total match count")
	}
}

func TestLinesTool_Verbose_BypassesLeanShape(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"verbose": true}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "LinePublicNumber") {
		t.Error("verbose=true should pass through raw upstream fields")
	}
}

func TestLinesTool_WithLineID(t *testing.T) {
	body := loadTestData(t, "line_detail_live.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"line_id": "GVB_17_1",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(mock.lastReq.URL.String(), "/line/GVB_17_1") {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, `"route"`) {
		t.Error("lean line-detail should include route key")
	}
	if !strings.Contains(text, `"active_journeys"`) {
		t.Error("lean line-detail should include active_journeys key")
	}
}

func TestLinesTool_WithLineID_Verbose(t *testing.T) {
	body := loadTestData(t, "line_detail_live.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"line_id": "GVB_17_1",
		"verbose": true,
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "JourneyPatternCode") {
		t.Error("verbose=true should include raw upstream fields")
	}
}

func TestJourneyTool(t *testing.T) {
	body := loadTestData(t, "journey_live.json")
	mock := newMockDoer(body)

	_, handler := JourneyTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"journey_id": "GVB_20260422_17_19206_0",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatal("expected non-error result")
	}

	if !strings.Contains(mock.lastReq.URL.String(), "/journey/GVB_20260422_17_19206_0") {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, `"stops"`) {
		t.Error("lean journey should include stops key")
	}
	if !strings.Contains(text, `"server_time"`) {
		t.Error("lean journey should include server_time key")
	}
}

func TestJourneyTool_Verbose(t *testing.T) {
	body := loadTestData(t, "journey_live.json")
	mock := newMockDoer(body)

	_, handler := JourneyTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"journey_id": "GVB_20260422_17_19206_0",
		"verbose":    true,
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "JourneyPatternCode") {
		t.Error("verbose=true should include raw upstream fields")
	}
}

func TestJourneyTool_MissingParam(t *testing.T) {
	mock := newMockDoer("{}")

	_, handler := JourneyTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result when journey_id is missing")
	}
}

func TestFetchJSON_HTTPError(t *testing.T) {
	mock := newMockDoerWithStatus("not found", 404)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Fatal("expected error result for HTTP 404")
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "404") {
		t.Errorf("expected error to mention status code, got %q", text)
	}
}
