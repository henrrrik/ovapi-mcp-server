package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/henrrrik/ovapi-mcp-server/ovapiclient"
)

func DeparturesTool(client ovapiclient.HTTPDoer, searcher StopSearcher) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("get_departures",
		mcp.WithDescription(
			"Get real-time departures for a Dutch public transport stop. Accepts either a fuzzy stop name or one or more TPC codes. "+
				"Returns a lean shape by default; set verbose=true for the raw upstream response (filters are still applied to verbose).\n\n"+
				"Coverage: KV78turbo bus/tram/metro/ferry only. NS trains are not included.\n\n"+
				"Per-departure 'status' vocabulary from the BISON feed: PLANNED, DRIVING, ARRIVED, PASSED, OFFROUTE, CANCEL. "+
				"'display' gives a human-friendly countdown ('Nu', 'N min', or 'HH:MM').",
		),
		mcp.WithString("stop_name", mcp.Description("Fuzzy stop name (e.g. 'Amsterdam Centraal'). One of stop_name or tpc_code is required.")),
		mcp.WithString("tpc_code", mcp.Description("Timing point code, or comma-separated list of codes (e.g. '30006018' or '30006018,30006014'). Skips fuzzy search.")),
		mcp.WithNumber("limit", mcp.Description("When using stop_name: maximum number of matching stops to fetch departures for (default 3, max 10). Ignored when tpc_code is provided.")),
		mcp.WithString("line", mcp.Description("Filter departures to a single line by public number (e.g. '17').")),
		mcp.WithString("direction", mcp.Description("Filter departures whose destination name contains this substring (case-insensitive).")),
		mcp.WithNumber("time_window_minutes", mcp.Description("Only return departures planned within the next N minutes.")),
		mcp.WithNumber("max_departures", mcp.Description("Maximum number of departures per stop after filtering.")),
		mcp.WithBoolean("include_paired", mcp.Description("When using tpc_code: auto-expand the query to include paired TPCs (opposite-direction platforms etc). Default false.")),
		mcp.WithBoolean("drop_empty", mcp.Description("Omit stops whose departures list is empty after filtering. Default false.")),
		mcp.WithBoolean("verbose", mcp.Description("If true, return the raw upstream response instead of the lean shape. Filters still apply. Default false.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		codes, errResult := resolveCodes(ctx, request, searcher)
		if errResult != nil {
			return errResult, nil
		}
		if request.GetBool("include_paired", false) {
			codes = expandPaired(ctx, searcher, codes)
		}

		u := ovapiclient.BuildURL(ovapiBase, "tpc", strings.Join(codes, ","))
		body, errResult := fetchBytes(ctx, client, u)
		if errResult != nil {
			return errResult, nil
		}

		filters := departureFilters{
			line:              request.GetString("line", ""),
			direction:         request.GetString("direction", ""),
			timeWindowMinutes: int(request.GetInt("time_window_minutes", 0)),
			maxDepartures:     int(request.GetInt("max_departures", 0)),
		}
		dropEmpty := request.GetBool("drop_empty", false)

		if request.GetBool("verbose", false) {
			filtered, err := filterVerboseTPC(body, filters)
			if err != nil {
				return mcp.NewToolResultError("failed to filter upstream response: " + err.Error()), nil
			}
			return mcp.NewToolResultText(string(filtered)), nil
		}

		var raw rawTPCResponse
		if err := json.Unmarshal(body, &raw); err != nil {
			return mcp.NewToolResultError("failed to parse upstream response: " + err.Error()), nil
		}

		lean := transformTPC(raw, filters)
		if dropEmpty {
			lean.Stops = dropEmptyStops(lean.Stops)
		}
		sortStopsByCode(lean.Stops)
		if err := annotateLeanPairs(ctx, searcher, lean.Stops); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		out, err := json.Marshal(lean)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(out)), nil
	}

	return tool, handler
}

func resolveCodes(ctx context.Context, request mcp.CallToolRequest, searcher StopSearcher) ([]string, *mcp.CallToolResult) {
	if tpc := strings.TrimSpace(request.GetString("tpc_code", "")); tpc != "" {
		return parseTPCCodes(tpc)
	}
	name := request.GetString("stop_name", "")
	if name == "" {
		return nil, mcp.NewToolResultError("one of stop_name or tpc_code is required")
	}
	limit := clampLimit(int(request.GetInt("limit", 3)), 3, 1, 10)
	stops, err := searcher.SearchStops(ctx, name, limit)
	if err != nil {
		return nil, mcp.NewToolResultError(err.Error())
	}
	if len(stops) == 0 {
		return nil, mcp.NewToolResultError("no stops found matching '" + name + "'")
	}
	codes := make([]string, len(stops))
	for i, s := range stops {
		codes[i] = s.TPCCode
	}
	return codes, nil
}

func parseTPCCodes(raw string) ([]string, *mcp.CallToolResult) {
	parts := strings.Split(raw, ",")
	codes := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			codes = append(codes, p)
		}
	}
	if len(codes) == 0 {
		return nil, mcp.NewToolResultError("tpc_code must contain at least one code")
	}
	return codes, nil
}

func clampLimit(v, def, lo, hi int) int {
	if v > hi {
		return hi
	}
	if v < lo {
		return def
	}
	return v
}

// expandPaired adds the paired TPCs (same physical stop, opposite direction)
// to the request code set. Unknown codes or missing pairings are tolerated —
// expansion is best-effort.
func expandPaired(ctx context.Context, searcher StopSearcher, codes []string) []string {
	if searcher == nil || len(codes) == 0 {
		return codes
	}
	pairs, err := searcher.PairedStopsByCode(ctx, codes)
	if err != nil {
		return codes
	}
	seen := make(map[string]bool, len(codes))
	for _, c := range codes {
		seen[c] = true
	}
	out := append([]string{}, codes...)
	for _, c := range codes {
		for _, p := range pairs[c] {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

func dropEmptyStops(stops []LeanStop) []LeanStop {
	kept := stops[:0]
	for _, s := range stops {
		if len(s.Departures) > 0 {
			kept = append(kept, s)
		}
	}
	return kept
}

// sortStopsByCode pins a deterministic order; upstream's map iteration
// would otherwise shuffle stops between identical requests.
func sortStopsByCode(stops []LeanStop) {
	sort.Slice(stops, func(i, j int) bool { return stops[i].TPCCode < stops[j].TPCCode })
}

// annotateLeanPairs populates PairedWith on each lean stop. When the searcher is
// nil (no database configured) or the input list is empty, it is a no-op.
func annotateLeanPairs(ctx context.Context, searcher StopSearcher, stops []LeanStop) error {
	if searcher == nil || len(stops) == 0 {
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

// fetchBytes performs the upstream GET and returns the body, or an MCP error result.
func fetchBytes(ctx context.Context, client ovapiclient.HTTPDoer, rawURL string) ([]byte, *mcp.CallToolResult) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, mcp.NewToolResultError(err.Error())
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, mcp.NewToolResultError(err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, mcp.NewToolResultError(fmt.Sprintf("OVapi returned HTTP %d", resp.StatusCode))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, mcp.NewToolResultError(err.Error())
	}
	return body, nil
}
