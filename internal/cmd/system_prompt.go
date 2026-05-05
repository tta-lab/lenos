package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tta-lab/lenos/internal/workspace"
)

// systemPromptCmd dumps the fully-resolved system prompt currently sent to
// the model. Useful for verifying the bash-first protocol + few-shot
// examples are actually reaching the LLM.
var systemPromptCmd = &cobra.Command{
	Use:   "system-prompt",
	Short: "Print the fully-resolved system prompt sent to the model",
	Long: `system-prompt prints the system prompt that the agent coordinator currently
pushes onto the model on every turn. Concatenates the bash-first base
prompt, git status/attribution, and the identity wrapper (universal rules +
identity body + memory tails).

Flags:
  --agent, -a      Agent identity file name (e.g. coder, pr-review-lead).
                   Defaults to "coder". The agent body is injected into the
                   identity slot at the prompt top — NOT in <memory>.
  --context-file, -f  Extra context file (repeatable). Injected into the
                   <memory> block at the prompt tail.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		agentName, _ := cmd.Flags().GetString("agent")
		contextFiles, _ := cmd.Flags().GetStringArray("context-file")
		ws, cleanup, err := setupWorkspace(cmd, agentName, contextFiles, false)
		if err != nil {
			return err
		}
		defer cleanup()

		appWs, ok := ws.(*workspace.AppWorkspace)
		if !ok {
			return fmt.Errorf("internal: workspace is not AppWorkspace")
		}
		coord := appWs.App().AgentCoordinator
		if coord == nil {
			return fmt.Errorf("agent coordinator not initialized — check provider configuration")
		}
		fmt.Fprint(cmd.OutOrStdout(), coord.SystemPrompt())
		return nil
	},
}

func init() {
	systemPromptCmd.Flags().StringP("agent", "a", "", "Agent identity file name (e.g. coder, pr-review-lead)")
	systemPromptCmd.Flags().StringArrayP("context-file", "f", nil, "Extra context file (repeatable)")
}
