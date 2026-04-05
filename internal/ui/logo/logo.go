// Package logo renders a Lenos wordmark in a stylized way.
package logo

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

// Opts are the options for rendering the Lenos title art.
type Opts struct {
	FieldColor   color.Color // diagonal lines
	TitleColorA  color.Color // left gradient ramp point
	TitleColorB  color.Color // right gradient ramp point
	BrandColor   color.Color // lenos text color
	VersionColor color.Color // Version text color
	Width        int         // width of the rendered logo, used for truncation
}

// Render renders the Lenos logo. Set the argument to true to render the narrow
// version, intended for use in a sidebar.
//
// The compact argument determines whether it renders compact for the sidebar
// or wider for the main pane.
func Render(s *styles.Styles, version string, compact bool, o Opts) string {
	fg := func(c color.Color, text string) string {
		return lipgloss.NewStyle().Foreground(c).Render(text)
	}

	// Render "lenos" with gradient.
	lenos := fg(o.TitleColorA, "lenos")
	lenosWidth := lipgloss.Width(lenos)

	// Version row.
	version = ansi.Truncate(version, max(0, o.Width-lenosWidth-1), "…")
	gap := max(0, o.Width-lenosWidth-lipgloss.Width(version)-1)
	brandRow := fg(o.BrandColor, "lenos") + strings.Repeat(" ", gap) + fg(o.VersionColor, version)

	// Join the brand/version row and lenos title.
	result := brandRow + "\n" + lenos

	if o.Width > 0 {
		lines := strings.Split(result, "\n")
		for i, line := range lines {
			lines[i] = ansi.Truncate(line, o.Width, "")
		}
		result = strings.Join(lines, "\n")
	}
	return result
}

// SmallRender renders a smaller version of the Lenos logo, suitable for
// smaller windows or sidebar usage.
func SmallRender(t *styles.Styles, width int) string {
	title := t.Base.Foreground(t.Secondary).Render("lenos")
	title = title + " " + styles.ApplyBoldForegroundGrad(t, "", t.Secondary, t.Primary)
	remainingWidth := width - lipgloss.Width(title) - 1
	if remainingWidth > 0 {
		lines := strings.Repeat("╱", remainingWidth)
		title = title + " " + t.Base.Foreground(t.Primary).Render(lines)
	}
	return title
}
