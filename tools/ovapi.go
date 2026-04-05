package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/henrrrik/ovapi-mcp-server/ovapiclient"
)

const ovapiBase = "https://v0.ovapi.nl"

const maxResponseSize = 5 * 1024 * 1024 // 5 MB

func fetchJSON(ctx context.Context, client ovapiclient.HTTPDoer, rawURL string) (*mcp.CallToolResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	resp, err := client.Do(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mcp.NewToolResultError(fmt.Sprintf("OVapi returned HTTP %d", resp.StatusCode)), nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(string(body)), nil
}

func DeparturesByStopTool(client ovapiclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("ovapi_departures_by_stop",
		mcp.WithDescription("Get real-time departures for specific Dutch public transport stop(s) by timing point code (TPC). Find codes at ovzoeker.nl."),
		mcp.WithString("tpc_codes", mcp.Required(), mcp.Description("Comma-separated timing point codes (e.g. 'ut010' or 'ut010,ut020')")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		codes := request.GetString("tpc_codes", "")
		if codes == "" {
			return mcp.NewToolResultError("tpc_codes is required"), nil
		}
		u := ovapiclient.BuildURL(ovapiBase, "tpc", codes)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}

func DeparturesByAreaTool(client ovapiclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("ovapi_departures_by_area",
		mcp.WithDescription("Get real-time departures for a Dutch public transport stop area (a collection of timing points). Find codes at ovzoeker.nl."),
		mcp.WithString("stopareacode", mcp.Required(), mcp.Description("The stop area code")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		code := request.GetString("stopareacode", "")
		if code == "" {
			return mcp.NewToolResultError("stopareacode is required"), nil
		}
		u := ovapiclient.BuildURL(ovapiBase, "stopareacode", code)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}

func LinesTool(client ovapiclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("ovapi_lines",
		mcp.WithDescription("List all Dutch public transport lines, or get details for a specific line"),
		mcp.WithString("line_id", mcp.Description("Specific line identifier (e.g. 'GVB_1_1'). Omit to list all lines.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		lineID := request.GetString("line_id", "")
		var u string
		if lineID == "" {
			u = ovapiclient.BuildURL(ovapiBase, "line")
		} else {
			u = ovapiclient.BuildURL(ovapiBase, "line", lineID)
		}
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}

func JourneyTool(client ovapiclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("ovapi_journey",
		mcp.WithDescription("Get journey and vehicle information for Dutch public transport, including location data"),
		mcp.WithString("journey_id", mcp.Required(), mcp.Description("Journey identifier")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := request.GetString("journey_id", "")
		if id == "" {
			return mcp.NewToolResultError("journey_id is required"), nil
		}
		u := ovapiclient.BuildURL(ovapiBase, "journey", id)
		return fetchJSON(ctx, client, u)
	}

	return tool, handler
}
