// Package main is the lenos `narrate` CLI binary. Bash subprocesses inside a
// lenos agent session invoke it as `narrate "<text>"` (or `cmd | narrate`)
// to append human-readable prose to the session's .md transcript.
//
// Reads LENOS_SESSION_ID (required); the data directory defaults to
// <cwd>/.lenos and can be overridden via LENOS_DATA_DIR.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tta-lab/lenos/internal/transcript"
)

var rootCmd = &cobra.Command{
	Use:   "narrate [text]",
	Short: "Append prose to the current lenos session's .md transcript",
	Long: `narrate appends prose to the current lenos session's .md transcript.

Resolves the target path as ${LENOS_DATA_DIR:-$(pwd)/.lenos}/sessions/${LENOS_SESSION_ID}.md.
LENOS_SESSION_ID is required; LENOS_DATA_DIR is optional and defaults to
the .lenos directory in the current working directory.

Examples:
  narrate "switching approach"
  git diff | narrate
  cat <<'EOF' | narrate
  multi-line message
  with multiple lines
  EOF

For visual emphasis, emit markdown directly:
  narrate <<<'> ⚠️ deprecated config detected'`,
	Args:          cobra.ArbitraryArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := resolveSessionPath()
		if err != nil {
			return err
		}
		text, err := resolveInput(args, cmd.InOrStdin())
		if err != nil {
			return err
		}
		rendered := transcript.RenderProse(text)
		w := transcript.NewMdWriter(path)
		if err := appendWithRetry(w, []byte(rendered)); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		return nil
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "narrate: %s\n", err)
		os.Exit(1)
	}
}
