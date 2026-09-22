package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/henrrrik/ovapi-mcp-server/nsclient"
)

// TrainStationRef identifies a station in train_* responses.
type TrainStationRef struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func stationRef(s nsclient.Station) TrainStationRef {
	return TrainStationRef{Code: s.Code, Name: s.Name}
}

// resolveTrainStation turns a station name or code argument into a station.
// A missing argument, an unknown name and an upstream failure each become a
// tool error.
func resolveTrainStation(ctx context.Context, trains *nsclient.Trains, value, param string) (nsclient.Station, *mcp.CallToolResult) {
	if value == "" {
		return nsclient.Station{}, mcp.NewToolResultError(
			fmt.Sprintf("%s is required: a station name or code, e.g. 'Utrecht Centraal' or 'UT'", param))
	}
	matches, err := trains.Stations.Search(ctx, value, 1)
	if err != nil {
		return nsclient.Station{}, mcp.NewToolResultError(err.Error())
	}
	if len(matches) == 0 {
		return nsclient.Station{}, mcp.NewToolResultError(
			fmt.Sprintf("no NS station matching '%s'; use train_stations to look up station names and codes", value))
	}
	return matches[0], nil
}

// nsGet fetches from the NS API, turning failures into tool errors.
func nsGet(ctx context.Context, trains *nsclient.Trains, path string, params url.Values) ([]byte, *mcp.CallToolResult) {
	body, err := trains.Client.Get(ctx, path, params)
	if err != nil {
		return nil, mcp.NewToolResultError(err.Error())
	}
	return body, nil
}

// trainResult marshals a lean shape into a tool result.
func trainResult(v any) (*mcp.CallToolResult, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(out)), nil
}

// nsProduct is the train product block shared by departures, arrivals and
// trip legs.
type nsProduct struct {
	Number            string `json:"number"`
	CategoryCode      string `json:"categoryCode"`
	LongCategoryName  string `json:"longCategoryName"`
	ShortCategoryName string `json:"shortCategoryName"`
	OperatorCode      string `json:"operatorCode"`
	OperatorName      string `json:"operatorName"`
}

// trainLabel renders a product as "IC 583" or "SPR 6939".
func trainLabel(p nsProduct) string {
	if p.CategoryCode == "" {
		return p.Number
	}
	if p.Number == "" {
		return p.CategoryCode
	}
	return p.CategoryCode + " " + p.Number
}

// delaySeconds is the difference between an actual and a planned timestamp
// in NS's "+0200" form; 0 when either is missing.
func delaySeconds(planned, actual string) int {
	p, a := parseAmsterdamTime(planned), parseAmsterdamTime(actual)
	if p.IsZero() || a.IsZero() {
		return 0
	}
	return int(a.Sub(p).Seconds())
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
