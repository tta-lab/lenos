package cmd

import (
	"errors"
	"os"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/spf13/cobra"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/event"
	"github.com/tta-lab/lenos/internal/ui/common"
	ui "github.com/tta-lab/lenos/internal/ui/model"
)

const reviewTriggerPrompt = `Review the current branch against the default branch.

Use only local git state. Do not run git fetch, git pull, git checkout, or any network operation.

Determine the default branch from local refs/config when available, preferring origin/HEAD if it exists. If the default branch cannot be determined, check common local branch names such as main and master and state the assumption. Find the merge base between HEAD and the default branch, inspect the diff from that merge base to HEAD, then provide prioritized findings.`

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Review the current branch against the default branch",
	Long: `Review opens an interactive read-only reviewer session.

The reviewer inspects local git state only, compares the current branch against
the default branch, and reports prioritized findings.`,
	Example: `
# Review the current branch interactively
lenos review

# Review with a one-off model override
lenos review -m gpt-5
  `,
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID, _ := cmd.Flags().GetString("session")
		continueLast, _ := cmd.Flags().GetBool("continue")
		ws, cleanup, err := setupWorkspaceWithProgressBar(cmd, config.AgentReviewer, nil, true)
		if err != nil {
			return err
		}
		defer cleanup()

		if sessionID != "" {
			sess, err := resolveWorkspaceSessionID(cmd.Context(), ws, sessionID)
			if err != nil {
				return err
			}
			sessionID = sess.ID
		} else if continueLast {
			sess, ok, err := resolveWorkspaceContinueSession(cmd.Context(), ws)
			if err != nil {
				return err
			}
			if ok {
				sessionID = sess.ID
				continueLast = false
			}
		}

		event.AppInitialized()

		com := common.DefaultCommon(ws)
		model := ui.New(com, sessionID, continueLast, reviewTriggerPrompt)

		var env uv.Environ = os.Environ()
		program := tea.NewProgram(
			model,
			tea.WithEnvironment(env),
			tea.WithContext(cmd.Context()),
			tea.WithFilter(ui.MouseEventFilter),
		)
		go ws.Subscribe(program)

		if _, err := program.Run(); err != nil {
			event.Error(err)
			return errors.New("lenos crashed during review")
		}
		activeSessionID := model.ActiveSessionID()
		if hint := formatResumeHint(activeSessionID); hint != "" {
			cmd.Println(hint)
		}
		if hint := formatJournalHint(ws.WorkingDir(), activeSessionID); hint != "" {
			cmd.Println(hint)
		}
		return nil
	},
}

func init() {
	reviewCmd.Flags().StringP("model", "m", "", "Model to use. Accepts 'model' or 'provider/model' to disambiguate models with the same name across providers")
	reviewCmd.Flags().Bool("small-model", false, "Use the small-tier model for this session")
	reviewCmd.Flags().String("reasoning-effort", "", "Reasoning effort for this session: medium, high, or xhigh")
	reviewCmd.Flags().StringP("session", "s", "", "Continue a previous session by ID")
	reviewCmd.Flags().BoolP("continue", "C", false, "Continue the most recent session")
	reviewCmd.MarkFlagsMutuallyExclusive("session", "continue")
}
