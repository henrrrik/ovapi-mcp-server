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

		data, err := json.Marshal(stops)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}

	return tool, handler
}
