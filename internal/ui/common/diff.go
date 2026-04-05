package common

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/tta-lab/lenos/internal/ui/diffview"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

// DiffFormatter returns a diff formatter with the given styles that can be
// used to format diff outputs.
func DiffFormatter(s *styles.Styles) *diffview.DiffView {
	formatDiff := diffview.New()
	style := chroma.MustNewStyle("lenos", s.ChromaTheme())
	diff := formatDiff.ChromaStyle(style).Style(s.Diff).TabWidth(4)
	return diff
}
