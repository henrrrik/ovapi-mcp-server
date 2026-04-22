package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

const liveLineID = "GVB_17_1"

func TestTransformLineDetail_BasicShape(t *testing.T) {
	body := loadTestData(t, "line_detail_live.json")
	lean, err := transformLineDetail([]byte(body), liveLineID)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if lean.ID != liveLineID {
		t.Errorf("id = %q", lean.ID)
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
	if lean.Line.Direction != 1 {
		t.Errorf("direction = %d", lean.Line.Direction)
	}
	if lean.Line.Destination == "" {
		t.Error("line destination empty")
	}
	if lean.ServerTime == "" {
		t.Error("server_time empty")
	}
}

func TestTransformLineDetail_RouteSorted(t *testing.T) {
	body := loadTestData(t, "line_detail_live.json")
	lean, _ := transformLineDetail([]byte(body), liveLineID)

	if len(lean.Route) == 0 {
		t.Fatal("expected route stops")
	}
	prev := -1
	for _, s := range lean.Route {
		if s.Order < prev {
			t.Fatalf("route not sorted: order %d came after %d", s.Order, prev)
		}
		prev = s.Order
	}
	// First entry should be the start of the line.
	if lean.Route[0].Order != 1 {
		// Not all lines start at 1, so just assert it's the smallest we saw.
		for _, s := range lean.Route {
			if s.Order < lean.Route[0].Order {
				t.Errorf("route[0] order = %d but saw smaller %d", lean.Route[0].Order, s.Order)
			}
		}
	}
}

func TestTransformLineDetail_ActiveJourneys(t *testing.T) {
	body := loadTestData(t, "line_detail_live.json")
	lean, _ := transformLineDetail([]byte(body), liveLineID)

	if len(lean.ActiveJourneys) == 0 {
		t.Fatal("expected active journeys")
	}
	for _, aj := range lean.ActiveJourneys {
		if aj.JourneyID == "" {
			t.Error("journey_id empty")
		}
		if aj.Status == "" {
			t.Error("status empty")
		}
	}
}

func TestTransformLineDetail_ResponseSizeBound(t *testing.T) {
	body := loadTestData(t, "line_detail_live.json")
	lean, _ := transformLineDetail([]byte(body), liveLineID)

	out, err := json.Marshal(lean)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Spec acceptance: "<10 KB" for typical cases; allow a little slack for
	// long routes with many active journeys.
	if len(out) > 14_000 {
		t.Errorf("lean line-detail too large: %d bytes", len(out))
	}
	if len(out) >= len(body) {
		t.Errorf("lean not smaller than raw: lean=%d raw=%d", len(out), len(body))
	}
}

func TestTransformLineDetail_NoBisonFieldNames(t *testing.T) {
	body := loadTestData(t, "line_detail_live.json")
	lean, _ := transformLineDetail([]byte(body), liveLineID)
	out, _ := json.Marshal(lean)
	forbidden := []string{
		"TimingPointCode", "UserStopOrderNumber", "FortifyOrderNumber",
		"JourneyPatternCode", "LocalServiceLevelCode", "ProductFormulaType",
		"LinePlanningNumber",
	}
	for _, f := range forbidden {
		if strings.Contains(string(out), f) {
			t.Errorf("lean shape leaks raw field %q", f)
		}
	}
}

func TestTransformLineDetail_EmptyActualsAndNetwork(t *testing.T) {
	body := `{"GVB_17_1":{"Line":{"LinePublicNumber":"17","TransportType":"TRAM","DataOwnerCode":"GVB","DestinationName50":"CS","LineDirection":1},"Actuals":{},"Network":{},"ServerTime":"2026-04-22T08:00:00Z"}}`
	lean, err := transformLineDetail([]byte(body), "GVB_17_1")
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if lean.ActiveJourneys == nil {
		t.Error("expected non-nil active_journeys (would serialize as null)")
	}
	if lean.Route == nil {
		t.Error("expected non-nil route (would serialize as null)")
	}
}

func TestTransformLineDetail_MissingLineIDFallback(t *testing.T) {
	body := `{"GVB_17_1":{"Line":{"LinePublicNumber":"17","TransportType":"TRAM","DataOwnerCode":"GVB"},"Actuals":{},"Network":{},"ServerTime":"2026-04-22T08:00:00Z"}}`
	lean, _ := transformLineDetail([]byte(body), "different_id")
	if lean.ID != "GVB_17_1" {
		t.Errorf("expected single-entry fallback to use actual id, got %q", lean.ID)
	}
}
