package chat

import (
	"encoding/json"
	"strings"

	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

// BashToolMessageItem is a message item that represents a bash tool call.
type BashToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*BashToolMessageItem)(nil)

// NewBashToolMessageItem creates a new [BashToolMessageItem].
func NewBashToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &BashToolRenderContext{}, canceled)
}

// BashToolRenderContext renders bash tool messages.
type BashToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (b *BashToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return pendingTool(sty, "$", opts.Anim, opts.Compact)
	}

	var params CommandParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		params.Command = "failed to parse command"
	}

	// Regular bash command.
	cmd := strings.ReplaceAll(params.Command, "\n", " ")
	cmd = strings.ReplaceAll(cmd, "\t", "    ")
	header := toolHeader(sty, opts.Status, "$", cappedWidth, opts.Compact, cmd)
	if opts.Compact {
		return header
	}

	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}

	if !opts.HasResult() {
		return header
	}

	var meta CommandResponseMetadata
	_ = json.Unmarshal([]byte(opts.Result.Metadata), &meta)

	output := meta.Output
	if output == "" && opts.Result.Content != CommandNoOutput {
		output = opts.Result.Content
	}
	if output == "" {
		return header
	}

	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
	body := sty.Tool.Body.Render(toolOutputPlainContent(sty, output, bodyWidth, opts.ExpandedContent))
	return joinToolParts(header, body)
}

// joinToolParts joins header and body with a blank line separator.
func joinToolParts(header, body string) string {
	return strings.Join([]string{header, "", body}, "\n")
}
