// Package main is the lenos `narrate` CLI binary. Bash subprocesses inside a
// lenos agent session invoke it as `narrate "<text>"` (or `cmd | narrate`)
// to append human-readable prose to the session's .md transcript.
//
// Reads LENOS_SESSION_ID + LENOS_DATA_DIR; failure modes per E14 of
// flicknote 30666153 (clear stderr message + non-zero exit).
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "narrate [text]",
	Short: "Append prose to the current lenos session's .md transcript",
	Long: `narrate appends prose to the current lenos session's .md transcript.

Reads LENOS_SESSION_ID and LENOS_DATA_DIR from environment to derive the
target path: ${LENOS_DATA_DIR}/sessions/${LENOS_SESSION_ID}.md. Both env
vars are required.

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
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "narrate: %s\n", err)
		os.Exit(1)
	}
}
