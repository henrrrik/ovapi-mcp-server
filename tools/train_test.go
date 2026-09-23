package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/henrrrik/ovapi-mcp-server/nsclient"
)

// nsRoutingDoer serves the recorded NS fixtures by upstream path and keeps
// the last request so tests can assert on the query the tool built.
type nsRoutingDoer struct {
	t       *testing.T
	lastReq *http.Request
}

func (d *nsRoutingDoer) Do(req *http.Request) (*http.Response, error) {
	d.lastReq = req
	var body string
	switch {
	case strings.HasSuffix(req.URL.Path, "/api/v2/stations"):
		body = loadTestData(d.t, "ns_stations.json")
	case strings.HasSuffix(req.URL.Path, "/api/v2/departures"):
		body = loadTestData(d.t, "ns_departures_ut.json")
	case strings.HasSuffix(req.URL.Path, "/api/v3/trips") && req.URL.Query().Get("fromStation") == "ZP":
		body = loadTestData(d.t, "ns_trips_zp_hgl.json")
	case strings.HasSuffix(req.URL.Path, "/api/v3/trips"):
		body = loadTestData(d.t, "ns_trips_asd_ut.json")
	case strings.Contains(req.URL.Path, "/api/v3/disruptions"):
		body = loadTestData(d.t, "ns_disruptions.json")
	default:
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{"detail":"no fixture for ` + req.URL.Path + `"}`)), Header: http.Header{}}, nil
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
}

func newTrains(t *testing.T) (*nsclient.Trains, *nsRoutingDoer) {
	t.Helper()
	doer := &nsRoutingDoer{t: t}
	return nsclient.NewTrains("test-key", doer), doer
}

func callTrainTool(t *testing.T, tool mcp.Tool, handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) (string, bool) {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Name = tool.Name
	req.Params.Arguments = args
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", tool.Name, err)
	}
	return result.Content[0].(mcp.TextContent).Text, result.IsError
}

func decodeInto(t *testing.T, text string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(text), v); err != nil {
		t.Fatalf("parse: %v\nbody: %s", err, text)
	}
}

func lastQuery(d *nsRoutingDoer) string {
	return d.lastReq.URL.Path + "?" + d.lastReq.URL.RawQuery
}

// ---- train_departures ----

func TestTrainDepartures_LeanShape(t *testing.T) {
	trains, doer := newTrains(t)
	tool, handler := TrainDeparturesTool(trains)
	text, isErr := callTrainTool(t, tool, handler, map[string]any{"station": "Utrecht Centraal"})
	if isErr {
		t.Fatalf("tool error: %s", text)
	}
	if q := lastQuery(doer); !strings.Contains(q, "/api/v2/departures?") || !strings.Contains(q, "station=UT") {
		t.Errorf("expected departures for station code UT, got %s", q)
	}
	var out TrainDeparturesResponse
	decodeInto(t, text, &out)
	if out.Station.Code != "UT" || out.Station.Name != "Utrecht Centraal" {
		t.Errorf("station = %+v", out.Station)
	}
	if out.DisruptionCount != 2 {
		t.Errorf("disruption_count = %d, want 2 (meta.numberOfDisruptions)", out.DisruptionCount)
	}
	if len(out.Departures) != 10 {
		t.Fatalf("expected 10 departures, got %d", len(out.Departures))
	}
	d := out.Departures[0]
	want := TrainDeparture{
		Train: "IC 583", Category: "IC", CategoryName: "Intercity", Number: "583", Operator: "NS",
		Direction: "Groningen", Planned: "2026-09-22T22:51:00+02:00", Expected: "2026-09-22T23:01:00+02:00",
		DelaySeconds: 600, PlannedTrack: "12", ActualTrack: "12", Status: "ON_STATION",
		Route: []string{"Amersfoort C.", "Zwolle", "Assen"}, Messages: []string{},
	}
	if d.Train != want.Train || d.Category != want.Category || d.CategoryName != want.CategoryName || d.Number != want.Number ||
		d.Operator != want.Operator || d.Direction != want.Direction || d.Planned != want.Planned || d.Expected != want.Expected ||
		d.DelaySeconds != want.DelaySeconds || d.PlannedTrack != want.PlannedTrack || d.ActualTrack != want.ActualTrack ||
		d.TrackChanged || d.Cancelled || d.Status != want.Status || strings.Join(d.Route, ",") != strings.Join(want.Route, ",") {
		t.Errorf("first departure:\n got  %+v\n want %+v", d, want)
	}
	for i := 1; i < len(out.Departures); i++ {
		if out.Departures[i-1].Planned > out.Departures[i].Planned {
			t.Errorf("departures not sorted by planned time at %d", i)
		}
	}
	changed := 0
	for _, d := range out.Departures {
		if d.TrackChanged {
			changed++
			if d.PlannedTrack == d.ActualTrack {
				t.Errorf("track_changed set although tracks match: %+v", d)
			}
		}
	}
	if changed != 1 {
		t.Errorf("expected exactly one track change in the fixture, got %d", changed)
	}
	if strings.Contains(text, "plannedDateTime") || strings.Contains(text, "routeStations") {
		t.Error("lean shape leaked upstream field names")
	}
}

func TestTrainDepartures_CodeAndOptions(t *testing.T) {
	trains, doer := newTrains(t)
	tool, handler := TrainDeparturesTool(trains)
	_, isErr := callTrainTool(t, tool, handler, map[string]any{
		"station": "ut", "max_journeys": float64(5), "date_time": "2026-09-23T08:00:00+02:00",
	})
	if isErr {
		t.Fatal("unexpected tool error")
	}
	q := lastQuery(doer)
	for _, want := range []string{"station=UT", "maxJourneys=5", "dateTime=2026-09-23T08%3A00%3A00%2B02%3A00"} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %s: %s", want, q)
		}
	}
}

func TestTrainDepartures_UnknownStation(t *testing.T) {
	trains, _ := newTrains(t)
	tool, handler := TrainDeparturesTool(trains)
	text, isErr := callTrainTool(t, tool, handler, map[string]any{"station": "Nowhere Special"})
	if !isErr {
		t.Fatalf("expected a tool error, got %s", text)
	}
	for _, want := range []string{"Nowhere Special", "train_stations"} {
		if !strings.Contains(text, want) {
			t.Errorf("error should mention %q, got %q", want, text)
		}
	}
}

func TestTrainDepartures_MissingStation(t *testing.T) {
	trains, _ := newTrains(t)
	tool, handler := TrainDeparturesTool(trains)
	if text, isErr := callTrainTool(t, tool, handler, map[string]any{}); !isErr || !strings.Contains(text, "station") {
		t.Errorf("expected 'station is required', got %q", text)
	}
}

func TestTrainDepartures_Verbose(t *testing.T) {
	trains, _ := newTrains(t)
	tool, handler := TrainDeparturesTool(trains)
	text, isErr := callTrainTool(t, tool, handler, map[string]any{"station": "UT", "verbose": true})
	if isErr || !strings.Contains(text, "plannedDateTime") {
		t.Errorf("verbose should pass the upstream body through, got %s", text[:80])
	}
}

// ---- train_trips ----

func TestTrainTrips_LeanShape(t *testing.T) {
	trains, doer := newTrains(t)
	tool, handler := TrainTripsTool(trains)
	text, isErr := callTrainTool(t, tool, handler, map[string]any{"from": "Amsterdam", "to": "Utrecht"})
	if isErr {
		t.Fatalf("tool error: %s", text)
	}
	if q := lastQuery(doer); !strings.Contains(q, "fromStation=ASD") || !strings.Contains(q, "toStation=UT") {
		t.Errorf("expected ASD -> UT, got %s", q)
	}
	var out TrainTripsResponse
	decodeInto(t, text, &out)
	if out.From.Code != "ASD" || out.To.Code != "UT" {
		t.Errorf("from/to = %+v / %+v", out.From, out.To)
	}
	if len(out.Trips) != 5 {
		t.Fatalf("expected 5 trips, got %d", len(out.Trips))
	}
	tr := out.Trips[0]
	if tr.PlannedDurationMinutes != 27 || tr.DurationMinutes != 26 || tr.Transfers != 0 || tr.Status != "NORMAL" ||
		tr.CrowdForecast != "LOW" || tr.PriceEUR != 10.00 || len(tr.Legs) != 1 {
		t.Errorf("trip[0] = %+v", tr)
	}
	if tr.Departure != "2026-09-22T22:55:00+02:00" || tr.PlannedDeparture != "2026-09-22T22:54:00+02:00" {
		t.Errorf("departure times = %s / %s", tr.Departure, tr.PlannedDeparture)
	}
	if tr.Arrival == "" || !strings.HasSuffix(tr.Arrival, "+02:00") {
		t.Errorf("arrival = %q", tr.Arrival)
	}
	leg := tr.Legs[0]
	if leg.Train != "IC 3085" || leg.Category != "IC" || leg.Operator != "NS" || leg.Direction == "" || leg.Stops != 3 || leg.Cancelled {
		t.Errorf("leg[0] = %+v", leg)
	}
	if leg.From.Code != "ASD" || leg.From.Name != "Amsterdam Centraal" || leg.From.Planned != "2026-09-22T22:54:00+02:00" ||
		leg.From.Actual != "2026-09-22T22:55:00+02:00" || leg.From.Track != "4b" {
		t.Errorf("leg[0].from = %+v", leg.From)
	}
	if leg.To.Code != "UT" || leg.To.Track == "" {
		t.Errorf("leg[0].to = %+v", leg.To)
	}
	if strings.Contains(text, "ctxRecon") || strings.Contains(text, "nesProperties") {
		t.Error("lean shape leaked upstream field names")
	}
}

func TestTrainTrips_ViaAndArrivalSearch(t *testing.T) {
	trains, doer := newTrains(t)
	tool, handler := TrainTripsTool(trains)
	_, isErr := callTrainTool(t, tool, handler, map[string]any{
		"from": "ASD", "to": "UT", "via": "Utrecht Overvecht", "date_time": "2026-09-23T09:00:00+02:00", "search_for_arrival": true,
	})
	if isErr {
		t.Fatal("unexpected tool error")
	}
	q := lastQuery(doer)
	for _, want := range []string{"viaStation=UTO", "searchForArrival=true", "dateTime=2026-09-23T09"} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %s: %s", want, q)
		}
	}
}

func TestTrainTrips_MissingEndpoints(t *testing.T) {
	trains, _ := newTrains(t)
	tool, handler := TrainTripsTool(trains)
	if text, isErr := callTrainTool(t, tool, handler, map[string]any{"from": "ASD"}); !isErr || !strings.Contains(text, "to") {
		t.Errorf("expected error about 'to', got %q", text)
	}
	if text, isErr := callTrainTool(t, tool, handler, map[string]any{"from": "Nowhere Special", "to": "UT"}); !isErr || !strings.Contains(text, "Nowhere Special") {
		t.Errorf("expected error naming the unknown station, got %q", text)
	}
}

func TestTrainTrips_DisruptionsAreSurfaced(t *testing.T) {
	trains, _ := newTrains(t)
	tool, handler := TrainTripsTool(trains)
	text, isErr := callTrainTool(t, tool, handler, map[string]any{"from": "Zutphen", "to": "Hengelo"})
	if isErr {
		t.Fatalf("tool error: %s", text)
	}
	var out TrainTripsResponse
	decodeInto(t, text, &out)
	if len(out.Trips) != 3 {
		t.Fatalf("expected 3 trips, got %d", len(out.Trips))
	}
	viaDeventer, cancelled, disrupted := out.Trips[0], out.Trips[1], out.Trips[2]

	// A trip NS reports as NORMAL carries no disruption block.
	if viaDeventer.Status != "NORMAL" || viaDeventer.Disruption != nil {
		t.Errorf("normal trip = %+v", viaDeventer)
	}
	if !viaDeventer.Legs[1].Reachable || viaDeventer.Legs[1].TransferMinutes != 14 {
		t.Errorf("transfer leg = %+v", viaDeventer.Legs[1])
	}

	// The cancelled direct explains itself with the upstream primary message.
	if cancelled.Status != "CANCELLED" || cancelled.Disruption == nil {
		t.Fatalf("cancelled trip = %+v", cancelled)
	}
	if d := cancelled.Disruption; d.ID != "6067828" || d.Kind != "TRIP_CANCELLED" || d.Title != "Dit reisadvies vervalt" ||
		!strings.Contains(d.Message, "object op het spoor") {
		t.Errorf("cancelled.disruption = %+v", d)
	}
	if leg := cancelled.Legs[0]; !leg.Cancelled || leg.Reachable || leg.CancelledCause != "door een object op het spoor" ||
		len(leg.Messages) != 1 || !strings.Contains(leg.Messages[0], "geen treinen") {
		t.Errorf("cancelled leg = %+v", leg)
	}

	// A trip through a disrupted section keeps its DISRUPTION status, carries
	// the disruption id and text, and is never presented as optimal.
	if disrupted.Status != "DISRUPTION" || disrupted.Disruption == nil {
		t.Fatalf("disrupted trip = %+v", disrupted)
	}
	if d := disrupted.Disruption; d.ID != "6067828" || d.Kind != "DISRUPTION" || d.Title != "Storing" {
		t.Errorf("disrupted.disruption = %+v", d)
	}
	// NS's recommendation is passed through as is, even on a disrupted trip.
	if !disrupted.Optimal || viaDeventer.Optimal {
		t.Errorf("optimal flags should match upstream: disrupted=%v viaDeventer=%v", disrupted.Optimal, viaDeventer.Optimal)
	}
	if strings.Contains(text, "primaryMessage") || strings.Contains(text, "nesProperties") {
		t.Error("lean shape leaked upstream field names")
	}
}

// ns_trips_zp_hgl_live.json is the real response recorded during the
// 2026-09-23 Zutphen - Hengelo disruption: two DISRUPTION trips that still
// run (one of them NS's optimal), then NORMAL ones.
func TestTrainTrips_LiveDisruptionRecording(t *testing.T) {
	var raw rawNSTrips
	decodeInto(t, loadTestData(t, "ns_trips_zp_hgl_live.json"), &raw)
	out := transformTrainTrips(nsclient.Station{Code: "ZP", Name: "Zutphen"}, nsclient.Station{Code: "HGL", Name: "Hengelo"}, raw)
	if len(out.Trips) != 5 {
		t.Fatalf("expected 5 trips, got %d", len(out.Trips))
	}
	for i, tr := range out.Trips[:2] {
		if tr.Status != "DISRUPTION" || tr.Disruption == nil {
			t.Fatalf("trip %d = %+v", i, tr)
		}
		d := tr.Disruption
		if d.ID != "6067828" || d.Kind != "DISRUPTION" || d.Title != "Storing" || !strings.Contains(d.Message, "object op het spoor") || d.Phase != "PHASE_1B" {
			t.Errorf("trip %d disruption = %+v", i, d)
		}
		if len(tr.Legs) != 1 || tr.Legs[0].Cancelled || !tr.Legs[0].Reachable || len(tr.Legs[0].Messages) != 1 {
			t.Errorf("trip %d leg = %+v", i, tr.Legs[0])
		}
	}
	if !out.Trips[1].Optimal {
		t.Error("NS marked the second (disrupted) trip optimal; the flag must survive")
	}
	for i, tr := range out.Trips[2:] {
		if tr.Status != "NORMAL" || tr.Disruption != nil {
			t.Errorf("trip %d should be NORMAL without a disruption block, got %+v", i+2, tr)
		}
	}
}

// A cancelled trip's primary message can arrive without the message body
// (seen live on Venlo - Düsseldorf): kind and title only, no empty strings.
func TestTransformTripDisruption_TitleOnly(t *testing.T) {
	d := transformTripDisruption(&rawNSPrimaryMessage{Title: "Dit reisadvies vervalt", Type: "TRIP_CANCELLED"})
	if d == nil || d.Kind != "TRIP_CANCELLED" || d.Title != "Dit reisadvies vervalt" || d.ID != "" || d.Message != "" {
		t.Fatalf("got %+v", d)
	}
	b, _ := json.Marshal(d)
	if strings.Contains(string(b), `"message"`) || strings.Contains(string(b), `"id"`) {
		t.Errorf("empty fields should be omitted, got %s", b)
	}
	if transformTripDisruption(nil) != nil {
		t.Error("nil in, nil out")
	}
}

func TestTransformTrainLegStop_PrefersRawLocationName(t *testing.T) {
	stop := transformTrainLegStop(rawNSLegStop{
		Name: "Schiphol Airport \u2708", RawLocationName: "Schiphol Airport", StationCode: "SHL",
		PlannedDateTime: "2026-09-23T10:05:00+0200", PlannedTrack: "1-2", ActualTrack: "2",
	})
	if stop.Name != "Schiphol Airport" {
		t.Errorf("name = %q, want the plain rawLocationName", stop.Name)
	}
	if stop.TrackChanged {
		t.Errorf("track 2 is within planned 1-2; track_changed should be false: %+v", stop)
	}
}

// ---- train_disruptions ----

func TestTrainDisruptions_LeanShape(t *testing.T) {
	trains, doer := newTrains(t)
	tool, handler := TrainDisruptionsTool(trains)
	text, isErr := callTrainTool(t, tool, handler, map[string]any{})
	if isErr {
		t.Fatalf("tool error: %s", text)
	}
	if q := lastQuery(doer); !strings.Contains(q, "/api/v3/disruptions?") || !strings.Contains(q, "isActive=true") {
		t.Errorf("expected active disruptions query, got %s", q)
	}
	var out TrainDisruptionsResponse
	decodeInto(t, text, &out)
	if len(out.Disruptions) != 4 {
		t.Fatalf("expected 4, got %d", len(out.Disruptions))
	}
	d := out.Disruptions[0]
	if d.ID != "6067807" || d.Type != "DISRUPTION" || d.Title != "Breda - Eindhoven." || !d.Active ||
		d.Start != "2026-09-22T08:03:00+02:00" || d.End != "2026-09-23T01:30:00+02:00" || d.ExpectedEnd == "" ||
		!strings.HasPrefix(d.Situation, "Door een defect spoor") || d.Cause != "defect spoor" || d.Consequence != "minder treinen" ||
		len(d.Advices) != 1 || d.Phase != "Impact bekend" || d.Impact != 2 {
		t.Errorf("disruption[0] = %+v", d)
	}
	if !containsString(d.Stations, "Tilburg") || !containsString(d.Stations, "Breda") {
		t.Errorf("stations should list the affected section, got %v", d.Stations)
	}
	m := out.Disruptions[2]
	if m.Type != "MAINTENANCE" || m.AlternativeTransport == "" {
		t.Errorf("maintenance item should carry alternative transport, got %+v", m)
	}
	if strings.Contains(text, "publicationSections") || strings.Contains(text, "timespans") {
		t.Error("lean shape leaked upstream field names")
	}
}

func TestTrainDisruptions_TypeFilterAndInactive(t *testing.T) {
	trains, doer := newTrains(t)
	tool, handler := TrainDisruptionsTool(trains)
	text, isErr := callTrainTool(t, tool, handler, map[string]any{"type": "maintenance", "include_inactive": true})
	if isErr {
		t.Fatalf("tool error: %s", text)
	}
	q := lastQuery(doer)
	if !strings.Contains(q, "type=MAINTENANCE") || strings.Contains(q, "isActive=true") {
		t.Errorf("expected type filter without isActive, got %s", q)
	}
	var out TrainDisruptionsResponse
	decodeInto(t, text, &out)
	if len(out.Disruptions) != 3 {
		t.Errorf("client-side type filter should leave the 3 maintenance items, got %d", len(out.Disruptions))
	}
}

func TestTrainDisruptions_ByStation(t *testing.T) {
	trains, doer := newTrains(t)
	tool, handler := TrainDisruptionsTool(trains)
	if _, isErr := callTrainTool(t, tool, handler, map[string]any{"station": "Utrecht"}); isErr {
		t.Fatal("unexpected tool error")
	}
	if !strings.HasSuffix(doer.lastReq.URL.Path, "/api/v3/disruptions/station/UT") {
		t.Errorf("expected station endpoint for UT, got %s", doer.lastReq.URL.Path)
	}
}

// ---- train_stations ----

func TestTrainStations_Search(t *testing.T) {
	trains, _ := newTrains(t)
	tool, handler := TrainStationsTool(trains)
	text, isErr := callTrainTool(t, tool, handler, map[string]any{"query": "Utrecht", "limit": float64(3)})
	if isErr {
		t.Fatalf("tool error: %s", text)
	}
	var out TrainStationsResponse
	decodeInto(t, text, &out)
	if len(out.Stations) != 3 || out.Stations[0].Code != "UT" {
		t.Fatalf("expected UT first among 3, got %+v", out.Stations)
	}
	s := out.Stations[0]
	if s.UICCode != "8400621" || s.Name != "Utrecht Centraal" || s.Country != "NL" || s.Type != "MEGA_STATION" || s.Coord == nil || len(s.Synonyms) != 2 {
		t.Errorf("station fields = %+v", s)
	}
	if s.DistanceM != nil {
		t.Error("distance_m should be absent for a name search")
	}
}

func TestTrainStations_Nearest(t *testing.T) {
	trains, _ := newTrains(t)
	tool, handler := TrainStationsTool(trains)
	text, isErr := callTrainTool(t, tool, handler, map[string]any{"lat": 52.0894, "lng": 5.11, "limit": float64(2)})
	if isErr {
		t.Fatalf("tool error: %s", text)
	}
	var out TrainStationsResponse
	decodeInto(t, text, &out)
	if len(out.Stations) != 2 || out.Stations[0].Code != "UT" || out.Stations[0].DistanceM == nil {
		t.Fatalf("expected UT nearest with a distance, got %+v", out.Stations)
	}
}

func TestTrainStations_RequiresQueryOrCoordinates(t *testing.T) {
	trains, _ := newTrains(t)
	tool, handler := TrainStationsTool(trains)
	if text, isErr := callTrainTool(t, tool, handler, map[string]any{}); !isErr {
		t.Errorf("expected an error, got %s", text)
	}
	if text, isErr := callTrainTool(t, tool, handler, map[string]any{"lat": 52.0}); !isErr || !strings.Contains(text, "lng") {
		t.Errorf("expected an error about lng, got %s", text)
	}
}

// ---- upstream errors and descriptions ----

type failingDoer struct{ status int }

func (f failingDoer) Do(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader(`{"title":"Not Found","detail":"Station not found: UT","status":404}`)), Header: http.Header{}}, nil
}

func TestTrainDepartures_UpstreamProblemIsToolError(t *testing.T) {
	// The station cache can't load either, so the code path goes straight to
	// the upstream error.
	trains := nsclient.NewTrains("k", failingDoer{status: 404})
	tool, handler := TrainDeparturesTool(trains)
	text, isErr := callTrainTool(t, tool, handler, map[string]any{"station": "UT"})
	if !isErr || !strings.Contains(text, "Station not found: UT") {
		t.Errorf("expected the upstream problem detail, got %q", text)
	}
}

func TestToolDescription_TrainTools(t *testing.T) {
	trains, _ := newTrains(t)
	for _, tc := range []struct {
		tool  mcp.Tool
		needs []string
	}{
		{first(TrainDeparturesTool(trains)), []string{"NS", "train_stations", "delay_seconds", "track_changed"}},
		{first(TrainTripsTool(trains)), []string{"NS", "legs", "price_eur", "transfers"}},
		{first(TrainDisruptionsTool(trains)), []string{"NS", "MAINTENANCE", "DISRUPTION"}},
		{first(TrainStationsTool(trains)), []string{"NS", "code", "train_departures"}},
	} {
		if !strings.HasPrefix(tc.tool.Name, "train_") {
			t.Errorf("tool %s should be prefixed train_", tc.tool.Name)
		}
		for _, n := range tc.needs {
			if !strings.Contains(tc.tool.Description, n) {
				t.Errorf("%s description should mention %q", tc.tool.Name, n)
			}
		}
	}
}

func first(tool mcp.Tool, _ func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) mcp.Tool {
	return tool
}

// Times come from NS as "2026-09-22T22:51:00+0200"; the lean shapes use the
// same RFC 3339 form as the rest of the server.
func TestNormalizeNSTime(t *testing.T) {
	if got := normalizeUpstreamTime("2026-09-22T22:51:00+0200"); got != "2026-09-22T22:51:00+02:00" {
		t.Errorf("got %q", got)
	}
	if got := normalizeUpstreamTime(""); got != "" {
		t.Errorf("empty should stay empty, got %q", got)
	}
}
