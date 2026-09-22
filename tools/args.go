package tools

import (
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// stringArg reads a string parameter leniently. mcp-go does not validate
// arguments against the InputSchema, and CallToolRequest.GetString returns
// the default for any non-string JSON value, so a caller passing line: 17
// instead of line: "17" would silently disable the filter. Numbers are
// therefore accepted and formatted without an exponent, and the result is
// trimmed so a stray space ("17 ") cannot turn a filter into a false
// negative. Missing, null and other types read as "".
func stringArg(request mcp.CallToolRequest, key string) string {
	v, ok := request.GetArguments()[key]
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	}
	return ""
}
