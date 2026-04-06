package tools

import (
	"context"
	_ "embed"
	"log/slog"
	"strings"

	"charm.land/fantasy"
	"github.com/tta-lab/lenos/internal/agent/tools/mcp"
	"github.com/tta-lab/lenos/internal/config"
)

type ReadMCPResourceParams struct {
	MCPName string `json:"mcp_name" description:"The MCP server name"`
	URI     string `json:"uri" description:"The resource URI to read"`
}

const ReadMCPResourceToolName = "read_mcp_resource"

//go:embed read_mcp_resource.md
var readMCPResourceDescription []byte

func NewReadMCPResourceTool(cfg *config.ConfigStore) fantasy.AgentTool {
	return fantasy.NewParallelAgentTool(
		ReadMCPResourceToolName,
		string(readMCPResourceDescription),
		func(ctx context.Context, params ReadMCPResourceParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			params.MCPName = strings.TrimSpace(params.MCPName)
			params.URI = strings.TrimSpace(params.URI)
			if params.MCPName == "" {
				return fantasy.NewTextErrorResponse("mcp_name parameter is required"), nil
			}
			if params.URI == "" {
				return fantasy.NewTextErrorResponse("uri parameter is required"), nil
			}

			contents, err := mcp.ReadResource(ctx, cfg, params.MCPName, params.URI)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if len(contents) == 0 {
				return fantasy.NewTextResponse(""), nil
			}

			var textParts []string
			for _, content := range contents {
				if content == nil {
					continue
				}
				if content.Text != "" {
					textParts = append(textParts, content.Text)
					continue
				}
				if len(content.Blob) > 0 {
					textParts = append(textParts, string(content.Blob))
					continue
				}
				slog.Debug("MCP resource content missing text/blob", "uri", content.URI)
			}

			if len(textParts) == 0 {
				return fantasy.NewTextResponse(""), nil
			}

			return fantasy.NewTextResponse(strings.Join(textParts, "\n")), nil
		},
	)
}
