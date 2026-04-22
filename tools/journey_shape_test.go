package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

const liveJourneyID = "GVB_20260422_17_19206_0"

func TestTransformJourney_BasicShape(t *testing.T) {
	body := loadTestData(t, "journey_live.json")
	lean, err := transformJourney([]byte(body), liveJourneyID)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}

	if lean.JourneyID != liveJourneyID {
		t.Errorf("journey_id = %q, want %q", lean.JourneyID, liveJourneyID)
	}
	if lean.Line.PublicNumber != "17" {
		t.Errorf("line public_number = %q", lean.Line.PublicNumber)
	}
	if lean.Line.Mode != "tram" {
		t.Errorf("line mode = %q", lean.Line.Mode)
	}
	if lean.Line.Owner != "GVB" {
		t.Errorf("line owner = %q", lean.Line.Owner)
	}
	if lean.Destination == "" {
		t.Error("destination empty")
	}
	if lean.ServerTime == "" {
		t.Error("server_time empty")
	}
	if len(lean.Stops) == 0 {
		t.Fatal("expected stops")
	}
}

func TestTransformJourney_StopsSortedByOrder(t *testing.T) {
	body := loadTestData(t, "journey_live.json")
	lean, _ := transformJourney([]byte(body), liveJourneyID)

	prev := -1
	for _, s := range lean.Stops {
		if s.Order < prev {
			t.Fatalf("stops not sorted: order %d came after %d", s.Order, prev)
		}
		prev = s.Order
	}
}

func TestTransformJourney_StopFields(t *testing.T) {
	body := loadTestData(t, "journey_live.json")
	lean, _ := transformJourney([]byte(body), liveJourneyID)

	// Every stop should have name, tpc_code, and a non-zero order.
	for _, s := range lean.Stops {
		if s.Name == "" {
			t.Errorf("stop %d missing name", s.Order)
		}
		if s.TPCCode == "" {
			t.Errorf("stop %d missing tpc_code", s.Order)
		}
		if s.Order == 0 {
			t.Errorf("stop %q has zero order", s.Name)
		}
	}
}

func TestTransformJourney_ResponseSizeBound(t *testing.T) {
	body := loadTestData(t, "journey_live.json")
	lean, _ := transformJourney([]byte(body), liveJourneyID)

	out, err := json.Marshal(lean)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Spec acceptance: "<8 KB for typical cases". Allow a bit of slack for an
	// end-of-line route with many stops.
	if len(out) > 12_000 {
		t.Errorf("lean journey too large: %d bytes", len(out))
	}
	if len(out) >= len(body) {
		t.Errorf("lean not smaller than raw: lean=%d raw=%d", len(out), len(body))
	}
}

func TestTransformJourney_NoBisonFieldNames(t *testing.T) {
	body := loadTestData(t, "journey_live.json")
	lean, _ := transformJourney([]byte(body), liveJourneyID)
	out, _ := json.Marshal(lean)

	// Ensure raw BISON/KV1 field names are not leaking into the lean shape.
	forbidden := []string{
		"TimingPointCode", "UserStopOrderNumber", "FortifyOrderNumber",
		"JourneyPatternCode", "LocalServiceLevelCode", "ProductFormulaType",
		"TimingPointDataOwnerCode", "LinePlanningNumber",
	}
	s := string(out)
	for _, f := range forbidden {
		if strings.Contains(s, f) {
			t.Errorf("lean shape leaks raw field %q", f)
		}
	}
}

func TestTransformJourney_NullCoordForSentinel(t *testing.T) {
	body := `{"J1":{"ServerTime":"2026-04-22T08:00:00Z","Stops":{"1":{"UserStopOrderNumber":1,"TimingPointName":"Nowhere","TimingPointCode":"X","Latitude":47.974766,"Longitude":3.3135424}}}}`
	lean, err := transformJourney([]byte(body), "J1")
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if len(lean.Stops) != 1 {
		t.Fatalf("expected 1 stop, got %d", len(lean.Stops))
	}
	if lean.Stops[0].Coord != nil {
		t.Errorf("expected sentinel coord to be null, got %v", lean.Stops[0].Coord)
	}
}

func TestTransformJourney_InferTownFromNamePrefix(t *testing.T) {
	body := `{"J1":{"ServerTime":"2026-04-22T08:00:00Z","Stops":{"1":{"UserStopOrderNumber":1,"TimingPointName":"Amsterdam, Centraal Station","TimingPointCode":"X","TimingPointTown":"unknown","Latitude":52.1,"Longitude":4.9}}}}`
	lean, _ := transformJourney([]byte(body), "J1")
	if lean.Stops[0].Town != "Amsterdam" {
		t.Errorf("expected inferred town 'Amsterdam', got %q", lean.Stops[0].Town)
	}
}

func TestTransformJourney_DropsUnknownAccessibility(t *testing.T) {
	body := `{"J1":{"ServerTime":"","Stops":{"1":{"UserStopOrderNumber":1,"TimingPointName":"A","TimingPointCode":"X","WheelChairAccessible":"UNKNOWN"}}}}`
	lean, _ := transformJourney([]byte(body), "J1")
	if lean.Stops[0].WheelchairAccessible != "" {
		t.Errorf("expected UNKNOWN to be dropped, got %q", lean.Stops[0].WheelchairAccessible)
	}
}

func TestTransformJourney_KeepsAccessibleValue(t *testing.T) {
	body := `{"J1":{"ServerTime":"","Stops":{"1":{"UserStopOrderNumber":1,"TimingPointName":"A","TimingPointCode":"X","WheelChairAccessible":"ACCESSIBLE"}}}}`
	lean, _ := transformJourney([]byte(body), "J1")
	if lean.Stops[0].WheelchairAccessible != "ACCESSIBLE" {
		t.Errorf("expected ACCESSIBLE, got %q", lean.Stops[0].WheelchairAccessible)
	}
}

func TestTransformJourney_FallbackToSingleEntry(t *testing.T) {
	// Simulate upstream returning the journey under a slightly different key.
	body := `{"other_id":{"ServerTime":"2026-04-22T08:00:00Z","Stops":{"1":{"UserStopOrderNumber":1,"TimingPointName":"X","TimingPointCode":"1"}}}}`
	lean, _ := transformJourney([]byte(body), "requested_id")
	if lean.JourneyID != "other_id" {
		t.Errorf("expected fallback to single entry, got %q", lean.JourneyID)
	}
}

func TestTransformJourney_MissingEntry_ReturnsEmptyStops(t *testing.T) {
	body := `{"a":{"Stops":{}}, "b":{"Stops":{}}}`
	lean, _ := transformJourney([]byte(body), "not_present")
	if lean.JourneyID != "not_present" {
		t.Errorf("journey_id = %q", lean.JourneyID)
	}
	if lean.Stops == nil {
		t.Error("expected empty slice, got nil — would serialize as null")
	}
}
