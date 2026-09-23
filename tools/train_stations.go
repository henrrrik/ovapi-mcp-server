package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/henrrrik/ovapi-mcp-server/nsclient"
)

const (
	defaultTrainStationLimit = 10
	maxTrainStationLimit     = 50
)

// TrainStationsResponse is the lean shape returned by train_stations.
type TrainStationsResponse struct {
	Stations []TrainStation `json:"stations"`
}

type TrainStation struct {
	Code      string      `json:"code"`
	UICCode   string      `json:"uic_code,omitempty"`
	Name      string      `json:"name"`
	ShortName string      `json:"short_name,omitempty"`
	Synonyms  []string    `json:"synonyms,omitempty"`
	Country   string      `json:"country"`
	Coord     *[2]float64 `json:"coord"`
	Type      string      `json:"type"`
	DistanceM *int        `json:"distance_m,omitempty"`
}

func TrainStationsTool(trains *nsclient.Trains) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("train_stations",
		mcp.WithDescription(
			"Look up NS (Nederlandse Spoorwegen) railway stations by name, or find the "+
				"nearest ones to a coordinate. Returns the station 'code' the other "+
				"train_* tools accept (train_departures, train_trips, train_disruptions), "+
				"the UIC code, names and synonyms ('Utrecht CS'), country, coordinates "+
				"and 'type' (MEGA_STATION, INTERCITY_STATION, STOPTREIN_STATION, ...). "+
				"Name search ranks an exact code or name first, then names containing "+
				"every query word, then prefixes, with bigger stations first on ties, so "+
				"'Utrecht' returns Utrecht Centraal before the suburban halts. Covers "+
				"Dutch stations plus foreign stations served from the Netherlands. Pass "+
				"'query', or 'lat' and 'lng' for a nearest-first list with 'distance_m'.",
		),
		mcp.WithString("query", mcp.Description("Station name, part of a name, or code (e.g. 'Utrecht', 'Den Bosch', 'ASD').")),
		mcp.WithNumber("lat", mcp.Description("Latitude for a nearest-station search (with lng).")),
		mcp.WithNumber("lng", mcp.Description("Longitude for a nearest-station search (with lat).")),
		mcp.WithNumber("limit", mcp.Description("Maximum results (default 10, max 50).")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		limit := clampLimit(request.GetInt("limit", defaultTrainStationLimit), defaultTrainStationLimit, maxTrainStationLimit)
		args := request.GetArguments()
		_, hasLat := args["lat"]
		_, hasLng := args["lng"]
		switch query := stringArg(request, "query"); {
		case query != "":
			return searchTrainStations(ctx, trains, query, limit)
		case hasLat && hasLng:
			return nearestTrainStations(ctx, trains, request.GetFloat("lat", 0), request.GetFloat("lng", 0), limit)
		case hasLat || hasLng:
			return mcp.NewToolResultError("both lat and lng are required for a nearest-station search"), nil
		}
		return mcp.NewToolResultError("pass 'query' (a station name or code) or 'lat' and 'lng'"), nil
	}

	return tool, handler
}

func searchTrainStations(ctx context.Context, trains *nsclient.Trains, query string, limit int) (*mcp.CallToolResult, error) {
	stations, err := trains.Stations.Search(ctx, query, limit)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return trainResult(trainStationsFrom(stations))
}

func nearestTrainStations(ctx context.Context, trains *nsclient.Trains, lat, lng float64, limit int) (*mcp.CallToolResult, error) {
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return mcp.NewToolResultError("lat/lng out of range"), nil
	}
	nearest, err := trains.Stations.Nearest(ctx, lat, lng, limit)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return trainResult(trainStationsNear(nearest))
}

func trainStationFrom(s nsclient.Station) TrainStation {
	return TrainStation{
		Code: s.Code, UICCode: s.UICCode, Name: s.Name, ShortName: s.ShortName, Synonyms: s.Synonyms,
		Country: s.Country, Coord: cleanCoord(s.Lat, s.Lng), Type: s.Type,
	}
}

func trainStationsFrom(stations []nsclient.Station) TrainStationsResponse {
	out := TrainStationsResponse{Stations: make([]TrainStation, 0, len(stations))}
	for _, s := range stations {
		out.Stations = append(out.Stations, trainStationFrom(s))
	}
	return out
}

func trainStationsNear(stations []nsclient.StationDistance) TrainStationsResponse {
	out := TrainStationsResponse{Stations: make([]TrainStation, 0, len(stations))}
	for _, s := range stations {
		ts := trainStationFrom(s.Station)
		d := s.DistanceM
		ts.DistanceM = &d
		out.Stations = append(out.Stations, ts)
	}
	return out
}
