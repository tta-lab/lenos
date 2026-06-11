package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"charm.land/log/v2"
	"github.com/spf13/cobra"
	"github.com/tta-lab/lenos/internal/event"
	"github.com/tta-lab/lenos/internal/workspace"
)

var runCmd = &cobra.Command{
	Aliases: []string{"r"},
	Use:     "run [prompt...]",
	Short:   "Run a single non-interactive prompt",
	Long: `Run a single prompt in non-interactive mode and exit.
The prompt can be provided as arguments or piped from stdin.`,
	Example: `
# Run a simple prompt
lenos run "Guess my 5 favorite Pokémon"

# Pipe input from stdin
curl https://charm.land | lenos run "Summarize this website"

# Read from a file
lenos run "What is this code doing?" <<< prrr.go

# Redirect output to a file
lenos run "Generate a hot README for this project" > MY_HOT_README.md

# Run in quiet mode (hide the spinner)
lenos run --quiet "Generate a README for this project"

# Run in verbose mode (show logs)
lenos run --verbose "Generate a README for this project"

# Continue a previous session
lenos run --session {session-id} "Follow up on your last response"

# Continue the most recent session
lenos run --continue "Follow up on your last response"

# Run an agent in read-only mode (sandbox blocks any writes to cwd)
lenos run --readonly --agent reviewer "review the changes in HEAD"

  `,
	RunE: func(cmd *cobra.Command, args []string) error {
		var (
			quiet, _          = cmd.Flags().GetBool("quiet")
			verbose, _        = cmd.Flags().GetBool("verbose")
			sessionID, _      = cmd.Flags().GetString("session")
			useLast, _        = cmd.Flags().GetBool("continue")
			usageJSON, _      = cmd.Flags().GetString("usage-json")
			trajectoryJSON, _ = cmd.Flags().GetString("trajectory-json")
		)

		// Cancel on SIGINT or SIGTERM.
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		prompt := strings.Join(args, " ")

		prompt, err := MaybePrependStdin(prompt)
		if err != nil {
			slog.Error("Failed to read from stdin", "error", err)
			return err
		}

		if prompt == "" {
			return fmt.Errorf("no prompt provided")
		}

		// Reject goal flags when continuing an existing session.
		goalText, _ := cmd.Flags().GetString("goal")
		goalFile, _ := cmd.Flags().GetString("goal-file")
		if (goalText != "" || goalFile != "") && (sessionID != "" || useLast) {
			return fmt.Errorf("--goal and --goal-file cannot be used with --session or --continue; goals are session-start contracts")
		}

		event.SetNonInteractive(true)
		event.AppInitialized()

		switch {
		case sessionID != "":
			event.SetContinueBySessionID(true)
		case useLast:
			event.SetContinueLastSession(true)
		}

		agentName, _ := cmd.Flags().GetString("agent")
		contextFiles, _ := cmd.Flags().GetStringArray("context-file")
		readOnly, _ := cmd.Flags().GetBool("readonly")

		ws, cleanup, err := setupWorkspace(cmd, agentName, contextFiles, readOnly)
		if err != nil {
			return err
		}
		defer cleanup()

		if !ws.Config().IsConfigured() {
			return fmt.Errorf("no providers configured - please run 'lenos' to set up a provider interactively")
		}

		appWs := ws.(*workspace.AppWorkspace)
		if sessionID != "" {
			sess, err := resolveWorkspaceSessionID(ctx, ws, sessionID)
			if err != nil {
				return err
			}
			sessionID = sess.ID
		}

		if verbose {
			slog.SetDefault(slog.New(log.New(os.Stderr)))
		}

		return appWs.App().RunNonInteractive(ctx, os.Stdout, prompt, quiet || verbose, sessionID, useLast, usageJSON, trajectoryJSON)
	},
}

func init() {
	runCmd.Flags().BoolP("quiet", "q", false, "Hide spinner")
	runCmd.Flags().BoolP("verbose", "v", false, "Show logs")
	runCmd.Flags().StringP("model", "m", "", "Model to use. Accepts 'model' or 'provider/model' to disambiguate models with the same name across providers")
	runCmd.Flags().Bool("small-model", false, "Use the small-tier model for this session")
	runCmd.Flags().String("reasoning-effort", "", "Reasoning effort for this session: medium, high, or xhigh")
	runCmd.Flags().StringP("session", "s", "", "Continue a previous session by ID")
	runCmd.Flags().BoolP("continue", "C", false, "Continue the most recent session")
	runCmd.Flags().StringP("agent", "a", "", "Agent identity file name (e.g. coder) to inject as context")
	runCmd.Flags().StringArrayP("context-file", "f", nil, "Extra context file to inject at startup (repeatable)")
	runCmd.Flags().String("pair-with", "", "Default target for untargeted message blocks")
	runCmd.Flags().String("usage-json", "", "Write final usage summary JSON to path")
	runCmd.Flags().String("trajectory-json", "", "Write incremental ATIF trajectory JSON to path")
	runCmd.Flags().Bool("readonly", false, "Enforce read-only filesystem access on the working directory via the temenos sandbox; agent cannot create or modify files in cwd.")
	runCmd.Flags().Bool("no-sandbox", false, "Disable temenos sandbox isolation and run commands directly on the host.")
	runCmd.Flags().String("goal", "", "Write a goal file with this Markdown body to gate session exit")
	runCmd.Flags().String("goal-file", "", "Copy the provided goal file into .lenos/goals/<session-id>.md to gate session exit")
	runCmd.MarkFlagsMutuallyExclusive("session", "continue")
	runCmd.MarkFlagsMutuallyExclusive("goal", "goal-file")
}
