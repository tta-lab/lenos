package diffview

import (
	"charm.land/lipgloss/v2"
)

// LineStyle defines the styles for a given line type in the diff view.
type LineStyle struct {
	LineNumber lipgloss.Style
	Symbol     lipgloss.Style
	Code       lipgloss.Style
}

// Style defines the overall style for the diff view, including styles for
// different line types such as divider, missing, equal, insert, and delete
// lines.
type Style struct {
	DividerLine LineStyle
	MissingLine LineStyle
	EqualLine   LineStyle
	InsertLine  LineStyle
	DeleteLine  LineStyle
}

// DefaultLightStyle provides a default light theme style for the diff view.
func DefaultLightStyle() Style {
	return Style{
		DividerLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#4a3e38")).
				Background(lipgloss.Color("#2a1e28")),
			Code: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#a89d8a")).
				Background(lipgloss.Color("#2a2e28")),
		},
		MissingLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Background(lipgloss.Color("#e8dcc8")),
			Code: lipgloss.NewStyle().
				Background(lipgloss.Color("#e8dcc8")),
		},
		EqualLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#3a2e28")).
				Background(lipgloss.Color("#e8dcc8")),
			Code: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#1a1016")).
				Background(lipgloss.Color("#e8dcc8")),
		},
		InsertLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#5a7a5a")).
				Background(lipgloss.Color("#c8e6c9")),
			Symbol: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#5a7a5a")).
				Background(lipgloss.Color("#e8f5e9")),
			Code: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#1a1016")).
				Background(lipgloss.Color("#e8f5e9")),
		},
		DeleteLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#c4647a")).
				Background(lipgloss.Color("#ffcdd2")),
			Symbol: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#c4647a")).
				Background(lipgloss.Color("#ffebee")),
			Code: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#1a1016")).
				Background(lipgloss.Color("#ffebee")),
		},
	}
}

// DefaultDarkStyle provides a default dark theme style for the diff view.
func DefaultDarkStyle() Style {
	return Style{
		DividerLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#a89d8a")).
				Background(lipgloss.Color("#4a5a7a")),
			Code: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#a89d8a")).
				Background(lipgloss.Color("#3a2e28")),
		},
		MissingLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Background(lipgloss.Color("#3a2e28")),
			Code: lipgloss.NewStyle().
				Background(lipgloss.Color("#3a2e28")),
		},
		EqualLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#e8dcc8")).
				Background(lipgloss.Color("#3a2e28")),
			Code: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#e8dcc8")).
				Background(lipgloss.Color("#1a1016")),
		},
		InsertLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#5a7a5a")).
				Background(lipgloss.Color("#293229")),
			Symbol: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#5a7a5a")).
				Background(lipgloss.Color("#303a30")),
			Code: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#e8dcc8")).
				Background(lipgloss.Color("#303a30")),
		},
		DeleteLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#c4647a")).
				Background(lipgloss.Color("#332929")),
			Symbol: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#c4647a")).
				Background(lipgloss.Color("#3a3030")),
			Code: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#e8dcc8")).
				Background(lipgloss.Color("#3a3030")),
		},
	}
}
