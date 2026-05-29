package chat

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/stringext"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

// genericPrettyName converts a snake_case or kebab-case tool name to a
// human-readable title case name.
func genericPrettyName(name string) string {
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	return stringext.Capitalize(name)
}

// ResultMessageItem represents a command result message in the chat UI.
type ResultMessageItem struct {
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	message *message.Message
	sty     *styles.Styles
}

var _ MessageItem = (*ResultMessageItem)(nil)

// NewResultMessageItem creates a new ResultMessageItem.
func NewResultMessageItem(sty *styles.Styles, message *message.Message) MessageItem {
	return &ResultMessageItem{
		highlightableMessageItem: defaultHighlighter(sty),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     &focusableMessageItem{},
		message:                  message,
		sty:                      sty,
	}
}

// RawRender implements [MessageItem].
func (m *ResultMessageItem) RawRender(width int) string {
	cappedWidth := cappedMessageWidth(width)

	rendered, height, ok := m.getCachedRender(cappedWidth)
	if ok {
		return m.renderHighlighted(rendered, cappedWidth, height)
	}

	var content string
	cmd := m.message.CommandContent()
	if strings.TrimSpace(cmd.Narration) != "" {
		content = m.renderNarration(cappedWidth, cmd.Narration)
	} else if cmd.Command != "" {
		content = m.renderCommandResult(cappedWidth, cmd)
	} else {
		content = m.sty.Chat.Message.ResultBlock.Render(m.message.Content().Text)
	}

	height = lipgloss.Height(content)
	m.setCachedRender(content, cappedWidth, height)
	return m.renderHighlighted(content, cappedWidth, height)
}

func (m *ResultMessageItem) renderNarration(width int, body string) string {
	renderer := common.PlainMarkdownRenderer(m.sty, width)
	rendered, err := renderer.Render(body)
	if err != nil {
		return body
	}
	return strings.TrimSpace(rendered)
}

// renderCommandResult renders a command result with header and output.
func (m *ResultMessageItem) renderCommandResult(width int, cmd message.CommandContent) string {
	if cmd.Pending {
		pendingStyle := m.sty.Tool.StateWaiting.Width(width)
		return pendingStyle.Render("running...")
	}
	var parts []string
	if cmd.ExitCode != nil && *cmd.ExitCode != 0 {
		// Non-zero exit: output body and compact × N badge.
		var sb strings.Builder
		if cmd.Output != "" {
			bodyStyle := m.sty.Tool.Body.Width(width)
			sb.WriteString(bodyStyle.Render(cmd.Output))
			sb.WriteString(" ")
		}
		sb.WriteString(m.sty.Tool.IconError.Render(fmt.Sprintf("%d", *cmd.ExitCode)))
		parts = append(parts, sb.String())
	}
	if len(parts) == 0 {
		// Exit 0: assistant already shows the command.
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// Render implements MessageItem.
func (m *ResultMessageItem) Render(width int) string {
	return m.RawRender(width)
}

// ID implements MessageItem.
func (m *ResultMessageItem) ID() string {
	return m.message.ID
}

// HandleKeyEvent implements [KeyEventHandler].
func (m *ResultMessageItem) HandleKeyEvent(key tea.KeyMsg) (bool, tea.Cmd) {
	if k := key.String(); k == "c" || k == "y" {
		return true, common.CopyToClipboard(m.CopyText(), "Command copied to clipboard")
	}
	return false, nil
}

func (m *ResultMessageItem) CopyText() string {
	return m.formatCommandForCopy()
}

// formatCommandForCopy formats the command result for clipboard copying.
func (m *ResultMessageItem) formatCommandForCopy() string {
	cmd := m.message.CommandContent()

	// TextContent (runtime responses): return text directly.
	if cmd.Command == "" {
		return m.message.Content().Text
	}

	// Pending commands: just the command line.
	if cmd.Pending {
		return "$ " + cmd.Command
	}

	var sb strings.Builder
	sb.WriteString("$ ")
	sb.WriteString(cmd.Command)

	if cmd.Output != "" {
		sb.WriteString("\n")
		sb.WriteString(cmd.Output)
	}

	// Append exit code for non-zero exits on its own line.
	if cmd.ExitCode != nil && *cmd.ExitCode != 0 {
		sb.WriteString("\n[")
		fmt.Fprintf(&sb, "%d", *cmd.ExitCode)
		sb.WriteString("]")
	}

	return sb.String()
}
