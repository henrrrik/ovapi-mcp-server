package tools

import (
	"bytes"
	"context"
	"log"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestWithLogging_Success(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	}

	wrapped := WithLogging(logger, "test_tool", handler)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"city": "Amsterdam",
	}

	_, err := wrapped(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{"tool=test_tool", "city=Amsterdam", "duration="} {
		if !bytes.Contains([]byte(output), []byte(want)) {
			t.Errorf("log output missing %q, got: %s", want, output)
		}
	}

	if bytes.Contains([]byte(output), []byte("error=")) {
		t.Error("success log should not contain error=")
	}
}

func TestWithLogging_Error(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultError("something broke"), nil
	}

	wrapped := WithLogging(logger, "failing_tool", handler)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	_, err := wrapped(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{"tool=failing_tool", "error=something broke", "duration="} {
		if !bytes.Contains([]byte(output), []byte(want)) {
			t.Errorf("log output missing %q, got: %s", want, output)
		}
	}
}
