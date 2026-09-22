package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// mcp-go performs no InputSchema validation and CallToolRequest.GetString
// returns the default for any non-string JSON value. An LLM caller that
// passes line: 17 instead of line: "17" would otherwise silently disable the
// filter. stringArg accepts numbers for string parameters and trims
// whitespace, so "17 " and 17 both mean line 17.
func TestStringArg(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"string", map[string]any{"k": "17"}, "17"},
		{"string trimmed", map[string]any{"k": " 17\t"}, "17"},
		{"whitespace only", map[string]any{"k": "   "}, ""},
		{"float64 integer", map[string]any{"k": float64(17)}, "17"},
		{"float64 large integer", map[string]any{"k": float64(30006018)}, "30006018"},
		{"int", map[string]any{"k": 17}, "17"},
		{"missing", map[string]any{}, ""},
		{"bool ignored", map[string]any{"k": true}, ""},
		{"nil ignored", map[string]any{"k": nil}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = c.args
			if got := stringArg(req, "k"); got != c.want {
				t.Errorf("stringArg(%v) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}

func runLeanArgs(t *testing.T, fixture string, args map[string]any) (LeanResponse, string) {
	t.Helper()
	mockHTTP := newMockDoer(loadTestData(t, fixture))
	_, handler := DeparturesTool(mockHTTP, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].(mcp.TextContent).Text
	if result.IsError {
		t.Fatalf("tool error: %s", text)
	}
	var parsed LeanResponse
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return parsed, mockHTTP.lastReq.URL.String()
}

func assertOnlyLine17Served(t *testing.T, parsed LeanResponse, label string) {
	t.Helper()
	if len(parsed.Stops) == 0 {
		t.Fatalf("%s: expected stops", label)
	}
	for _, s := range parsed.Stops {
		if s.LineServedHere == nil || !*s.LineServedHere {
			t.Errorf("%s: stop %s: expected line_served_here=true, got %v", label, s.TPCCode, s.LineServedHere)
		}
		if len(s.Departures) == 0 {
			t.Errorf("%s: stop %s: expected line 17 departures", label, s.TPCCode)
		}
		for _, d := range s.Departures {
			if d.Line != "17" {
				t.Errorf("%s: stop %s: filter leaked line %q", label, s.TPCCode, d.Line)
			}
		}
	}
}

func TestDeparturesTool_LineFilter_AcceptsNumber(t *testing.T) {
	defer fixedTime(t)()
	parsed, _ := runLeanArgs(t, "tpc_live.json", map[string]any{
		"tpc_code": "30006018",
		"line":     float64(17),
	})
	assertOnlyLine17Served(t, parsed, "line:17")
}

// A stray space must not turn into a positive claim that the line skips the
// stop: line_served_here would otherwise be false for a stop line 17 serves.
func TestDeparturesTool_LineFilter_TrimsWhitespace(t *testing.T) {
	defer fixedTime(t)()
	for _, line := range []string{"17 ", " 17", "\t17"} {
		parsed, _ := runLeanArgs(t, "tpc_live.json", map[string]any{
			"tpc_code": "30006018",
			"line":     line,
		})
		assertOnlyLine17Served(t, parsed, "line:"+line)
	}
}

// A whitespace-only line filter means "no filter", so line_served_here is
// omitted and nothing is filtered.
func TestDeparturesTool_LineFilter_WhitespaceOnlyIsNoFilter(t *testing.T) {
	defer fixedTime(t)()
	parsed, _ := runLeanArgs(t, "tpc_live.json", map[string]any{
		"tpc_code": "30006018",
		"line":     "  ",
	})
	for _, s := range parsed.Stops {
		if s.LineServedHere != nil {
			t.Errorf("stop %s: expected line_served_here omitted, got %v", s.TPCCode, *s.LineServedHere)
		}
	}
}

func TestDeparturesTool_TPCCode_AcceptsNumber(t *testing.T) {
	defer fixedTime(t)()
	_, url := runLeanArgs(t, "tpc_live.json", map[string]any{
		"tpc_code": float64(30006018),
	})
	if !strings.HasSuffix(url, "/tpc/30006018") {
		t.Errorf("expected upstream URL for tpc 30006018, got %s", url)
	}
}

func TestDeparturesTool_Direction_TrimsWhitespace(t *testing.T) {
	defer fixedTime(t)()
	trimmed, _ := runLeanArgs(t, "tpc_live.json", map[string]any{
		"tpc_code": "30006018", "direction": "centraal",
	})
	padded, _ := runLeanArgs(t, "tpc_live.json", map[string]any{
		"tpc_code": "30006018", "direction": " centraal ",
	})
	if countDepartures(trimmed) == 0 {
		t.Fatal("fixture should have departures towards Centraal")
	}
	if countDepartures(padded) != countDepartures(trimmed) {
		t.Errorf("padded direction filter returned %d departures, trimmed returned %d",
			countDepartures(padded), countDepartures(trimmed))
	}
}

func countDepartures(r LeanResponse) int {
	n := 0
	for _, s := range r.Stops {
		n += len(s.Departures)
	}
	return n
}

func linesTotal(t *testing.T, args map[string]any) int {
	t.Helper()
	_, handler := LinesTool(newMockDoer(loadTestData(t, "lines_live.json")))
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp LeanLinesIndexResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return resp.Total
}

func TestLinesTool_PublicNumber_AcceptsNumber(t *testing.T) {
	asString := linesTotal(t, map[string]any{"public_number": "1"})
	asNumber := linesTotal(t, map[string]any{"public_number": float64(1)})
	if asString == 0 {
		t.Fatal("fixture should contain public_number 1")
	}
	if asNumber != asString {
		t.Errorf("public_number:1 matched %d lines, public_number:\"1\" matched %d", asNumber, asString)
	}
}

func TestLinesTool_LineID_IsTrimmed(t *testing.T) {
	mock := newMockDoer(loadTestData(t, "line_detail_live.json"))
	_, handler := LinesTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"line_id": " GVB_17_1 "}
	if _, err := handler(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(mock.lastReq.URL.String(), "/line/GVB_17_1") {
		t.Errorf("line_id should be trimmed, got %s", mock.lastReq.URL.String())
	}
}
