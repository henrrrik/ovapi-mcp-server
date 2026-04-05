package tools

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/henrrrik/ovapi-mcp-server/ovapiclient"
)

func DeparturesTool(client ovapiclient.HTTPDoer, searcher StopSearcher) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("get_departures",
		mcp.WithDescription("Get real-time departures for a Dutch public transport stop. Searches for the stop by name and returns departures for matching stops."),
		mcp.WithString("stop_name", mcp.Required(), mcp.Description("Name of the stop (e.g. 'Amsterdam Centraal', 'Utrecht', 'Schiphol')")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of matching stops to fetch departures for (default 3, max 10)")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := request.GetString("stop_name", "")
		if name == "" {
			return mcp.NewToolResultError("stop_name is required"), nil
		}

		limit := int(request.GetInt("limit", 3))
		if limit > 10 {
			limit = 10
		}
		if limit < 1 {
			limit = 3
		}

		stops, err := searcher.SearchStops(ctx, name, limit)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if len(stops) == 0 {
			return mcp.NewToolResultError("no stops found matching '" + name + "'"), nil
		}

		codes := make([]string, len(stops))
		for i, s := range stops {
			codes[i] = s.TPCCode
		}

		u := ovapiclient.BuildURL(ovapiBase, "tpc", strings.Join(codes, ","))
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}
