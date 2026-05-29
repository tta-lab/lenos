package messageblock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type fixture struct {
	Name      string           `json:"name"`
	Source    string           `json:"source"`
	Messages  []fixtureMessage `json:"messages"`
	Bash      string           `json:"bash"`
	HasBash   bool             `json:"has_bash"`
	Lifecycle string           `json:"lifecycle"`
	Error     *fixtureError    `json:"error"`
}

type fixtureMessage struct {
	Target string `json:"target,omitempty"`
	Body   string `json:"body"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Offset int    `json:"offset"`
}

type fixtureError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
	Offset     int    `json:"offset"`
	Incomplete bool   `json:"incomplete,omitempty"`
}

func TestMessageBlockFixtureCorpus(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "fixtures.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var fixtures []fixture
	require.NoError(t, json.Unmarshal(data, &fixtures))
	require.NotEmpty(t, fixtures)

	seen := make(map[string]bool, len(fixtures))
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			require.NotEmpty(t, fixture.Name)
			require.False(t, seen[fixture.Name], "duplicate fixture name")
			seen[fixture.Name] = true
			require.NotEmpty(t, fixture.Source)
			require.Contains(t, map[string]bool{
				"continue":     true,
				"message-only": true,
				"parse-error":  true,
			}, fixture.Lifecycle)

			if fixture.Error != nil {
				require.Equal(t, "parse-error", fixture.Lifecycle)
				require.NotEmpty(t, fixture.Error.Code)
				require.NotEmpty(t, fixture.Error.Message)
				require.Positive(t, fixture.Error.Line)
				require.Positive(t, fixture.Error.Column)
				return
			}

			for _, message := range fixture.Messages {
				require.NotEmpty(t, message.Body)
				require.Positive(t, message.Line)
				require.Positive(t, message.Column)
			}

			if fixture.HasBash {
				require.NotEmpty(t, fixture.Bash)
				require.Equal(t, "continue", fixture.Lifecycle)
			} else {
				require.Empty(t, fixture.Bash)
				require.Equal(t, "message-only", fixture.Lifecycle)
			}
		})
	}

	requireFixtureNames(t, seen, []string{
		"valid-basic-message",
		"valid-multiline-message",
		"valid-raw-quotes",
		"valid-raw-delimiter-candidate",
		"valid-addressed-message",
		"valid-mixed-bash-and-message",
		"valid-indented-top-level-message",
		"valid-heredoc-literal-message-looking-text",
		"invalid-raw-prose-before-bash",
		"invalid-fenced-bash",
		"invalid-same-line-semicolon",
		"invalid-same-line-background",
		"invalid-same-line-pipeline",
		"invalid-same-line-heredoc-setup",
		"invalid-message-inside-if",
		"invalid-message-inside-loop",
		"invalid-message-inside-function",
		"invalid-message-inside-command-substitution",
		"invalid-message-inside-string",
		"invalid-message-inside-comment",
		"invalid-message-as-command-word",
		"invalid-unterminated-message",
		"invalid-mismatched-hash-count",
		"invalid-target-characters",
		"invalid-mc-variant",
	})
}

func requireFixtureNames(t *testing.T, seen map[string]bool, names []string) {
	t.Helper()

	for _, name := range names {
		require.True(t, seen[name], "missing fixture %q", name)
	}
}
