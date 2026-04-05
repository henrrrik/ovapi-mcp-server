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

func TestDeparturesByStopTool(t *testing.T) {
	body := loadTestData(t, "tpc.json")
	mock := newMockDoer(body)

	_, handler := DeparturesByStopTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_codes": "ut010",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatal("expected non-error result")
	}

	if !strings.Contains(mock.lastReq.URL.String(), "/tpc/ut010") {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "Utrecht Centraal") {
		t.Error("result should contain fixture data")
	}
}

func TestDeparturesByStopTool_MissingParam(t *testing.T) {
	mock := newMockDoer("{}")

	_, handler := DeparturesByStopTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result when tpc_codes is missing")
	}
}

func TestDeparturesByAreaTool(t *testing.T) {
	body := loadTestData(t, "stopareacode.json")
	mock := newMockDoer(body)

	_, handler := DeparturesByAreaTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"stopareacode": "utcs",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatal("expected non-error result")
	}

	if !strings.Contains(mock.lastReq.URL.String(), "/stopareacode/utcs") {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "Utrecht Centraal") {
		t.Error("result should contain fixture data")
	}
}

func TestDeparturesByAreaTool_MissingParam(t *testing.T) {
	mock := newMockDoer("{}")

	_, handler := DeparturesByAreaTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result when stopareacode is missing")
	}
}

func TestLinesTool_NoParams(t *testing.T) {
	body := loadTestData(t, "lines.json")
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
	if !strings.Contains(text, "GVB") {
		t.Error("result should contain fixture data")
	}
}

func TestLinesTool_WithLineID(t *testing.T) {
	body := loadTestData(t, "line_detail.json")
	mock := newMockDoer(body)

	_, handler := LinesTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"line_id": "GVB_1_1",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(mock.lastReq.URL.String(), "/line/GVB_1_1") {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "Network") {
		t.Error("result should contain fixture data")
	}
}

func TestJourneyTool(t *testing.T) {
	body := loadTestData(t, "journey.json")
	mock := newMockDoer(body)

	_, handler := JourneyTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"journey_id": "journey123",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatal("expected non-error result")
	}

	if !strings.Contains(mock.lastReq.URL.String(), "/journey/journey123") {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "GVB") {
		t.Error("result should contain fixture data")
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

	_, handler := DeparturesByStopTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_codes": "ut010",
	}

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
