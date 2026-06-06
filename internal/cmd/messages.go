package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/db"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/session"
)

var messagesCmd = &cobra.Command{
	Use:   "messages",
	Short: "Show recent user messages for the current session",
	Long: `Show user messages for the session identified by the LENOS_SESSION_ID
environment variable.

The agent uses this after a Compact Session to recover the last few user
messages that were cleared from the context window.

Examples:
  lenos messages              # all user messages (excluding summaries)
  lenos messages --tail 3     # last 3 user messages`,
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := os.Getenv("LENOS_SESSION_ID")
		if sessionID == "" {
			return fmt.Errorf("LENOS_SESSION_ID not set; run this from within a lenos agent session")
		}

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get cwd: %w", err)
		}

		cfg, err := config.Init(cwd, "", false)
		if err != nil {
			return fmt.Errorf("init config: %w", err)
		}
		dataDir := cfg.Config().Options.DataDirectory

		conn, err := db.Connect(cmd.Context(), dataDir)
		if err != nil {
			return fmt.Errorf("connect db: %w", err)
		}
		defer conn.Close()

		q := db.New(conn)
		svc := session.NewService(q, conn)
		msgSvc := message.NewService(q)

		sess, err := svc.Get(cmd.Context(), sessionID)
		if err != nil {
			return fmt.Errorf("session not found: %w", err)
		}

		allMsgs, err := msgSvc.List(cmd.Context(), sess.ID)
		if err != nil {
			return fmt.Errorf("list messages: %w", err)
		}

		// Filter to user messages only (non-summary), extract plain text.
		var userMsgs []string
		for _, m := range allMsgs {
			if m.Role != message.User || (m.IsSummaryMessage) {
				continue
			}
			text := plainTextParts(m.Parts)
			if text != "" {
				userMsgs = append(userMsgs, text)
			}
		}

		tailN, _ := cmd.Flags().GetInt("tail")
		if tailN > 0 && len(userMsgs) > tailN {
			userMsgs = userMsgs[len(userMsgs)-tailN:]
		}

		for i, msg := range userMsgs {
			if i > 0 {
				fmt.Println(strings.Repeat("-", 60))
			}
			fmt.Println(msg)
		}
		return nil
	},
}

func plainTextParts(parts []message.ContentPart) string {
	var sb strings.Builder
	for _, p := range parts {
		if tc, ok := p.(message.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func init() {
	messagesCmd.Flags().Int("tail", 0, "Show only the last N user messages")
	messagesCmd.SetContext(rootCmd.Context())
	rootCmd.AddCommand(messagesCmd)
}
