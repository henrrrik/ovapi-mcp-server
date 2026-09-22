package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// Upstream keys passes and line actuals by DataOwnerCode
// ("NL_20260922_7_10926_0") but /journey/{id} only resolves ids prefixed
// with OperatorCode ("GVB_20260922_7_10926_0"). Verified live for GVB and
// CXX: the owner-prefixed id 404s, the operator-prefixed one returns the run.
func TestJourneyIDForOperator(t *testing.T) {
	cases := []struct {
		name, id, owner, operator, want string
	}{
		{"owner differs from operator", "NL_20260922_7_10926_0", "NL", "GVB", "GVB_20260922_7_10926_0"},
		{"connexxion", "NL_20260422_M300_159_0", "NL", "CXX", "CXX_20260422_M300_159_0"},
		{"owner equals operator", "GVB_20260421_17_19185_0", "GVB", "GVB", "GVB_20260421_17_19185_0"},
		{"no operator code", "NL_20260922_7_10926_0", "NL", "", "NL_20260922_7_10926_0"},
		{"no owner code", "NL_20260922_7_10926_0", "", "GVB", "NL_20260922_7_10926_0"},
		{"id not prefixed by owner", "X_20260922_7_10926_0", "NL", "GVB", "X_20260922_7_10926_0"},
		{"owner is a prefix of a longer token", "NLX_1", "NL", "GVB", "NLX_1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := journeyIDForOperator(c.id, c.owner, c.operator); got != c.want {
				t.Errorf("journeyIDForOperator(%q, %q, %q) = %q, want %q", c.id, c.owner, c.operator, got, c.want)
			}
		})
	}
}

// The Schiphol fixture is Connexxion data owned by "NL": every pass is keyed
// NL_... with OperatorCode CXX.
func TestDeparturesTool_JourneyID_UsesOperatorCode(t *testing.T) {
	mockHTTP := newMockDoer(loadTestData(t, "tpc_schiphol.json"))
	_, handler := DeparturesTool(mockHTTP, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"tpc_code": "57330760"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var parsed LeanResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Stops) != 1 || len(parsed.Stops[0].Departures) == 0 {
		t.Fatalf("expected one stop with departures, got %+v", parsed.Stops)
	}
	found := false
	for _, d := range parsed.Stops[0].Departures {
		if strings.HasPrefix(d.JourneyID, "NL_") {
			t.Errorf("journey_id %q still carries the DataOwnerCode prefix", d.JourneyID)
		}
		if d.JourneyID == "CXX_20260422_M300_159_0" {
			found = true
		}
	}
	if !found {
		t.Error("expected journey_id CXX_20260422_M300_159_0 (fixture key NL_20260422_M300_159_0 re-prefixed with OperatorCode)")
	}
}

func TestTransformLineDetail_ActiveJourneys_UseOperatorCode(t *testing.T) {
	body := `{"GVB_17_1":{"Line":{"LinePublicNumber":"17","DataOwnerCode":"NL","TransportType":"TRAM","LineDirection":1},
		"Actuals":{"NL_20260922_17_30870_0":{"DataOwnerCode":"NL","OperatorCode":"GVB","TimingPointName":"Centraal Station","UserStopOrderNumber":3,"TripStopStatus":"DRIVING"}},
		"Network":{}}}`
	lean, err := transformLineDetail([]byte(body), "GVB_17_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(lean.ActiveJourneys) != 1 {
		t.Fatalf("expected 1 active journey, got %d", len(lean.ActiveJourneys))
	}
	if got := lean.ActiveJourneys[0].JourneyID; got != "GVB_20260922_17_30870_0" {
		t.Errorf("active journey_id = %q, want GVB_20260922_17_30870_0", got)
	}
}

// A stale or unknown journey id is a 404 upstream, which surfaces as a tool
// error; the description must not promise an empty stops list.
func TestToolDescription_Journey_StaleIDIsAnError(t *testing.T) {
	tool, _ := JourneyTool(newMockDoer("{}"))
	if strings.Contains(tool.Description, "empty stops list rather than an error") {
		t.Errorf("journey description still claims stale ids return an empty list\nhave: %s", tool.Description)
	}
	if !strings.Contains(tool.Description, "404") {
		t.Errorf("journey description should say stale ids produce an HTTP 404 error\nhave: %s", tool.Description)
	}
}

func TestJourneyTool_NotFound_IsToolError(t *testing.T) {
	_, handler := JourneyTool(newMockDoerWithStatus("{}", 404))
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"journey_id": "GVB_20260421_17_19185_0"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected a tool error for an upstream 404")
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "404") {
		t.Errorf("error should mention the 404, got %q", text)
	}
}
