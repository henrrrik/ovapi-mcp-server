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
	PlannedDeparture       string     `json:"planned_departure"`
	Departure              string     `json:"departure"`
	PlannedArrival         string     `json:"planned_arrival"`
	Arrival                string     `json:"arrival"`
	PlannedDurationMinutes int        `json:"planned_duration_minutes"`
	DurationMinutes        int        `json:"duration_minutes"`
	Transfers              int        `json:"transfers"`
	Status                 string     `json:"status"`
	CrowdForecast          string     `json:"crowd_forecast,omitempty"`
	PriceEUR               float64    `json:"price_eur,omitempty"`
	Optimal                bool       `json:"optimal"`
	Legs                   []TrainLeg `json:"legs"`
}

type TrainLeg struct {
	Train         string       `json:"train"`
	Category      string       `json:"category"`
	Operator      string       `json:"operator,omitempty"`
	Direction     string       `json:"direction,omitempty"`
	From          TrainLegStop `json:"from"`
	To            TrainLegStop `json:"to"`
	Stops         int          `json:"stops"`
	Cancelled     bool         `json:"cancelled"`
	CrowdForecast string       `json:"crowd_forecast,omitempty"`
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
	Legs []rawNSLeg `json:"legs"`
}

type rawNSLeg struct {
	Direction     string       `json:"direction"`
	Cancelled     bool         `json:"cancelled"`
	PartCancelled bool         `json:"partCancelled"`
	Origin        rawNSLegStop `json:"origin"`
	Destination   rawNSLegStop `json:"destination"`
	Product       nsProduct    `json:"product"`
	Stops         []struct {
		Passing bool `json:"passing"`
	} `json:"stops"`
	CrowdForecast string `json:"crowdForecast"`
}

type rawNSLegStop struct {
	Name            string `json:"name"`
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
				"'transfers', 'status' (NORMAL, CANCELLED, ...), 'crowd_forecast', "+
				"'price_eur' (second class, full fare, when known) and 'legs': one per "+
				"train with 'train', 'category', 'direction', 'from'/'to' (station code, "+
				"name, planned/actual time, track, track_changed), the number of "+
				"intermediate 'stops' and 'cancelled'.\n\n"+
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
		Legs:                   make([]TrainLeg, 0, len(t.Legs)),
	}
	for _, l := range t.Legs {
		trip.Legs = append(trip.Legs, transformTrainLeg(l))
	}
	if n := len(trip.Legs); n > 0 {
		trip.PlannedDeparture = trip.Legs[0].From.Planned
		trip.Departure = trip.Legs[0].From.Actual
		trip.PlannedArrival = trip.Legs[n-1].To.Planned
		trip.Arrival = trip.Legs[n-1].To.Actual
	}
	return trip
}

func transformTrainLeg(l rawNSLeg) TrainLeg {
	return TrainLeg{
		Train:         trainLabel(l.Product),
		Category:      l.Product.CategoryCode,
		Operator:      l.Product.OperatorName,
		Direction:     l.Direction,
		From:          transformTrainLegStop(l.Origin),
		To:            transformTrainLegStop(l.Destination),
		Stops:         len(l.Stops),
		Cancelled:     l.Cancelled || l.PartCancelled,
		CrowdForecast: l.CrowdForecast,
	}
}

func transformTrainLegStop(s rawNSLegStop) TrainLegStop {
	return TrainLegStop{
		Code:         s.StationCode,
		Name:         s.Name,
		Planned:      normalizeUpstreamTime(s.PlannedDateTime),
		Actual:       normalizeUpstreamTime(firstNonEmpty(s.ActualDateTime, s.PlannedDateTime)),
		Track:        firstNonEmpty(s.ActualTrack, s.PlannedTrack),
		TrackChanged: s.PlannedTrack != "" && s.ActualTrack != "" && s.PlannedTrack != s.ActualTrack,
	}
}
