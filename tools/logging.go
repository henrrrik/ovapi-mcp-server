package tools

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// WithLogging wraps a tool handler with audit logging.
func WithLogging(logger *log.Logger, toolName string, handler server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()

		result, err := handler(ctx, request)

		params := formatParams(request.GetArguments())
		duration := time.Since(start)

		if err != nil {
			logger.Printf("tool=%s params=%s duration=%s error=%v", toolName, params, duration, err)
		} else if result != nil && result.IsError {
			errText := ""
			if len(result.Content) > 0 {
				if tc, ok := result.Content[0].(mcp.TextContent); ok {
					errText = tc.Text
				}
			}
			logger.Printf("tool=%s params=%s duration=%s error=%s", toolName, params, duration, errText)
		} else {
			logger.Printf("tool=%s params=%s duration=%s", toolName, params, duration)
		}

		return result, err
	}
}

func formatParams(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}

	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%v", k, args[k])
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
