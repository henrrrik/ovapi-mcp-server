package tools

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/henrrrik/ovapi-mcp-server/db"
)

type StopSearcher interface {
	SearchStops(ctx context.Context, query string, limit int) ([]db.Stop, error)
	PairedStopsByCode(ctx context.Context, codes []string) (map[string][]string, error)
}

func SearchStopsTool(searcher StopSearcher) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("search_stops",
		mcp.WithDescription("Search for Dutch public transport stops by name. Returns matching stops with TPC codes, names, and coordinates."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query for stop name (e.g. 'Amsterdam Centraal', 'Utrecht', 'Schiphol')")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of results to return (default 10, max 50)")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := request.GetString("query", "")
		if query == "" {
			return mcp.NewToolResultError("query is required"), nil
		}

		limit := int(request.GetInt("limit", 10))
		if limit > 50 {
			limit = 50
		}
		if limit < 1 {
			limit = 10
		}

		stops, err := searcher.SearchStops(ctx, query, limit)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if err := annotatePairs(ctx, searcher, stops); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		data, err := json.Marshal(stops)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}

	return tool, handler
}

// annotatePairs fills in PairedWith on each stop. A lookup failure logs nothing
// and returns the error so the handler can surface it to the caller.
func annotatePairs(ctx context.Context, searcher StopSearcher, stops []db.Stop) error {
	if len(stops) == 0 {
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
