package lenosbash

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderDiagnosticTagUnclosed(t *testing.T) {
	t.Parallel()

	source := "Let me check.\n" + BashStartTag + "\nls -la\n"
	rendered := RenderDiagnostic(source, Diagnostic{
		Kind:       "tag_unclosed",
		Message:    "unclosed " + BashStartTag + " tag at end of response",
		Line:       3,
		Column:     7,
		Offset:     28,
		Incomplete: true,
	})

	assert.Contains(t, rendered, RuntimeTag+"\ninvalid Lenos Run")
	assert.Contains(t, rendered, "error: unclosed "+BashStartTag+" tag at end of response")
	assert.Contains(t, rendered, BashBlock("your command here"))
}

func TestRenderDiagnosticWithoutPosition(t *testing.T) {
	t.Parallel()

	rendered := RenderDiagnostic("", Diagnostic{
		Kind:    "tag_unclosed",
		Message: "unclosed " + BashStartTag + " tag",
	})

	assert.Contains(t, rendered, "error: unclosed "+BashStartTag+" tag")
}
