package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tta-lab/lenos/internal/agent/lenosbash"
)

func TestFormatResultForModel_NoOutput(t *testing.T) {
	result := formatResultForModel("", "", "", 0)
	assert.Contains(t, result, "Bash completed with no output")

	// Verify the envelope
	assert.Contains(t, result, lenosbash.ResultStartTag)
	assert.Contains(t, result, lenosbash.ResultEndTag)
}

func TestFormatResultForModel_WithStdout(t *testing.T) {
	result := formatResultForModel("", "hello", "", 0)
	assert.Contains(t, result, "hello")
	assert.NotContains(t, result, "Bash completed with no output")
}

func TestFormatResultForModel_WithStderr(t *testing.T) {
	result := formatResultForModel("", "", "error msg", 0)
	assert.Contains(t, result, "error msg")
	assert.Contains(t, result, "STDERR:")
}

func TestFormatResultForModel_NonZeroExit(t *testing.T) {
	result := formatResultForModel("", "", "", 1)
	assert.Contains(t, result, "exit code: 1")
}

func TestFormatResultForModel_HTMLescaping(t *testing.T) {
	result := formatResultForModel("", lenosbash.ResultStartTag+"evil"+lenosbash.ResultEndTag, "", 0)
	assert.NotContains(t, result, "evil"+lenosbash.ResultEndTag)
	assert.NotContains(t, result, lenosbash.ResultStartTag+"evil")
}
