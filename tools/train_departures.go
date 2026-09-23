package tools

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/henrrrik/ovapi-mcp-server/nsclient"
)

const (
	defaultTrainJourneys = 10
	maxTrainJourneys     = 40
)

// TrainDeparturesResponse is the lean shape returned by train_departures.
type TrainDeparturesResponse struct {
	Station         TrainStationRef  `json:"station"`
	DisruptionCount int              `json:"disruption_count"`
	Departures      []TrainDeparture `json:"departures"`
}

type TrainDeparture struct {
	Train        string   `json:"train"`
	Category     string   `json:"category"`
	CategoryName string   `json:"category_name,omitempty"`
	Number       string   `json:"number"`
	Operator     string   `json:"operator,omitempty"`
	Direction    string   `json:"direction"`
	Planned      string   `json:"planned"`
	Expected     string   `json:"expected"`
	DelaySeconds int      `json:"delay_seconds"`
	PlannedTrack string   `json:"planned_track,omitempty"`
	ActualTrack  string   `json:"actual_track,omitempty"`
	TrackChanged bool     `json:"track_changed"`
	Cancelled    bool     `json:"cancelled"`
	Status       string   `json:"status,omitempty"`
	Route        []string `json:"route"`
	Messages     []string `json:"messages"`
}

type rawNSDepartures struct {
	Payload struct {
		Departures []rawNSDeparture `json:"departures"`
	} `json:"payload"`
	Meta struct {
		NumberOfDisruptions int `json:"numberOfDisruptions"`
	} `json:"meta"`
}

type rawNSDeparture struct {
	Direction       string    `json:"direction"`
	PlannedDateTime string    `json:"plannedDateTime"`
	ActualDateTime  string    `json:"actualDateTime"`
	PlannedTrack    string    `json:"plannedTrack"`
	ActualTrack     string    `json:"actualTrack"`
	Product         nsProduct `json:"product"`
	Cancelled       bool      `json:"cancelled"`
	DepartureStatus string    `json:"departureStatus"`
	RouteStations   []struct {
		MediumName string `json:"mediumName"`
	} `json:"routeStations"`
	Messages []struct {
		Message string `json:"message"`
	} `json:"messages"`
}

func TrainDeparturesTool(trains *nsclient.Trains) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("train_departures",
		mcp.WithDescription(
			"Real-time train departures from a Dutch railway station, from the NS "+
				"(Nederlandse Spoorwegen) Reisinformatie API. Covers all train operators "+
				"on the Dutch network (NS, Arriva, Keolis, ...); for bus, tram, metro and "+
				"ferry use get_departures.\n\n"+
				"'station' takes a station name or code ('Utrecht Centraal', 'Utrecht', "+
				"'UT'); use train_stations to look names and codes up. Each departure has "+
				"'train' ('IC 583'), 'category' (IC, SPR, ICD, THA, ...), 'direction', "+
				"'planned' and 'expected' times with the Europe/Amsterdam offset, "+
				"'delay_seconds', 'planned_track'/'actual_track' with 'track_changed', "+
				"'cancelled', 'status' (ON_STATION, INCOMING, UNKNOWN), 'route' (the "+
				"main stops on the way, medium names) and 'messages'. "+
				"'disruption_count' is the number of disruptions upstream links to this "+
				"station; fetch them with train_disruptions.",
		),
		mcp.WithString("station", mcp.Required(), mcp.Description("Station name or NS station code, e.g. 'Amsterdam Centraal', 'Amsterdam', 'ASD'.")),
		mcp.WithNumber("max_journeys", mcp.Description("Maximum departures to return (default 10, max 40).")),
		mcp.WithString("date_time", mcp.Description("Departures from this moment instead of now, RFC 3339, e.g. '2026-09-23T08:00:00+02:00'.")),
		mcp.WithBoolean("verbose", mcp.Description("If true, return the raw upstream response instead of the lean shape. Default false.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		station, errResult := resolveTrainStation(ctx, trains, stringArg(request, "station"), "station")
		if errResult != nil {
			return errResult, nil
		}
		params := url.Values{}
		params.Set("station", station.Code)
		params.Set("maxJourneys", strconv.Itoa(clampLimit(request.GetInt("max_journeys", defaultTrainJourneys), defaultTrainJourneys, maxTrainJourneys)))
		if dt := stringArg(request, "date_time"); dt != "" {
			params.Set("dateTime", dt)
		}
		body, errResult := nsGet(ctx, trains, "/api/v2/departures", params)
		if errResult != nil {
			return errResult, nil
		}
		if request.GetBool("verbose", false) {
			return mcp.NewToolResultText(string(body)), nil
		}
		var raw rawNSDepartures
		if err := json.Unmarshal(body, &raw); err != nil {
			return mcp.NewToolResultError("failed to parse upstream response: " + err.Error()), nil
		}
		return trainResult(transformTrainDepartures(station, raw))
	}

	return tool, handler
}

func transformTrainDepartures(station nsclient.Station, raw rawNSDepartures) TrainDeparturesResponse {
	out := TrainDeparturesResponse{
		Station:         stationRef(station),
		DisruptionCount: raw.Meta.NumberOfDisruptions,
		Departures:      make([]TrainDeparture, 0, len(raw.Payload.Departures)),
	}
	for _, d := range raw.Payload.Departures {
		out.Departures = append(out.Departures, transformTrainDeparture(d))
	}
	sort.SliceStable(out.Departures, func(i, j int) bool {
		return out.Departures[i].Planned < out.Departures[j].Planned
	})
	return out
}

func transformTrainDeparture(d rawNSDeparture) TrainDeparture {
	route := make([]string, 0, len(d.RouteStations))
	for _, r := range d.RouteStations {
		route = append(route, r.MediumName)
	}
	messages := make([]string, 0, len(d.Messages))
	for _, m := range d.Messages {
		if m.Message != "" {
			messages = append(messages, m.Message)
		}
	}
	return TrainDeparture{
		Train:        trainLabel(d.Product),
		Category:     d.Product.CategoryCode,
		CategoryName: d.Product.LongCategoryName,
		Number:       d.Product.Number,
		Operator:     d.Product.OperatorName,
		Direction:    d.Direction,
		Planned:      normalizeUpstreamTime(d.PlannedDateTime),
		Expected:     normalizeUpstreamTime(firstNonEmpty(d.ActualDateTime, d.PlannedDateTime)),
		DelaySeconds: delaySeconds(d.PlannedDateTime, d.ActualDateTime),
		PlannedTrack: d.PlannedTrack,
		ActualTrack:  d.ActualTrack,
		TrackChanged: d.PlannedTrack != "" && d.ActualTrack != "" && d.PlannedTrack != d.ActualTrack,
		Cancelled:    d.Cancelled,
		Status:       d.DepartureStatus,
		Route:        route,
		Messages:     messages,
	}
}
