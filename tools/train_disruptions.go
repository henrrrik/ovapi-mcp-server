package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/henrrrik/ovapi-mcp-server/nsclient"
)

// TrainDisruptionsResponse is the lean shape returned by train_disruptions.
type TrainDisruptionsResponse struct {
	Disruptions []TrainDisruption `json:"disruptions"`
}

type TrainDisruption struct {
	ID                   string   `json:"id"`
	Type                 string   `json:"type"`
	Title                string   `json:"title"`
	Active               bool     `json:"active"`
	Start                string   `json:"start,omitempty"`
	End                  string   `json:"end,omitempty"`
	ExpectedEnd          string   `json:"expected_end,omitempty"`
	Phase                string   `json:"phase,omitempty"`
	Impact               int      `json:"impact,omitempty"`
	Situation            string   `json:"situation,omitempty"`
	Cause                string   `json:"cause,omitempty"`
	Consequence          string   `json:"consequence,omitempty"`
	AlternativeTransport string   `json:"alternative_transport,omitempty"`
	Advices              []string `json:"advices"`
	Stations             []string `json:"stations"`
}

type rawNSDisruption struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	IsActive bool   `json:"isActive"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Phase    struct {
		Label string `json:"label"`
	} `json:"phase"`
	Impact struct {
		Value int `json:"value"`
	} `json:"impact"`
	ExpectedDuration struct {
		EndTime string `json:"endTime"`
	} `json:"expectedDuration"`
	Timespans []struct {
		Situation struct {
			Label string `json:"label"`
		} `json:"situation"`
		Cause struct {
			Label string `json:"label"`
		} `json:"cause"`
		AlternativeTransport struct {
			Label string `json:"label"`
		} `json:"alternativeTransport"`
		Advices []string `json:"advices"`
	} `json:"timespans"`
	PublicationSections []struct {
		Section struct {
			Stations []struct {
				Name string `json:"name"`
			} `json:"stations"`
		} `json:"section"`
		Consequence struct {
			Description string `json:"description"`
		} `json:"consequence"`
	} `json:"publicationSections"`
}

var trainDisruptionTypes = map[string]bool{"DISRUPTION": true, "MAINTENANCE": true, "CALAMITY": true}

func TrainDisruptionsTool(trains *nsclient.Trains) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("train_disruptions",
		mcp.WithDescription(
			"Current disruptions and engineering works on the Dutch railway network, "+
				"from NS (Nederlandse Spoorwegen). Each entry has 'type' (DISRUPTION for "+
				"incidents, MAINTENANCE for planned works, CALAMITY for major events), "+
				"'title' (the affected section, e.g. 'Breda - Eindhoven.'), 'active', "+
				"'start'/'end'/'expected_end' (Europe/Amsterdam offset), 'situation' (what "+
				"is happening, in Dutch), 'cause', 'consequence' (e.g. 'minder treinen', "+
				"'er rijden bussen'), 'alternative_transport', 'advices' and the affected "+
				"'stations'. Pass 'station' to get only the disruptions touching one "+
				"station. Only active entries are returned unless 'include_inactive' is set.",
		),
		mcp.WithString("station", mcp.Description("Optional station name or code; restricts the list to disruptions affecting that station.")),
		mcp.WithString("type", mcp.Description("Optional filter: 'DISRUPTION', 'MAINTENANCE' or 'CALAMITY'.")),
		mcp.WithBoolean("include_inactive", mcp.Description("Also return entries that are not currently active (e.g. announced works). Default false.")),
		mcp.WithBoolean("verbose", mcp.Description("If true, return the raw upstream response instead of the lean shape. Default false.")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		kind := strings.ToUpper(stringArg(request, "type"))
		if kind != "" && !trainDisruptionTypes[kind] {
			return mcp.NewToolResultError(fmt.Sprintf("type must be one of DISRUPTION, MAINTENANCE or CALAMITY, got '%s'", kind)), nil
		}
		includeInactive := request.GetBool("include_inactive", false)
		path, params := "/api/v3/disruptions", url.Values{}
		if station := stringArg(request, "station"); station != "" {
			resolved, errResult := resolveTrainStation(ctx, trains, station, "station")
			if errResult != nil {
				return errResult, nil
			}
			path += "/station/" + url.PathEscape(resolved.Code)
		} else {
			if !includeInactive {
				params.Set("isActive", "true")
			}
			if kind != "" {
				params.Set("type", kind)
			}
		}
		body, errResult := nsGet(ctx, trains, path, params)
		if errResult != nil {
			return errResult, nil
		}
		if request.GetBool("verbose", false) {
			return mcp.NewToolResultText(string(body)), nil
		}
		var raw []rawNSDisruption
		if err := json.Unmarshal(body, &raw); err != nil {
			return mcp.NewToolResultError("failed to parse upstream response: " + err.Error()), nil
		}
		return trainResult(transformTrainDisruptions(raw, kind, includeInactive))
	}

	return tool, handler
}

// transformTrainDisruptions applies the type and active filters on the
// client side as well, since the per-station endpoint takes neither.
func transformTrainDisruptions(raw []rawNSDisruption, kind string, includeInactive bool) TrainDisruptionsResponse {
	out := TrainDisruptionsResponse{Disruptions: make([]TrainDisruption, 0, len(raw))}
	for _, d := range raw {
		if kind != "" && d.Type != kind {
			continue
		}
		if !includeInactive && !d.IsActive {
			continue
		}
		out.Disruptions = append(out.Disruptions, transformTrainDisruption(d))
	}
	return out
}

func transformTrainDisruption(d rawNSDisruption) TrainDisruption {
	out := TrainDisruption{
		ID: d.ID, Type: d.Type, Title: d.Title, Active: d.IsActive,
		Start:       normalizeUpstreamTime(d.Start),
		End:         normalizeUpstreamTime(d.End),
		ExpectedEnd: normalizeUpstreamTime(d.ExpectedDuration.EndTime),
		Phase:       d.Phase.Label,
		Impact:      d.Impact.Value,
		Advices:     []string{},
		Stations:    []string{},
	}
	if len(d.Timespans) > 0 {
		ts := d.Timespans[0]
		out.Situation = ts.Situation.Label
		out.Cause = ts.Cause.Label
		out.AlternativeTransport = ts.AlternativeTransport.Label
		out.Advices = append(out.Advices, ts.Advices...)
	}
	seen := map[string]bool{}
	for i, ps := range d.PublicationSections {
		if i == 0 {
			out.Consequence = ps.Consequence.Description
		}
		for _, s := range ps.Section.Stations {
			if s.Name != "" && !seen[s.Name] {
				seen[s.Name] = true
				out.Stations = append(out.Stations, s.Name)
			}
		}
	}
	return out
}
