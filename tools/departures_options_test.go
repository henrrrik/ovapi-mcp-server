package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestDeparturesTool_IncludePaired_ExpandsCodes(t *testing.T) {
	defer fixedTime(t)()

	body := loadTestData(t, "tpc_live.json")
	mockHTTP := newMockDoer(body)
	mockSearch := &mockStopSearcher{
		pairs: map[string][]string{
			"30006018": {"30006014"},
		},
	}

	_, handler := DeparturesTool(mockHTTP, mockSearch)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_code":       "30006018",
		"include_paired": true,
	}

	if _, err := handler(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	url := mockHTTP.lastReq.URL.String()
	if !strings.Contains(url, "30006018") || !strings.Contains(url, "30006014") {
		t.Errorf("expected URL to include both codes, got: %s", url)
	}
}

func TestDeparturesTool_IncludePaired_NilSearcher_NoExpansion(t *testing.T) {
	defer fixedTime(t)()

	body := loadTestData(t, "tpc_live.json")
	mockHTTP := newMockDoer(body)

	_, handler := DeparturesTool(mockHTTP, nil)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_code":       "30006018",
		"include_paired": true,
	}

	if _, err := handler(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	url := mockHTTP.lastReq.URL.String()
	if strings.Contains(url, "30006014") {
		t.Errorf("expected no expansion without searcher, got: %s", url)
	}
}

func TestDeparturesTool_DropEmpty_RemovesEmptyStops(t *testing.T) {
	defer fixedTime(t)()

	body := loadTestData(t, "tpc_live.json")
	mockHTTP := newMockDoer(body)

	_, handler := DeparturesTool(mockHTTP, nil)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_code":   "30006018,30006014",
		"line":       "17",
		"direction":  "osdorp",
		"drop_empty": true,
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text
	var parsed LeanResponse
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	// With line=17 and direction=osdorp, only 30006014 has matches.
	for _, s := range parsed.Stops {
		if len(s.Departures) == 0 {
			t.Errorf("drop_empty should have removed empty stop %s", s.TPCCode)
		}
	}
}

func TestDeparturesTool_DropEmpty_Off_KeepsEmpty(t *testing.T) {
	defer fixedTime(t)()

	body := loadTestData(t, "tpc_live.json")
	mockHTTP := newMockDoer(body)

	_, handler := DeparturesTool(mockHTTP, nil)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tpc_code":  "30006018,30006014",
		"line":      "17",
		"direction": "osdorp",
	}
	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text
	var parsed LeanResponse
	_ = json.Unmarshal([]byte(text), &parsed)
	// Without drop_empty, we should still see both stops even though one is empty.
	if len(parsed.Stops) != 2 {
		t.Errorf("expected 2 stops without drop_empty, got %d", len(parsed.Stops))
	}
}
