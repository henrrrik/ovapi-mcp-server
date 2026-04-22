package tools

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/henrrrik/ovapi-mcp-server/db"
)

func runFindNear(t *testing.T, mock *mockStopSearcher, args map[string]any) NearStopsResponse {
	t.Helper()
	_, handler := FindStopsNearTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(mcp.TextContent).Text)
	}
	var resp NearStopsResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return resp
}

// TestFindStopsNear_MatchesHaversineForKnownPair validates the distance
// calculation against a known pair (Nicolaas Beetsstraat 30006018 and
// 30006014, which sit ~25 m apart across the street).
func TestFindStopsNear_MatchesHaversineForKnownPair(t *testing.T) {
	// 30006018 coords; we use it as the reference point.
	lat, lng := 52.365417, 4.8653097
	mock := &mockStopSearcher{
		bboxResults: []db.Stop{
			{TPCCode: "30006018", Name: "Nicolaas Beetsstraat", Latitude: 52.365417, Longitude: 4.8653097},
			{TPCCode: "30006014", Name: "Nicolaas Beetsstraat", Latitude: 52.365543, Longitude: 4.8656464},
		},
	}

	resp := runFindNear(t, mock, map[string]any{
		"lat":      lat,
		"lng":      lng,
		"radius_m": float64(100),
	})
	if len(resp.Stops) != 2 {
		t.Fatalf("expected 2 stops, got %d", len(resp.Stops))
	}
	if resp.Stops[0].TPCCode != "30006018" {
		t.Errorf("nearest should be self, got %q", resp.Stops[0].TPCCode)
	}
	if resp.Stops[0].DistanceM != 0 {
		t.Errorf("distance to self should be 0, got %d", resp.Stops[0].DistanceM)
	}
	// Hand-computed haversine: ~27 m (allow +/-3).
	d := resp.Stops[1].DistanceM
	if d < 20 || d > 35 {
		t.Errorf("expected ~25-30m between pair, got %d", d)
	}
}

func TestFindStopsNear_SortsByDistance(t *testing.T) {
	lat, lng := 52.0, 4.0
	// Three stops at increasing distance.
	mock := &mockStopSearcher{
		bboxResults: []db.Stop{
			{TPCCode: "C", Name: "Far", Latitude: 52.003, Longitude: 4.003},
			{TPCCode: "A", Name: "Near", Latitude: 52.0005, Longitude: 4.0005},
			{TPCCode: "B", Name: "Mid", Latitude: 52.001, Longitude: 4.001},
		},
	}
	resp := runFindNear(t, mock, map[string]any{
		"lat": lat, "lng": lng, "radius_m": float64(1000),
	})
	if len(resp.Stops) != 3 {
		t.Fatalf("expected 3 stops")
	}
	want := []string{"A", "B", "C"}
	for i, s := range resp.Stops {
		if s.TPCCode != want[i] {
			t.Errorf("position %d: got %q want %q", i, s.TPCCode, want[i])
		}
	}
}

func TestFindStopsNear_ExcludesSentinelCoord(t *testing.T) {
	mock := &mockStopSearcher{
		bboxResults: []db.Stop{
			{TPCCode: "GOOD", Name: "Real", Latitude: 52.0, Longitude: 4.0},
			{TPCCode: "SENT", Name: "Ghost", Latitude: 47.974766, Longitude: 3.3135424},
		},
	}
	resp := runFindNear(t, mock, map[string]any{
		"lat": 52.0, "lng": 4.0, "radius_m": float64(100),
	})
	if len(resp.Stops) != 1 {
		t.Fatalf("expected 1 stop (sentinel excluded), got %d", len(resp.Stops))
	}
	if resp.Stops[0].TPCCode != "GOOD" {
		t.Errorf("expected GOOD, got %q", resp.Stops[0].TPCCode)
	}
}

func TestFindStopsNear_RespectsRadius(t *testing.T) {
	mock := &mockStopSearcher{
		bboxResults: []db.Stop{
			{TPCCode: "NEAR", Name: "Near", Latitude: 52.001, Longitude: 4.0}, // ~111m
			{TPCCode: "FAR", Name: "Far", Latitude: 52.01, Longitude: 4.0},    // ~1110m
		},
	}
	resp := runFindNear(t, mock, map[string]any{
		"lat": 52.0, "lng": 4.0, "radius_m": float64(300),
	})
	if len(resp.Stops) != 1 {
		t.Fatalf("expected 1 stop inside 300m, got %d", len(resp.Stops))
	}
	if resp.Stops[0].TPCCode != "NEAR" {
		t.Errorf("expected NEAR, got %q", resp.Stops[0].TPCCode)
	}
}

func TestFindStopsNear_LimitRespected(t *testing.T) {
	stops := make([]db.Stop, 30)
	for i := 0; i < 30; i++ {
		// Spread across 30m.
		stops[i] = db.Stop{
			TPCCode: "X", Name: "X",
			Latitude:  52.0 + float64(i)*0.00001,
			Longitude: 4.0,
		}
	}
	mock := &mockStopSearcher{bboxResults: stops}
	resp := runFindNear(t, mock, map[string]any{
		"lat": 52.0, "lng": 4.0, "radius_m": float64(1000),
		"limit": float64(5),
	})
	if len(resp.Stops) != 5 {
		t.Errorf("expected 5 stops (limit), got %d", len(resp.Stops))
	}
}

func TestFindStopsNear_DefaultsApplied(t *testing.T) {
	mock := &mockStopSearcher{bboxResults: []db.Stop{}}
	runFindNear(t, mock, map[string]any{
		"lat": 52.0, "lng": 4.0,
	})
	// Default radius 500m → bbox should be ~0.0045° lat, ~0.0073° lng at 52°N.
	minLat, maxLat, _, _ := mock.lastBBox[0], mock.lastBBox[1], mock.lastBBox[2], mock.lastBBox[3]
	latSpan := maxLat - minLat
	if math.Abs(latSpan-2*500.0/111_000.0) > 1e-6 {
		t.Errorf("bbox lat span = %v, want ~%v", latSpan, 2*500.0/111_000.0)
	}
}

func TestFindStopsNear_ClampsRadiusAndLimit(t *testing.T) {
	mock := &mockStopSearcher{bboxResults: []db.Stop{}}
	runFindNear(t, mock, map[string]any{
		"lat": 52.0, "lng": 4.0,
		"radius_m": float64(999_999),
		"limit":    float64(999),
	})
	// bbox lat span should reflect clamped 5000m radius.
	latSpan := mock.lastBBox[1] - mock.lastBBox[0]
	want := 2 * float64(maxRadius) / 111_000.0
	if math.Abs(latSpan-want) > 1e-6 {
		t.Errorf("expected lat span = %v for clamped max radius, got %v", want, latSpan)
	}
}

func TestFindStopsNear_MissingLatLng(t *testing.T) {
	mock := &mockStopSearcher{}
	_, handler := FindStopsNearTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"lat": float64(52.0)}
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Error("expected error when lng missing")
	}
}

func TestFindStopsNear_SurfacesBBoxError(t *testing.T) {
	mock := &mockStopSearcher{bboxErr: errors.New("db kaboom")}
	_, handler := FindStopsNearTool(mock)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"lat": float64(52.0), "lng": float64(4.0),
	}
	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Fatal("expected error result")
	}
}

func TestFindStopsNear_IncludesPairedWith(t *testing.T) {
	mock := &mockStopSearcher{
		bboxResults: []db.Stop{
			{TPCCode: "30006018", Name: "Nicolaas Beetsstraat", Latitude: 52.365417, Longitude: 4.8653097},
		},
		pairs: map[string][]string{"30006018": {"30006014"}},
	}
	resp := runFindNear(t, mock, map[string]any{
		"lat": 52.365417, "lng": 4.8653097, "radius_m": float64(100),
	})
	if len(resp.Stops) != 1 {
		t.Fatalf("expected 1 stop, got %d", len(resp.Stops))
	}
	if len(resp.Stops[0].PairedWith) != 1 || resp.Stops[0].PairedWith[0] != "30006014" {
		t.Errorf("expected paired_with=[30006014], got %v", resp.Stops[0].PairedWith)
	}
}
