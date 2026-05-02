package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	uistyles "github.com/tta-lab/lenos/internal/ui/styles"
)

// FooterState is one of three derivation outcomes from inspecting the .md tail.
type FooterState int

const (
	FooterStateActive FooterState = iota
	FooterStateIdle
	FooterStateTurnEnded
)

// FooterDerivation is the result of inspecting the .md content.
type FooterDerivation struct {
	State         FooterState
	LatestBashCmd string        // first line of the latest bash block (active state only)
	LastDuration  time.Duration // duration parsed from the most recent trailer
	TurnNumber    int           // 1-indexed; turn-ended state only
}

// DeriveFooter inspects the markdown body and returns the state-driving info.
// Skips runtime-event blockquotes when locating the "last meaningful line".
func DeriveFooter(md []byte) FooterDerivation {
	// Tokenize into sections by blank lines.
	sections := tokenizeSections(string(md))
	if len(sections) == 0 {
		return FooterDerivation{State: FooterStateIdle}
	}

	// Walk backward from the last section, skipping runtime events.
	for i := len(sections) - 1; i >= 0; i-- {
		s := sections[i]
		if isRuntimeEvent(s) {
			continue
		}
		return classifySection(s, sections, i)
	}

	return FooterDerivation{State: FooterStateIdle}
}

// section represents a contiguous block of non-blank lines.
type section struct {
	lines    []string
	typeHint string // "bash", "trailer", "turn_end", "user_msg", "other"
}

func tokenizeSections(body string) []section {
	var sections []section
	var current []string
	var currentType string

	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(current) > 0 {
				sections = append(sections, section{lines: current, typeHint: currentType})
				current = nil
				currentType = ""
			}
			continue
		}

		// Classify the first non-blank line of a new section.
		if len(current) == 0 {
			currentType = classifyLine(trimmed)
		}

		current = append(current, trimmed)
	}

	if len(current) > 0 {
		sections = append(sections, section{lines: current, typeHint: currentType})
	}

	return sections
}

func classifyLine(line string) string {
	if strings.HasPrefix(line, "```lenos-bash") || strings.HasPrefix(line, "``` lenos-bash") {
		return "bash"
	}
	if strings.HasPrefix(line, "```bash") || strings.HasPrefix(line, "``` bash") {
		return "bash"
	}
	if strings.HasPrefix(line, "*[") && strings.Contains(line, "s]*") {
		return "trailer"
	}
	if strings.TrimSpace(line) == "*(turn ended)*" {
		return "turn_end"
	}
	if strings.HasPrefix(line, "**λ**") {
		return "user_msg"
	}
	return "other"
}

func isRuntimeEvent(s section) bool {
	if len(s.lines) == 0 {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(s.lines[0]), "> *runtime:")
}

func classifySection(s section, allSections []section, idx int) FooterDerivation {
	switch s.typeHint {
	case "bash":
		cmd := extractBashCmd(s.lines)
		return FooterDerivation{State: FooterStateActive, LatestBashCmd: cmd}

	case "trailer":
		dur := parseDuration(s.lines)
		// Check if the next non-runtime section is a turn_end marker.
		for i := idx + 1; i < len(allSections); i++ {
			if allSections[i].typeHint == "turn_end" {
				return FooterDerivation{
					State:        FooterStateTurnEnded,
					LastDuration: dur,
					TurnNumber:   countTurnEndsBefore(allSections, i),
				}
			}
		}
		return FooterDerivation{State: FooterStateIdle, LastDuration: dur}

	case "turn_end":
		// Find the most recent trailer before this turn end.
		for i := idx - 1; i >= 0; i-- {
			if allSections[i].typeHint == "trailer" {
				dur := parseDuration(allSections[i].lines)
				return FooterDerivation{
					State:        FooterStateTurnEnded,
					LastDuration: dur,
					TurnNumber:   countTurnEndsBefore(allSections, idx),
				}
			}
		}
		return FooterDerivation{
			State:      FooterStateTurnEnded,
			TurnNumber: countTurnEndsBefore(allSections, idx),
		}

	case "user_msg":
		return FooterDerivation{State: FooterStateIdle}

	default:
		return FooterDerivation{State: FooterStateIdle}
	}
}

func extractBashCmd(lines []string) string {
	for _, line := range lines {
		if strings.HasPrefix(line, "```lenos-bash") || strings.HasPrefix(line, "``` lenos-bash") {
			continue
		}
		if strings.HasPrefix(line, "```bash") || strings.HasPrefix(line, "``` bash") {
			continue
		}
		if strings.HasPrefix(line, "```") {
			break
		}
		return line
	}
	if len(lines) > 1 {
		return lines[1]
	}
	return ""
}

func parseDuration(lines []string) time.Duration {
	for _, line := range lines {
		if strings.HasPrefix(line, "*[") {
			return parseTrailerDuration(line)
		}
	}
	return 0
}

func parseTrailerDuration(line string) time.Duration {
	// Match duration after the comma in *[HH:MM:SS, Ns]* format.
	// The s we want is always after the comma, not the s in the timestamp.
	re := regexp.MustCompile(`,\s*(\d+(?:\.\d+)?)s\]`)
	matches := re.FindStringSubmatch(line)
	if len(matches) >= 2 {
		if secs, err := strconv.ParseFloat(matches[1], 64); err == nil {
			return time.Duration(secs * float64(time.Second))
		}
	}
	return 0
}

func countTurnEndsBefore(sections []section, idx int) int {
	count := 0
	for i := 0; i <= idx; i++ {
		if sections[i].typeHint == "turn_end" {
			count++
		}
	}
	return count
}

// Footer renders the 1-row footer line.
type Footer struct {
	deriv             FooterDerivation
	width             int
	styles            Styles
	lastBashWallclock time.Time
}

// NewFooter creates a footer with the given styles.
func NewFooter(styles Styles) *Footer {
	return &Footer{styles: styles, width: 100}
}

// Update sets the derivation and last bash wallclock.
func (f *Footer) Update(deriv FooterDerivation, lastBashWallclock time.Time) {
	f.deriv = deriv
	f.lastBashWallclock = lastBashWallclock
}

// SetWidth updates the terminal width.
func (f *Footer) SetWidth(w int) {
	f.width = w
}

// Render returns the 1-row footer string.
// wallclock is the time of the most recent unfinished bash block appearance
// (used to compute running duration in active state).
func (f *Footer) Render(now, wallclock time.Time) string {
	hints := "ctrl+g help  ctrl+c quit"

	var leftStr string
	var leftStyle lipgloss.Style
	switch f.deriv.State {
	case FooterStateActive:
		cmd := f.deriv.LatestBashCmd
		elapsed := int(now.Sub(wallclock).Round(time.Second).Seconds())
		leftStr = "agent working — " + cmd + " · running " + strconv.Itoa(elapsed) + "s"
		leftStyle = f.styles.FooterActive

	case FooterStateTurnEnded:
		dur := int(f.deriv.LastDuration.Seconds())
		leftStr = "turn " + strconv.Itoa(f.deriv.TurnNumber) + " ended · last cmd " +
			strconv.Itoa(dur) + "s"
		leftStyle = f.styles.FooterIdle

	case FooterStateIdle:
		dur := int(f.deriv.LastDuration.Seconds())
		if dur > 0 {
			leftStr = "idle · last cmd " + strconv.Itoa(dur) + "s"
		} else {
			leftStr = "idle"
		}
		leftStyle = f.styles.FooterIdle
	}

	// Truncate left text to fit available width.
	hintWidth := lipgloss.Width(hints)
	available := f.width - hintWidth
	if lipgloss.Width(leftStr) > available {
		leftStr = truncateToWidth(leftStr, available)
	}

	// Build the row with proper spacing.
	hintPart := f.styles.FooterHints.Render(hints)
	leftPart := leftStyle.Render(leftStr)
	spacer := strings.Repeat(" ", max(0, available-lipgloss.Width(leftStr)))

	return leftPart + spacer + hintPart
}

func truncateToWidth(s string, maxWidth int) string {
	for lipgloss.Width(s) > maxWidth && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// TickMsg is a periodic message Bubble Tea uses to redraw the elapsed counter.
type TickMsg time.Time

// Tick returns a tea.Cmd that emits TickMsg after 1s.
func Tick() tea.Cmd {
	return func() tea.Msg {
		return TickMsg(time.Now())
	}
}

// BottomBar renders the queue indicator (compact) and full queue items list
// (expanded) below the viewport. Hidden entirely when the queue is empty.
type BottomBar struct {
	queueDepth int
	queueItems []string
	width      int
	tuiStyles  Styles
	pillStyles *uistyles.Styles
	compact    bool
}

// NewBottomBar constructs a BottomBar in compact mode.
func NewBottomBar(tuiStyles Styles, pillStyles *uistyles.Styles) *BottomBar {
	return &BottomBar{
		tuiStyles:  tuiStyles,
		pillStyles: pillStyles,
		compact:    true,
	}
}

// Toggle flips the bar between compact and expanded.
func (b *BottomBar) Toggle() { b.compact = !b.compact }

// IsCompact reports whether the bar is in compact mode.
func (b *BottomBar) IsCompact() bool { return b.compact }

// SetWidth sets the available width for truncation.
func (b *BottomBar) SetWidth(w int) { b.width = w }

// SetQueue updates the queue depth and items list.
func (b *BottomBar) SetQueue(depth int, items []string) {
	b.queueDepth = depth
	b.queueItems = items
}

// Render returns "" when queueDepth is 0, a 1-row pill when compact, and a
// pill plus item list when expanded.
func (b *BottomBar) Render() string {
	if b.queueDepth <= 0 {
		return ""
	}

	row := b.compactRow()
	if b.compact {
		return row
	}

	list := queueList(b.queueItems, b.pillStyles)
	if list == "" {
		return row
	}
	return lipgloss.JoinVertical(lipgloss.Left, row, list)
}

func (b *BottomBar) compactRow() string {
	pill := queuePill(b.queueDepth, b.compact /*focused*/, !b.compact /*panelFocused*/, b.pillStyles)
	hint := b.tuiStyles.KeystrokeTip.Render(fmt.Sprintf("ctrl+t %s", b.toggleHint()))

	row := pill + " " + hint
	if b.width > 0 && lipgloss.Width(row) > b.width {
		row = truncateToWidth(row, b.width)
	}
	return row
}

func (b *BottomBar) toggleHint() string {
	if b.compact {
		return "open"
	}
	return "close"
}
