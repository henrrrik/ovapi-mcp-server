package tools

import (
	"context"
	"encoding/json"
	"math"
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/henrrrik/ovapi-mcp-server/db"
)

const (
	earthRadiusMeters = 6_371_000.0
	defaultRadius     = 500
	maxRadius         = 5000
	defaultNearLimit  = 10
	maxNearLimit      = 50
	// maxBBoxCandidates caps how many rows we pull from the DB for the
	// in-Go Haversine filter. The bbox itself is already tight (2 * radius
	// on each side); even a central-Amsterdam 5 km bbox yields ~2 000
	// stops, so 5 000 leaves ample headroom without inviting a runaway
	// scan on a degenerate query.
	maxBBoxCandidates = 5000
)

type NearStopsResponse struct {
	Stops []NearStopEntry `json:"stops"`
}

type NearStopEntry struct {
	TPCCode    string      `json:"tpc_code"`
	Name       string      `json:"name"`
	Town       string      `json:"town,omitempty"`
	Coord      *[2]float64 `json:"coord"`
	DistanceM  int         `json:"distance_m"`
	PairedWith []string    `json:"paired_with,omitempty"`
}

func FindStopsNearTool(searcher StopSearcher) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("find_stops_near",
		mcp.WithDescription(
			"Find Dutch public transport stops within a radius of a lat/lng point. "+
				"Returns nearest-first with Haversine distance in meters. Stops without "+
				"real coordinates (upstream sentinel) are skipped.",
		),
		mcp.WithNumber("lat", mcp.Required(), mcp.Description("Latitude in decimal degrees")),
		mcp.WithNumber("lng", mcp.Required(), mcp.Description("Longitude in decimal degrees")),
		mcp.WithNumber("radius_m", mcp.Description("Search radius in meters (default 500, max 5000)")),
		mcp.WithNumber("limit", mcp.Description("Maximum results (default 10, max 50)")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params, errResult := parseFindNearParams(request)
		if errResult != nil {
			return errResult, nil
		}

		minLat, maxLat, minLng, maxLng := bbox(params.lat, params.lng, float64(params.radius))
		candidates, err := searcher.StopsInBBox(ctx, minLat, maxLat, minLng, maxLng, maxBBoxCandidates)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		results := filterAndRankByDistance(candidates, params)
		if err := annotateNearPairs(ctx, searcher, results); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		out, err := json.Marshal(NearStopsResponse{Stops: results})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(out)), nil
	}

	return tool, handler
}

type findNearParams struct {
	lat, lng float64
	radius   int
	limit    int
}

func parseFindNearParams(request mcp.CallToolRequest) (findNearParams, *mcp.CallToolResult) {
	args := request.GetArguments()
	if _, ok := args["lat"]; !ok {
		return findNearParams{}, mcp.NewToolResultError("lat is required")
	}
	if _, ok := args["lng"]; !ok {
		return findNearParams{}, mcp.NewToolResultError("lng is required")
	}
	lat := request.GetFloat("lat", 0)
	lng := request.GetFloat("lng", 0)
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return findNearParams{}, mcp.NewToolResultError("lat/lng out of range")
	}

	radius := clampLimit(int(request.GetInt("radius_m", defaultRadius)), defaultRadius, 1, maxRadius)
	limit := clampLimit(int(request.GetInt("limit", defaultNearLimit)), defaultNearLimit, 1, maxNearLimit)

	return findNearParams{lat: lat, lng: lng, radius: radius, limit: limit}, nil
}

func filterAndRankByDistance(candidates []db.Stop, p findNearParams) []NearStopEntry {
	results := make([]NearStopEntry, 0, len(candidates))
	for _, c := range candidates {
		coord := cleanCoord(c.Latitude, c.Longitude)
		if coord == nil {
			continue
		}
		d := haversineMeters(p.lat, p.lng, c.Latitude, c.Longitude)
		if d > float64(p.radius) {
			continue
		}
		results = append(results, NearStopEntry{
			TPCCode:   c.TPCCode,
			Name:      c.Name,
			Town:      townOrEmpty(c.Town, c.Name),
			Coord:     coord,
			DistanceM: int(math.Round(d)),
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].DistanceM < results[j].DistanceM
	})
	if len(results) > p.limit {
		results = results[:p.limit]
	}
	return results
}

// bbox returns an axis-aligned bounding box around (lat, lng) that contains
// every point within radiusM meters. At Dutch latitudes, 1° lat ≈ 111_000 m;
// 1° lng ≈ 111_000 * cos(lat) m.
func bbox(lat, lng, radiusM float64) (minLat, maxLat, minLng, maxLng float64) {
	latDeg := radiusM / 111_000.0
	lngDeg := radiusM / (111_000.0 * math.Cos(lat*math.Pi/180))
	return lat - latDeg, lat + latDeg, lng - lngDeg, lng + lngDeg
}

func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	phi1 := lat1 * math.Pi / 180
	phi2 := lat2 * math.Pi / 180
	dPhi := (lat2 - lat1) * math.Pi / 180
	dLambda := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dPhi/2)*math.Sin(dPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(dLambda/2)*math.Sin(dLambda/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusMeters * c
}

func annotateNearPairs(ctx context.Context, searcher StopSearcher, stops []NearStopEntry) error {
	if searcher == nil || len(stops) == 0 {
		return nil
	}
	codes := make([]string, len(stops))
	for i, s := range stops {
		codes[i] = s.TPCCode
	}
	pairs, err := searcher.PairedStopsByCode(ctx, codes)
	if err != nil {
		return err
	}
	for i := range stops {
		if p, ok := pairs[stops[i].TPCCode]; ok {
			stops[i].PairedWith = p
		}
	}
	return nil
}
