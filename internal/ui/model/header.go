package model

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/tta-lab/lenos/internal/agent"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

const (
	headerDiag           = "╱"
	minHeaderDiags       = 3
	leftPadding          = 1
	rightPadding         = 1
	diagToDetailsSpacing = 1 // space between diagonal pattern and details section
)

type header struct {
	// cached logo and compact logo
	logo        string
	compactLogo string

	com     *common.Common
	width   int
	compact bool
}

// newHeader creates a new header model.
func newHeader(com *common.Common) *header {
	return &header{
		com: com,
	}
}

// drawHeader draws the header for the given session. todos drives the
// `TODO x/N` segment in the compact metadata strip — pass an empty slice
// when no taskwarrior job is bound to the session.
func (h *header) drawHeader(
	scr uv.Screen,
	area uv.Rectangle,
	sess *session.Session,
	compact bool,
	detailsOpen bool,
	todos []session.Todo,
	width int,
) {
	t := h.com.Styles
	if width != h.width || compact != h.compact {
		h.logo = renderLogo(h.com.Styles, compact, width)
	}

	h.width = width
	h.compact = compact

	if !compact || sess == nil {
		uv.NewStyledString(h.logo).Draw(scr, area)
		return
	}

	if sess.ID == "" {
		return
	}

	// Build compact logo lazily so it can include the agent name.
	if h.compactLogo == "" {
		t := h.com.Styles
		brand := t.Header.Brand.Render("Lenos")
		agentName := h.com.Workspace.AgentName()
		if agentName != "" {
			h.compactLogo = brand + t.HalfMuted.Render(" ("+agentName+")")
		} else {
			h.compactLogo = brand
		}
	}

	var b strings.Builder
	b.WriteString(h.compactLogo)

	availDetailWidth := width - leftPadding - rightPadding - lipgloss.Width(b.String()) - minHeaderDiags - diagToDetailsSpacing
	var sandbox *bool
	if opts := h.com.Config().Options; opts != nil {
		sandbox = opts.Sandbox
	}
	details := renderHeaderDetails(
		h.com,
		sess,
		detailsOpen,
		availDetailWidth,
		sandbox,
		todos,
	)

	remainingWidth := width -
		lipgloss.Width(b.String()) -
		lipgloss.Width(details) -
		leftPadding -
		rightPadding -
		diagToDetailsSpacing

	if remainingWidth > 0 {
		b.WriteString(" ")
	}

	b.WriteString(details)

	view := uv.NewStyledString(
		t.Base.Padding(0, rightPadding, 0, leftPadding).Render(b.String()))
	view.Draw(scr, area)
}

// renderHeaderDetails renders the details section of the header.
func renderHeaderDetails(
	com *common.Common,
	sess *session.Session,
	detailsOpen bool,
	availWidth int,
	sandbox *bool, // nil means enabled (default true)
	todos []session.Todo,
) string {
	t := com.Styles

	var parts []string

	// Sandbox status indicator
	sandboxEnabled := sandbox == nil || *sandbox // default true when nil
	if sandboxEnabled {
		parts = append(parts, t.Header.SandboxOn.Render("sandbox"))
	} else {
		parts = append(parts, t.Header.SandboxOff.Render("sandbox off"))
	}

	if model := selectedAgentModel(com); model != nil && model.CatwalkCfg.ContextWindow > 0 {
		percentage := (float64(sess.CompletionTokens+sess.PromptTokens) / float64(model.CatwalkCfg.ContextWindow)) * 100
		formattedPercentage := t.Header.Percentage.Render(fmt.Sprintf("%d%%", int(percentage)))
		parts = append(parts, formattedPercentage)
	}

	if detailsOpen && sess.PromptTokens > 0 {
		cachePercentage := float64(sess.CacheReadTokens) / float64(sess.PromptTokens) * 100
		parts = append(parts, t.Header.Percentage.Render(fmt.Sprintf("cache %d%%", int(cachePercentage))))
	}

	if branch := com.Workspace.CurrentBranch(context.Background()); branch != "" {
		parts = append(parts, t.HalfMuted.Render(branch))
	}

	jobs := com.Workspace.AgentActiveBackgroundJobs(sess.ID)
	if len(jobs) > 0 {
		parts = append(parts, t.HalfMuted.Render(formatBackgroundJobsSegment(jobs, detailsOpen)))
	}

	if seg := formatTodoSegment(t, todos); seg != "" {
		parts = append(parts, seg)
	}

	const keystroke = "ctrl+d"
	if detailsOpen {
		parts = append(parts, t.Header.Keystroke.Render(keystroke)+t.Header.KeystrokeTip.Render(" close"))
	} else {
		parts = append(parts, t.Header.Keystroke.Render(keystroke)+t.Header.KeystrokeTip.Render(" open "))
	}

	dot := t.Header.Separator.Render(" • ")
	metadata := strings.Join(parts, dot)
	metadata = dot + metadata

	cwd := formatHeaderWorkingDir(com.Workspace.WorkingDir(), detailsOpen)
	cwd = t.Header.WorkingDir.Render(cwd)

	result := cwd + metadata
	return ansi.Truncate(result, max(0, availWidth), "…")
}

func formatHeaderWorkingDir(workingDir string, detailsOpen bool) string {
	if detailsOpen {
		return workingDir
	}
	if base := filepath.Base(workingDir); base != "." && base != string(filepath.Separator) {
		return base
	}
	return workingDir
}

func formatBackgroundJobsSegment(jobs []agent.BackgroundJob, detailsOpen bool) string {
	if !detailsOpen {
		if len(jobs) == 1 {
			return "1 job"
		}
		return fmt.Sprintf("%d jobs", len(jobs))
	}

	commands := make([]string, 0, len(jobs))
	for _, job := range jobs {
		commands = append(commands, job.Command)
	}
	return fmt.Sprintf("jobs %d: %s", len(jobs), strings.Join(commands, ", "))
}

// formatTodoSegment formats the `TODO done/total` segment shown in the
// compact header strip. Returns "" when todos is empty so the caller drops
// the segment (and its leading separator dot) entirely.
func formatTodoSegment(t *styles.Styles, todos []session.Todo) string {
	if len(todos) == 0 {
		return ""
	}
	completed := 0
	for _, td := range todos {
		if td.Status == session.TodoStatusCompleted {
			completed++
		}
	}
	label := t.Header.Keystroke.Render("TODO")
	count := t.Header.KeystrokeTip.Render(fmt.Sprintf(" %d/%d", completed, len(todos)))
	return label + count
}
