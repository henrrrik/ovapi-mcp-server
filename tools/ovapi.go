package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/henrrrik/ovapi-mcp-server/ovapiclient"
)

const ovapiBase = "https://v0.ovapi.nl"

const maxResponseSize = 5 * 1024 * 1024 // 5 MB

func LinesTool(client ovapiclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("lines",
		mcp.WithDescription(
			"List Dutch public transport lines, or get details for a specific line.\n\n"+
				"No-arg call returns a compact sorted index of line summaries (id, public_number, "+
				"name, owner, mode, direction, destination). The unfiltered index is large "+
				"(~4300 entries upstream), so the response is capped by 'limit' (default 500, "+
				"max 5000); use 'mode', 'owner', or 'name_contains' to narrow results.\n\n"+
				"When 'line_id' is supplied, returns the detail for that line (format: "+
				"'{owner}_{public_number}_{direction}', e.g. 'GVB_17_1'). Set verbose=true "+
				"for the raw upstream response.\n\n"+
				"Coverage: KV78turbo (Dutch bus/tram/metro/ferry). NS trains are not included.",
		),
		mcp.WithString("line_id", mcp.Description("Specific line identifier (e.g. 'GVB_17_1'). Format: '{owner}_{public_number}_{direction}'. Omit to list all lines.")),
		mcp.WithString("mode", mcp.Description("Filter by transport mode: 'bus', 'tram', 'metro', 'boat'.")),
		mcp.WithString("owner", mcp.Description("Filter by operator data-owner code (e.g. 'GVB', 'QBUZZ', 'NL', 'CXX'). Case-insensitive.")),
		mcp.WithString("name_contains", mcp.Description("Case-insensitive substring match on line name, public number, or id.")),
		mcp.WithNumber("limit", mcp.Description("Max entries to return for the no-arg index (default 500, max 5000).")),
		mcp.WithBoolean("verbose", mcp.Description("If true, return the raw upstream response instead of the lean shape. Default false.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		lineID := request.GetString("line_id", "")
		if lineID != "" {
			return handleLineDetail(ctx, client, request, lineID)
		}
		return handleLinesIndex(ctx, client, request)
	}

	return tool, handler
}

func handleLineDetail(ctx context.Context, client ovapiclient.HTTPDoer, request mcp.CallToolRequest, lineID string) (*mcp.CallToolResult, error) {
	u := ovapiclient.BuildURL(ovapiBase, "line", lineID)
	body, errResult := fetchBytes(ctx, client, u)
	if errResult != nil {
		return errResult, nil
	}
	if request.GetBool("verbose", false) {
		return mcp.NewToolResultText(string(body)), nil
	}
	lean, err := transformLineDetail(body, lineID)
	if err != nil {
		return mcp.NewToolResultError("failed to parse upstream response: " + err.Error()), nil
	}
	out, err := json.Marshal(lean)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(out)), nil
}

func handleLinesIndex(ctx context.Context, client ovapiclient.HTTPDoer, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	u := ovapiclient.BuildURL(ovapiBase, "line")
	body, errResult := fetchBytes(ctx, client, u)
	if errResult != nil {
		return errResult, nil
	}
	if request.GetBool("verbose", false) {
		return mcp.NewToolResultText(string(body)), nil
	}
	var raw rawLinesIndex
	if err := json.Unmarshal(body, &raw); err != nil {
		return mcp.NewToolResultError("failed to parse upstream response: " + err.Error()), nil
	}
	filters := linesIndexFilters{
		mode:         strings.ToLower(strings.TrimSpace(request.GetString("mode", ""))),
		owner:        strings.TrimSpace(request.GetString("owner", "")),
		nameContains: strings.TrimSpace(request.GetString("name_contains", "")),
		limit:        int(request.GetInt("limit", 0)),
	}
	resp := transformLinesIndex(raw, filters)
	out, err := json.Marshal(resp)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(out)), nil
}

func JourneyTool(client ovapiclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("journey",
		mcp.WithDescription("Get journey and vehicle information for a Dutch public transport journey. Returns a lean shape by default (line, destination, server_time, and stops sorted by order); set verbose=true for the raw upstream response."),
		mcp.WithString("journey_id", mcp.Required(), mcp.Description("Journey identifier (e.g. 'GVB_20260422_17_19206_0')")),
		mcp.WithBoolean("verbose", mcp.Description("If true, return the raw upstream response instead of the lean shape. Default false.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := request.GetString("journey_id", "")
		if id == "" {
			return mcp.NewToolResultError("journey_id is required"), nil
		}
		u := ovapiclient.BuildURL(ovapiBase, "journey", id)
		body, errResult := fetchBytes(ctx, client, u)
		if errResult != nil {
			return errResult, nil
		}
		if request.GetBool("verbose", false) {
			return mcp.NewToolResultText(string(body)), nil
		}
		lean, err := transformJourney(body, id)
		if err != nil {
			return mcp.NewToolResultError("failed to parse upstream response: " + err.Error()), nil
		}
		out, err := json.Marshal(lean)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(out)), nil
	}

	return tool, handler
}
