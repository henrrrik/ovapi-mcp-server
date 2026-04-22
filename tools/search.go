package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/henrrrik/ovapi-mcp-server/db"
)

type StopSearcher interface {
	SearchStops(ctx context.Context, query string, limit int) ([]db.Stop, error)
	PairedStopsByCode(ctx context.Context, codes []string) (map[string][]string, error)
	StopsInBBox(ctx context.Context, minLat, maxLat, minLng, maxLng float64, limit int) ([]db.Stop, error)
}

// SearchResponse is the top-level shape for search_stops. Using a wrapper
// makes it easy to add metadata fields (count, truncated, etc) later without
// another breaking change.
type SearchResponse struct {
	Stops []SearchResultStop `json:"stops"`
}

func SearchStopsTool(searcher StopSearcher) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("search_stops",
		mcp.WithDescription(
			"Search for Dutch public transport stops by name. Returns stops ranked by "+
				"match quality with a 'score' (0-1000): 1000 for exact full-name matches, "+
				"800 when every query token appears at a word boundary of the stop name, "+
				"650 for substring matches, 350 for partial. True interchanges/hub stops "+
				"(stops with a stop_area_code, multiple paired platforms, or canonical "+
				"names like 'Centraal' or '*Station') get a scaled boost that can lift "+
				"them up to ~100 points higher.\n\n"+
				"Station-name aliases are normalized: 'CS' matches 'Centraal Station' and "+
				"vice versa, so 'Utrecht Centraal' finds 'Utrecht, CS Centrumzijde' and "+
				"'Den Haag CS' finds 'Den Haag, Centraal Station'.\n\n"+
				"Queries shorter than 3 characters return an empty list. Each result may "+
				"include 'paired_with' — other TPC codes for the same physical stop, "+
				"typically the opposite-direction platform, an adjacent quay, or a "+
				"rail-side versus bus-side entry.",
		),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query for stop name (e.g. 'Amsterdam Centraal', 'Utrecht', 'Schiphol'). Minimum 3 characters.")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of results to return (default 10, max 50)")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := strings.TrimSpace(request.GetString("query", ""))
		if query == "" {
			return mcp.NewToolResultError("query is required"), nil
		}

		limit := clampLimit(int(request.GetInt("limit", 10)), 10, 1, 50)

		if len([]rune(query)) < scoreMinQueryLength {
			return writeSearchResponse(SearchResponse{Stops: []SearchResultStop{}})
		}

		// Over-fetch and re-rank in Go so we can apply tiered scoring that
		// a plain pg_trgm ORDER BY cannot express.
		candidates, err := searcher.SearchStops(ctx, query, limit*searchCandidateFanout)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		codes := make([]string, len(candidates))
		for i, s := range candidates {
			codes[i] = s.TPCCode
		}
		pairs, err := searcher.PairedStopsByCode(ctx, codes)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		ranked := scoreAndRank(query, candidates, pairs, limit)
		return writeSearchResponse(SearchResponse{Stops: ranked})
	}

	return tool, handler
}

func writeSearchResponse(resp SearchResponse) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(resp)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
