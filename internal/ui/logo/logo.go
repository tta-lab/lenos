// Package logo renders a Lenos wordmark in a stylized way.
package logo

import (
	"image/color"

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

	// Render "Lenos" with version.
	lenosText := fg(o.BrandColor, "Lenos")
	lenosWidth := lipgloss.Width(lenosText)

	// Truncate version if needed.
	version = ansi.Truncate(version, max(0, o.Width-lenosWidth-1), "…")
	versionText := fg(o.VersionColor, " "+version)

	result := lenosText + versionText

	if o.Width > 0 {
		result = ansi.Truncate(result, o.Width, "")
	}
	return result
}

// SmallRender renders a smaller version of the Lenos logo, suitable for
// smaller windows or sidebar usage.
func SmallRender(t *styles.Styles, width int) string {
	title := t.Header.Brand.Render("Lenos")
	title = ansi.Truncate(title, width, "")
	return title
}
