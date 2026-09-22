package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
				"Line ids have the form '{operator}_{planning_number}_{direction}', e.g. "+
				"'GVB_17_1' (GVB tram 17, direction 1; 'GVB_17_2' is the return leg) or "+
				"'CXX_M300_1'. The planning number is often not the public number (public "+
				"line 302 may be planning number 5302) and ids cannot be constructed from "+
				"an index entry: the index is keyed by data owner (mostly 'NL_...') and those "+
				"keys do not resolve to a detail view. Take 'line_id' from a get_departures "+
				"departure or a journey response instead. Most lines appear twice in the "+
				"index — once per direction.\n\n"+
				"When 'line_id' is supplied, returns the detail for that line (line summary, "+
				"'active_journeys[]' with the current vehicle snapshots, and 'route[]' with "+
				"the full stop list).\n\n"+
				"  - active_journeys[].journey_id — pass to the journey tool for the full run\n"+
				"  - active_journeys[].status — same enum as get_departures (PLANNED/DRIVING/\n"+
				"    ARRIVED/PASSED/CANCEL/OFFROUTE), describing the vehicle at its current stop\n"+
				"  - active_journeys[].current_stop, .current_tpc_code and .current_order — the "+
				"vehicle's current stop; current_order is the 1-indexed UserStopOrderNumber "+
				"within that journey's own stop pattern, which can differ from route[] "+
				"(route[] follows the most common pattern; short-turn variants are shorter), "+
				"so join on current_tpc_code = route[].tpc_code rather than by position\n"+
				"  - route[] — stops in travel order with coordinates, is_timing_stop, and "+
				"stop_area_code when applicable\n\n"+
				"Times ('expected', 'server_time') carry the Europe/Amsterdam offset, e.g. "+
				"'2026-04-22T13:42:13+02:00'.\n\n"+
				"Set verbose=true for the raw upstream response; for the index, filters and "+
				"'limit' still apply.\n\n"+
				"Coverage: KV78turbo (Dutch bus/tram/metro/ferry). NS trains are not included.",
		),
		mcp.WithString("line_id", mcp.Description("Specific line identifier as emitted by get_departures (departures[].line_id) or journey (line_id), e.g. 'GVB_17_1' or 'CXX_M300_1'. Ids copied from this tool's own index ('NL_...') do not resolve. Omit to list all lines.")),
		mcp.WithString("mode", mcp.Description("Filter by transport mode. Accepts comma-separated values, e.g. 'tram,metro'. Known modes: 'bus', 'tram', 'metro', 'boat' (alias: 'ferry'). 'train' is accepted but returns no results (NS trains are not in KV78turbo).")),
		mcp.WithString("owner", mcp.Description("Filter by upstream DataOwnerCode. Live, nearly every Dutch line reports 'NL' and Flemish cross-border lines 'DELIJN'; operator codes such as 'GVB' or 'RET' no longer appear here, so filter by public_number or name_contains instead. Case-insensitive, comma-separated for multiple.")),
		mcp.WithString("public_number", mcp.Description("Filter by exact public line number (case-insensitive, no partial match). Useful when 'name_contains' is too loose — e.g. public_number='1' matches only line 1 across operators, while name_contains='1' also matches 10, 17, N1, etc.")),
		mcp.WithString("name_contains", mcp.Description("Case-insensitive substring match on line name or public number (e.g. 'Centraal' matches lines named after a Centraal station; '17' matches 17, 117, 170 ... — use public_number for an exact number).")),
		mcp.WithNumber("limit", mcp.Description("Max entries to return for the no-arg index (default 500, max 5000).")),
		mcp.WithBoolean("verbose", mcp.Description("If true, return the raw upstream response instead of the lean shape. Default false.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		lineID := stringArg(request, "line_id")
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
	if errors.Is(err, errLineNotFound) {
		return mcp.NewToolResultError(fmt.Sprintf("line '%s' not found upstream. Ids from the lines index "+
			"('NL_...') do not resolve; use the line_id from a get_departures departure or a journey "+
			"(e.g. 'GVB_17_1')", lineID)), nil
	}
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
	filters := linesIndexFilters{
		modes:        normalizeModeFilters(splitCSV(stringArg(request, "mode"))),
		owners:       splitCSV(stringArg(request, "owner")),
		nameContains: stringArg(request, "name_contains"),
		publicNumber: stringArg(request, "public_number"),
		limit:        int(request.GetInt("limit", 0)),
	}
	if request.GetBool("verbose", false) {
		filtered, err := filterVerboseLinesIndex(body, filters)
		if err != nil {
			return mcp.NewToolResultError("failed to filter upstream response: " + err.Error()), nil
		}
		return mcp.NewToolResultText(string(filtered)), nil
	}
	var raw rawLinesIndex
	if err := json.Unmarshal(body, &raw); err != nil {
		return mcp.NewToolResultError("failed to parse upstream response: " + err.Error()), nil
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
				"Response shape: 'line', 'line_id' (pass to the lines tool) and 'destination' "+
				"describe the run; 'server_time' is upstream's server clock; 'stops[]' is the "+
				"route in travel order (sorted by UserStopOrderNumber ascending). All times, "+
				"server_time included, carry the Europe/Amsterdam offset (e.g. "+
				"'2026-04-22T14:23:30+02:00') so they compare directly. Each stop "+
				"carries both scheduled and realtime-adjusted times:\n"+
				"  - target_arrival / target_departure — scheduled (timetable) times\n"+
				"  - expected_arrival / expected_departure — realtime-adjusted times, "+
				"equal to target_* when there is no realtime update yet\n"+
				"  - is_timing_stop — true when the stop is an official timetable anchor "+
				"(the vehicle holds to the scheduled departure if early)\n"+
				"  - stop_type — one of FIRST, INTERMEDIATE, LAST (route position)\n"+
				"  - status — same enum as get_departures (PLANNED/DRIVING/ARRIVED/PASSED/"+
				"CANCEL/OFFROUTE), per-stop: on a tracked run, stops behind the vehicle are "+
				"PASSED, its current stop ARRIVED, and stops ahead DRIVING; a run with no "+
				"realtime tracking yet reports PLANNED at every stop\n"+
				"  - platform, wheelchair_accessible, number_of_coaches — same realtime-"+
				"dependent caveats as get_departures (commonly empty for PLANNED stops)\n\n"+
				"Journey IDs encode the service date (e.g. 'GVB_20260422_17_19206_0' is "+
				"2026-04-22): they are only valid within that operating day. A stale or "+
				"unknown ID is an upstream HTTP 404, returned as a tool error.",
		),
		mcp.WithString("journey_id", mcp.Required(), mcp.Description("Journey identifier, e.g. 'GVB_20260422_17_19206_0'. Copy it verbatim from get_departures (journey_id on each departure) or lines (active_journeys[].journey_id); do not construct one. The segments are '{operator}_{YYYYMMDD}_{line_planning_number}_{journey_number}_0' — the planning number is not always the public number, and the upstream data-owner prefix (often 'NL') is already replaced with the operator code these IDs need.")),
		mcp.WithBoolean("verbose", mcp.Description("If true, return the raw upstream response instead of the lean shape. Default false.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := stringArg(request, "journey_id")
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
