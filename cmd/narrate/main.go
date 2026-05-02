// Package main is the lenos `narrate` CLI binary. Bash subprocesses inside a
// lenos agent session invoke it as `narrate "<text>"` (or `cmd | narrate`)
// to append human-readable prose to the session's .md transcript.
//
// Reads LENOS_SESSION_ID (required); the data directory is auto-discovered
// via the same fsext.LookupClosest walk-up from cwd that lenos itself uses.
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

Reads LENOS_SESSION_ID (required) and finds the session .md by walking up
from the current working directory to the closest .lenos/ directory —
the same resolution lenos itself uses, so the two always agree.

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
		fmt.Fprintln(cmd.OutOrStdout(), "narrate written")
		return nil
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "narrate: %s\n", err)
		os.Exit(1)
	}
}
