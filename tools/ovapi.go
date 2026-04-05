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

func LinesTool(client ovapiclient.HTTPDoer) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("lines",
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
	tool := mcp.NewTool("journey",
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
