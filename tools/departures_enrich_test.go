package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// runLeanFromFixture is like runLean but parameterized on the fixture file.
func runLeanFromFixture(t *testing.T, fixture string, args map[string]any) LeanResponse {
	t.Helper()
	body := loadTestData(t, fixture)
	mockHTTP := newMockDoer(body)

	_, handler := DeparturesTool(mockHTTP, nil)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("error result: %s", result.Content[0].(mcp.TextContent).Text)
	}
	text := result.Content[0].(mcp.TextContent).Text
	var parsed LeanResponse
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("response is not valid LeanResponse JSON: %v", err)
	}
	return parsed
}

func TestDeparturesLean_Platform_PresentOnSchipholStop(t *testing.T) {
	// Pin time so countdown math doesn't affect whether platform is surfaced.
	loc, _ := time.LoadLocation("Europe/Amsterdam")
	now := time.Date(2026, 4, 22, 8, 0, 0, 0, loc)
	prev := timeNow
	timeNow = func() time.Time { return now }
	defer func() { timeNow = prev }()

	resp := runLeanFromFixture(t, "tpc_schiphol.json", map[string]any{
		"tpc_code": "57330760",
	})
	if len(resp.Stops) == 0 {
		t.Fatal("expected stops")
	}

	foundPlatform := false
	for _, s := range resp.Stops {
		for _, d := range s.Departures {
			if d.Platform != nil && *d.Platform != "" {
				foundPlatform = true
				break
			}
		}
	}
	if !foundPlatform {
		t.Error("expected at least one departure with a non-nil platform at Schiphol")
	}
}

func TestDeparturesLean_NumberOfCoaches_NullWhenZero(t *testing.T) {
	defer fixedTime(t)()
	resp := runLeanFromFixture(t, "tpc_live.json", map[string]any{
		"tpc_code": "30006018",
	})
	// Fixture has NumberOfCoaches=0 on every pass, which should come out as null.
	for _, s := range resp.Stops {
		for _, d := range s.Departures {
			if d.NumberOfCoaches != nil {
				t.Errorf("expected null number_of_coaches for zero, got %v", *d.NumberOfCoaches)
			}
		}
	}
}

func TestDeparturesLean_WheelchairAccessible_DropsUnknown(t *testing.T) {
	defer fixedTime(t)()
	resp := runLeanFromFixture(t, "tpc_live.json", map[string]any{
		"tpc_code": "30006018",
	})
	for _, s := range resp.Stops {
		for _, d := range s.Departures {
			if d.WheelchairAccessible != nil {
				t.Errorf("expected null wheelchair_accessible for UNKNOWN, got %v", *d.WheelchairAccessible)
			}
		}
	}
}

func TestDeparturesLean_Display_NearCountdown(t *testing.T) {
	defer fixedTime(t)()
	resp := runLeanFromFixture(t, "tpc_live.json", map[string]any{
		"tpc_code": "30006018",
	})
	// fixedTime pins to 23:45 Amsterdam. One pass is planned 23:48:29,
	// expected 23:48:29 → ~3 min → "3 min".
	foundMin := false
	for _, s := range resp.Stops {
		for _, d := range s.Departures {
			if d.Display == "3 min" || d.Display == "4 min" {
				foundMin = true
				break
			}
		}
	}
	if !foundMin {
		t.Error("expected at least one departure with a short-minute display")
	}
}

func TestDeparturesLean_Display_FarShowsHHMM(t *testing.T) {
	defer fixedTime(t)()
	resp := runLeanFromFixture(t, "tpc_live.json", map[string]any{
		"tpc_code": "30006014",
	})
	// Fixture has passes 30+ min out (e.g. 00:51:46). Should render as HH:MM.
	foundHHMM := false
	for _, s := range resp.Stops {
		for _, d := range s.Departures {
			if len(d.Display) == 5 && d.Display[2] == ':' {
				foundHHMM = true
				break
			}
		}
	}
	if !foundHHMM {
		t.Error("expected at least one departure rendered as HH:MM")
	}
}

func TestDeparturesLean_SentinelCoord_YieldsNull(t *testing.T) {
	defer fixedTime(t)()
	// Minimal upstream-shaped fixture with sentinel coord.
	body := `{"X":{"Stop":{"TimingPointCode":"X","TimingPointName":"Nowhere","TimingPointTown":"unknown","Latitude":47.974766,"Longitude":3.3135424,"StopAreaCode":null},"Passes":{},"GeneralMessages":{}}}`
	mockHTTP := newMockDoer(body)

	_, handler := DeparturesTool(mockHTTP, nil)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"tpc_code": "X"}
	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, `"coord":null`) {
		t.Errorf("expected coord:null for sentinel, got: %s", text)
	}
}

func TestDeparturesLean_TownInferredFromNamePrefix(t *testing.T) {
	defer fixedTime(t)()
	body := `{"X":{"Stop":{"TimingPointCode":"X","TimingPointName":"Amsterdam, Centraal Station","TimingPointTown":"unknown","Latitude":52.1,"Longitude":4.9,"StopAreaCode":null},"Passes":{},"GeneralMessages":{}}}`
	mockHTTP := newMockDoer(body)

	_, handler := DeparturesTool(mockHTTP, nil)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"tpc_code": "X"}
	result, _ := handler(context.Background(), req)
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, `"town":"Amsterdam"`) {
		t.Errorf("expected town inferred from stop name prefix, got: %s", text)
	}
}
