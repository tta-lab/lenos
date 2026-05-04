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
prompt (env + output protocol + few-shot examples + available commands), the
git section (status + attribution), and the coder post-template.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ws, cleanup, err := setupWorkspace(cmd, "", nil, false)
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
