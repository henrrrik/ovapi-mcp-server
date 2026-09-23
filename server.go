package main

import (
	"log"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/henrrrik/ovapi-mcp-server/nsclient"
	"github.com/henrrrik/ovapi-mcp-server/ovapiclient"
	"github.com/henrrrik/ovapi-mcp-server/tools"
)

// NewOVapiServer registers the OVapi tools, the database-backed stop search
// when searcher is set, and the NS train tools when trains is set.
func NewOVapiServer(client ovapiclient.HTTPDoer, searcher tools.StopSearcher, trains *nsclient.Trains) *server.MCPServer {
	// Recovery turns a panic in a handler into a JSON-RPC error instead of
	// taking down the process and every connected SSE session with it.
	s := server.NewMCPServer(
		"ovapi-mcp-server",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithRecovery(),
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

	if trains != nil {
		add(tools.TrainStationsTool(trains))
		add(tools.TrainDeparturesTool(trains))
		add(tools.TrainTripsTool(trains))
		add(tools.TrainDisruptionsTool(trains))
	}

	return s
}
