package main

import (
	"log"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/henrrrik/ovapi-mcp-server/ovapiclient"
	"github.com/henrrrik/ovapi-mcp-server/tools"
)

func NewOVapiServer(client ovapiclient.HTTPDoer, searcher tools.StopSearcher) *server.MCPServer {
	s := server.NewMCPServer(
		"ovapi-mcp-server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	logger := log.New(os.Stderr, "", log.LstdFlags)

	add := func(tool mcp.Tool, handler server.ToolHandlerFunc) {
		s.AddTool(tool, tools.WithLogging(logger, tool.Name, handler))
	}

	add(tools.LinesTool(client))
	add(tools.JourneyTool(client))

	if searcher != nil {
		add(tools.DeparturesTool(client, searcher))
		add(tools.SearchStopsTool(searcher))
		add(tools.FindStopsNearTool(searcher))
	}

	return s
}
