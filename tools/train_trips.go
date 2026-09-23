package tools

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/henrrrik/ovapi-mcp-server/nsclient"
)

// TrainTripsResponse is the lean shape returned by train_trips.
type TrainTripsResponse struct {
	From  TrainStationRef `json:"from"`
	To    TrainStationRef `json:"to"`
	Trips []TrainTrip     `json:"trips"`
}

type TrainTrip struct {
	PlannedDeparture       string               `json:"planned_departure"`
	Departure              string               `json:"departure"`
	PlannedArrival         string               `json:"planned_arrival"`
	Arrival                string               `json:"arrival"`
	PlannedDurationMinutes int                  `json:"planned_duration_minutes"`
	DurationMinutes        int                  `json:"duration_minutes"`
	Transfers              int                  `json:"transfers"`
	Status                 string               `json:"status"`
	CrowdForecast          string               `json:"crowd_forecast,omitempty"`
	PriceEUR               float64              `json:"price_eur,omitempty"`
	Optimal                bool                 `json:"optimal"`
	Disruption             *TrainTripDisruption `json:"disruption,omitempty"`
	Legs                   []TrainLeg           `json:"legs"`
}

// TrainTripDisruption is why a trip is not NORMAL: the upstream primary
// message, keyed to the disruption id train_disruptions returns.
type TrainTripDisruption struct {
	ID      string `json:"id,omitempty"`
	Kind    string `json:"kind"`
	Title   string `json:"title,omitempty"`
	Message string `json:"message,omitempty"`
	Phase   string `json:"phase,omitempty"`
}

type TrainLeg struct {
	Train                string       `json:"train"`
	Category             string       `json:"category"`
	Operator             string       `json:"operator,omitempty"`
	Direction            string       `json:"direction,omitempty"`
	From                 TrainLegStop `json:"from"`
	To                   TrainLegStop `json:"to"`
	Stops                int          `json:"stops"`
	Cancelled            bool         `json:"cancelled"`
	CancelledCause       string       `json:"cancelled_cause,omitempty"`
	Reachable            bool         `json:"reachable"`
	AlternativeTransport bool         `json:"alternative_transport,omitempty"`
	TransferMinutes      int          `json:"transfer_minutes,omitempty"`
	Messages             []string     `json:"messages,omitempty"`
	CrowdForecast        string       `json:"crowd_forecast,omitempty"`
}

type TrainLegStop struct {
	Code         string `json:"code"`
	Name         string `json:"name"`
	Planned      string `json:"planned"`
	Actual       string `json:"actual"`
	Track        string `json:"track,omitempty"`
	TrackChanged bool   `json:"track_changed"`
}

type rawNSTrips struct {
	Trips []rawNSTrip `json:"trips"`
}

type rawNSTrip struct {
	PlannedDurationInMinutes int    `json:"plannedDurationInMinutes"`
	ActualDurationInMinutes  int    `json:"actualDurationInMinutes"`
	Transfers                int    `json:"transfers"`
	Status                   string `json:"status"`
	CrowdForecast            string `json:"crowdForecast"`
	Optimal                  bool   `json:"optimal"`
	ProductFare              struct {
		PriceInCents int `json:"priceInCents"`
	} `json:"productFare"`
	PrimaryMessage *rawNSPrimaryMessage `json:"primaryMessage"`
	Legs           []rawNSLeg           `json:"legs"`
}

// rawNSPrimaryMessage is the banner NS shows on a cancelled or disrupted trip.
type rawNSPrimaryMessage struct {
	Title   string       `json:"title"`
	Type    string       `json:"type"`
	Message rawNSMessage `json:"message"`
}

type rawNSMessage struct {
	ID    string `json:"id"`
	Head  string `json:"head"`
	Text  string `json:"text"`
	Type  string `json:"type"`
	Phase string `json:"phase"`
}

type rawNSLeg struct {
	Direction            string       `json:"direction"`
	Cancelled            bool         `json:"cancelled"`
	PartCancelled        bool         `json:"partCancelled"`
	Reachable            *bool        `json:"reachable"`
	AlternativeTransport bool         `json:"alternativeTransport"`
	Origin               rawNSLegStop `json:"origin"`
	Destination          rawNSLegStop `json:"destination"`
	Product              nsProduct    `json:"product"`
	Stops                []struct {
		Passing bool `json:"passing"`
	} `json:"stops"`
	Notes []struct {
		Value string `json:"value"`
		Key   string `json:"key"`
	} `json:"notes"`
	Messages      []rawNSMessage `json:"messages"`
	CrowdForecast string         `json:"crowdForecast"`
}

type rawNSLegStop struct {
	Name            string `json:"name"`
	RawLocationName string `json:"rawLocationName"`
	StationCode     string `json:"stationCode"`
	PlannedDateTime string `json:"plannedDateTime"`
	ActualDateTime  string `json:"actualDateTime"`
	PlannedTrack    string `json:"plannedTrack"`
	ActualTrack     string `json:"actualTrack"`
}

func TrainTripsTool(trains *nsclient.Trains) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("train_trips",
		mcp.WithDescription(
			"Plan a train journey between two Dutch railway stations with the NS "+
				"(Nederlandse Spoorwegen) journey planner. Returns the next few "+
				"itineraries (or the ones arriving before 'date_time' when "+
				"'search_for_arrival' is set). Each trip has planned and realtime "+
				"departure/arrival times (Europe/Amsterdam offset), 'duration_minutes', "+
				"'transfers', 'status' (NORMAL, DISRUPTION, CANCELLED, ...), 'crowd_forecast', "+
				"'price_eur' (second class, full fare, when known) and 'legs': one per "+
				"train with 'train', 'category', 'direction', 'from'/'to' (station code, "+
				"name, planned/actual time, track, track_changed), the number of "+
				"intermediate 'stops', 'cancelled' (with 'cancelled_cause'), 'reachable' "+
				"(false when a connection cannot be made), 'transfer_minutes' from the "+
				"previous leg and any 'messages'. A trip whose status is not NORMAL has a "+
				"'disruption' block: 'kind' (TRIP_CANCELLED or DISRUPTION), the NS "+
				"disruption 'id' (look it up with train_disruptions), 'title' and "+
				"'message' (a cancelled trip may carry only the title). 'optimal' is NS's "+
				"own recommendation, passed through as is: a delayed train on a disrupted "+
				"section can still be the best option.\n\n"+
				"Stations take a name or code; use train_stations to look them up. Trains "+
				"only: bus/tram/metro legs are not planned here (see get_departures).",
		),
		mcp.WithString("from", mcp.Required(), mcp.Description("Origin station name or code, e.g. 'Amsterdam Centraal' or 'ASD'.")),
		mcp.WithString("to", mcp.Required(), mcp.Description("Destination station name or code, e.g. 'Utrecht' or 'UT'.")),
		mcp.WithString("via", mcp.Description("Optional via station name or code.")),
		mcp.WithString("date_time", mcp.Description("Departure moment (or arrival moment with search_for_arrival), RFC 3339, e.g. '2026-09-23T08:00:00+02:00'. Default now.")),
		mcp.WithBoolean("search_for_arrival", mcp.Description("If true, date_time is the latest arrival time instead of the earliest departure. Default false.")),
		mcp.WithBoolean("verbose", mcp.Description("If true, return the raw upstream response instead of the lean shape. Default false.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		from, errResult := resolveTrainStation(ctx, trains, stringArg(request, "from"), "from")
		if errResult != nil {
			return errResult, nil
		}
		to, errResult := resolveTrainStation(ctx, trains, stringArg(request, "to"), "to")
		if errResult != nil {
			return errResult, nil
		}
		params := url.Values{}
		params.Set("fromStation", from.Code)
		params.Set("toStation", to.Code)
		if via := stringArg(request, "via"); via != "" {
			viaStation, errResult := resolveTrainStation(ctx, trains, via, "via")
			if errResult != nil {
				return errResult, nil
			}
			params.Set("viaStation", viaStation.Code)
		}
		if dt := stringArg(request, "date_time"); dt != "" {
			params.Set("dateTime", dt)
		}
		if request.GetBool("search_for_arrival", false) {
			params.Set("searchForArrival", "true")
		}
		body, errResult := nsGet(ctx, trains, "/api/v3/trips", params)
		if errResult != nil {
			return errResult, nil
		}
		if request.GetBool("verbose", false) {
			return mcp.NewToolResultText(string(body)), nil
		}
		var raw rawNSTrips
		if err := json.Unmarshal(body, &raw); err != nil {
			return mcp.NewToolResultError("failed to parse upstream response: " + err.Error()), nil
		}
		return trainResult(transformTrainTrips(from, to, raw))
	}

	return tool, handler
}

func transformTrainTrips(from, to nsclient.Station, raw rawNSTrips) TrainTripsResponse {
	out := TrainTripsResponse{From: stationRef(from), To: stationRef(to), Trips: make([]TrainTrip, 0, len(raw.Trips))}
	for _, t := range raw.Trips {
		out.Trips = append(out.Trips, transformTrainTrip(t))
	}
	return out
}

func transformTrainTrip(t rawNSTrip) TrainTrip {
	trip := TrainTrip{
		PlannedDurationMinutes: t.PlannedDurationInMinutes,
		DurationMinutes:        t.ActualDurationInMinutes,
		Transfers:              t.Transfers,
		Status:                 t.Status,
		CrowdForecast:          t.CrowdForecast,
		PriceEUR:               float64(t.ProductFare.PriceInCents) / 100,
		Optimal:                t.Optimal,
		Disruption:             transformTripDisruption(t.PrimaryMessage),
		Legs:                   make([]TrainLeg, 0, len(t.Legs)),
	}
	for i, l := range t.Legs {
		leg := transformTrainLeg(l)
		if i > 0 {
			leg.TransferMinutes = transferMinutes(trip.Legs[i-1].To.Actual, leg.From.Actual)
		}
		trip.Legs = append(trip.Legs, leg)
	}
	if n := len(trip.Legs); n > 0 {
		trip.PlannedDeparture = trip.Legs[0].From.Planned
		trip.Departure = trip.Legs[0].From.Actual
		trip.PlannedArrival = trip.Legs[n-1].To.Planned
		trip.Arrival = trip.Legs[n-1].To.Actual
	}
	return trip
}

// transformTripDisruption turns the upstream primary message into the lean
// disruption block; nil when the trip runs as planned.
func transformTripDisruption(m *rawNSPrimaryMessage) *TrainTripDisruption {
	if m == nil {
		return nil
	}
	return &TrainTripDisruption{
		ID:      m.Message.ID,
		Kind:    firstNonEmpty(m.Type, m.Message.Type),
		Title:   m.Title,
		Message: firstNonEmpty(m.Message.Text, m.Message.Head),
		Phase:   m.Message.Phase,
	}
}

// transferMinutes is the realtime gap between arriving on one leg and
// departing on the next; 0 when either time is missing.
func transferMinutes(arrival, departure string) int {
	a, d := parseAmsterdamTime(arrival), parseAmsterdamTime(departure)
	if a.IsZero() || d.IsZero() {
		return 0
	}
	return int(d.Sub(a).Minutes())
}

func transformTrainLeg(l rawNSLeg) TrainLeg {
	return TrainLeg{
		Train:                trainLabel(l.Product),
		Category:             l.Product.CategoryCode,
		Operator:             l.Product.OperatorName,
		Direction:            l.Direction,
		From:                 transformTrainLegStop(l.Origin),
		To:                   transformTrainLegStop(l.Destination),
		Stops:                len(l.Stops),
		Cancelled:            l.Cancelled || l.PartCancelled,
		CancelledCause:       legNote(l, "CANCELLATION_CAUSE"),
		Reachable:            l.Reachable == nil || *l.Reachable,
		AlternativeTransport: l.AlternativeTransport,
		Messages:             legMessages(l),
		CrowdForecast:        l.CrowdForecast,
	}
}

// legNote returns the value of the first note with the given key.
func legNote(l rawNSLeg, key string) string {
	for _, n := range l.Notes {
		if n.Key == key {
			return n.Value
		}
	}
	return ""
}

// legMessages flattens the upstream message list to distinct texts.
func legMessages(l rawNSLeg) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range l.Messages {
		text := firstNonEmpty(m.Text, m.Head)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		out = append(out, text)
	}
	return out
}

func transformTrainLegStop(s rawNSLegStop) TrainLegStop {
	return TrainLegStop{
		Code:         s.StationCode,
		Name:         firstNonEmpty(s.RawLocationName, s.Name),
		Planned:      normalizeUpstreamTime(s.PlannedDateTime),
		Actual:       normalizeUpstreamTime(firstNonEmpty(s.ActualDateTime, s.PlannedDateTime)),
		Track:        firstNonEmpty(s.ActualTrack, s.PlannedTrack),
		TrackChanged: trackChanged(s.PlannedTrack, s.ActualTrack),
	}
}
