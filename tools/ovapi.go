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
				"max 5000); use 'mode', 'owner', 'public_number', or 'name_contains' to narrow "+
				"results — they compose (all must match).\n\n"+
				"Every line has an 'id' formatted as '{owner}_{public_number}_{direction}', "+
				"e.g. 'GVB_22_1' is GVB line 22 in direction 1, 'GVB_22_2' is the return leg, "+
				"'QBUZZ_301_1' is Qbuzz line 301 outbound, 'RET_B_1' is RET metro B outbound. "+
				"Most lines appear twice in the index — once per direction. Pass a specific "+
				"'id' as 'line_id' for the detailed view.\n\n"+
				"When 'line_id' is supplied, returns the detail for that line (line summary, "+
				"'active_journeys[]' with the current vehicle snapshots, and 'route[]' with "+
				"the full stop list).\n\n"+
				"  - active_journeys[].journey_id — pass to the journey tool for the full run\n"+
				"  - active_journeys[].status — same enum as get_departures (PLANNED/DRIVING/\n"+
				"    ARRIVED/PASSED/CANCEL/OFFROUTE), describing the vehicle at its current stop\n"+
				"  - active_journeys[].current_stop and .current_order — the vehicle's current "+
				"stop name and its 1-indexed position along route[] (UserStopOrderNumber "+
				"verbatim from upstream)\n"+
				"  - route[] — stops in travel order with coordinates, is_timing_stop, and "+
				"stop_area_code when applicable\n\n"+
				"Set verbose=true for the raw upstream response.\n\n"+
				"Coverage: KV78turbo (Dutch bus/tram/metro/ferry). NS trains are not included.",
		),
		mcp.WithString("line_id", mcp.Description("Specific line identifier (e.g. 'GVB_17_1', 'QBUZZ_301_1', 'RET_B_1'). Format: '{owner}_{public_number}_{direction}'. Omit to list all lines.")),
		mcp.WithString("mode", mcp.Description("Filter by transport mode. Accepts comma-separated values, e.g. 'tram,metro'. Known modes: 'bus', 'tram', 'metro', 'boat' (alias: 'ferry'). 'train' is accepted but returns no results (NS trains are not in KV78turbo).")),
		mcp.WithString("owner", mcp.Description("Filter by operator data-owner code (e.g. 'GVB', 'QBUZZ', 'NL', 'CXX'). Case-insensitive. Comma-separated for multiple, e.g. 'GVB,HTM'.")),
		mcp.WithString("public_number", mcp.Description("Filter by exact public line number (case-insensitive, no partial match). Useful when 'name_contains' is too loose — e.g. public_number='1' matches only line 1 across operators, while name_contains='1' also matches 10, 17, N1, etc.")),
		mcp.WithString("name_contains", mcp.Description("Case-insensitive substring match on line name or public number (e.g. '17' matches line 17 across operators, 'sprinter' matches sprinter-named lines).")),
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
		modes:        normalizeModeFilters(splitCSV(request.GetString("mode", ""))),
		owners:       splitCSV(request.GetString("owner", "")),
		nameContains: strings.TrimSpace(request.GetString("name_contains", "")),
		publicNumber: strings.TrimSpace(request.GetString("public_number", "")),
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
		mcp.WithDescription(
			"Get the full route and live progress of a single public-transport journey "+
				"(one vehicle run, e.g. line 17 tram departing Amsterdam Centraal at 08:14). "+
				"Returns a lean shape by default; set verbose=true for the raw upstream "+
				"response.\n\n"+
				"Response shape: 'line' and 'destination' describe the run; 'server_time' is "+
				"upstream's server clock (useful for staleness checks); 'stops[]' is the "+
				"route in travel order (sorted by UserStopOrderNumber ascending). Each stop "+
				"carries both scheduled and realtime-adjusted times:\n"+
				"  - target_arrival / target_departure — scheduled (timetable) times\n"+
				"  - expected_arrival / expected_departure — realtime-adjusted times, "+
				"equal to target_* when there is no realtime update yet\n"+
				"  - is_timing_stop — true when the stop is an official timetable anchor "+
				"(the vehicle holds to the scheduled departure if early)\n"+
				"  - stop_type — one of FIRST, INTERMEDIATE, LAST (route position)\n"+
				"  - status — same enum as get_departures (PLANNED/DRIVING/ARRIVED/PASSED/"+
				"CANCEL/OFFROUTE), per-stop — stops ahead of the vehicle are PLANNED, stops "+
				"behind it are PASSED\n"+
				"  - platform, wheelchair_accessible, number_of_coaches — same realtime-"+
				"dependent caveats as get_departures (commonly empty for PLANNED stops)\n\n"+
				"Journey IDs encode the service date (e.g. 'GVB_20260422_17_19206_0' is "+
				"2026-04-22): they are only valid within that operating day. Stale IDs "+
				"return an empty stops list rather than an error.",
		),
		mcp.WithString("journey_id", mcp.Required(), mcp.Description("Journey identifier in the format '{owner}_{YYYYMMDD}_{line}_{vehicle}_{direction}', e.g. 'GVB_20260422_17_19206_0'. Get journey IDs from get_departures (journey_id on each departure) or lines (active_journeys[].journey_id).")),
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
