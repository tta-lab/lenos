package agent

import (
	"bytes"
	"context"
	"log/slog"
	"regexp"
	"strings"

	"charm.land/fantasy"
	"github.com/tta-lab/lenos/internal/message"

	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
)

func assistantTextMessage(text string, rc message.ReasoningContent) fantasy.Message {
	var parts []fantasy.MessagePart
	if rc.Thinking != "" {
		rp := fantasy.ReasoningPart{Text: rc.Thinking, ProviderOptions: fantasy.ProviderOptions{}}
		if rc.Signature != "" {
			rp.ProviderOptions[anthropic.Name] = &anthropic.ReasoningOptionMetadata{Signature: rc.Signature}
		}
		if rc.ResponsesData != nil {
			rp.ProviderOptions[openai.Name] = rc.ResponsesData
		}
		if rc.ThoughtSignature != "" {
			rp.ProviderOptions[google.Name] = &google.ReasoningMetadata{
				Signature: rc.ThoughtSignature,
				ToolID:    rc.ToolID,
			}
		}
		parts = append(parts, rp)
	}
	if t := strings.TrimSpace(text); t != "" {
		parts = append(parts, fantasy.TextPart{Text: t})
	}
	return fantasy.Message{Role: fantasy.MessageRoleAssistant, Content: parts}
}

func persistObservation(ctx context.Context, deps loopDeps, obs string) error {
	_, err := deps.messages.Create(ctx, deps.sessionID, message.CreateMessageParams{
		Role:  message.Runtime,
		Parts: []message.ContentPart{message.TextContent{Text: obs}},
	})
	return err
}

func turnPromptMessage(prompt turnPrompt) fantasy.Message {
	if prompt.Role == message.Runtime {
		return fantasy.NewUserMessage(message.RuntimeText(prompt.Text))
	}
	return fantasy.NewUserMessage(prompt.Text)
}

func markStepFinished(ctx context.Context, deps loopDeps, msg *message.Message, reason message.FinishReason) {
	if msg.IsFinished() {
		return
	}
	msg.AddFinish(reason, "", "")
	if err := deps.messages.Update(ctx, *msg); err != nil {
		slog.Warn("loop: persist step finish", "error", err)
	}
}

func abandonPending(ctx context.Context, msgs message.Service, m *message.Message) {
	exitCode := -1
	cmd := ""
	if cc := m.CommandContent(); cc.Command != "" {
		cmd = cc.Command
	}
	m.Parts = []message.ContentPart{message.CommandContent{
		Command: cmd, Output: "canceled before result", ExitCode: &exitCode, Pending: false,
	}}
	if err := msgs.Update(ctx, *m); err != nil {
		slog.Warn("loop: abandon pending result", "error", err)
	}
}

func combine(stdout, stderr []byte) []byte {
	if len(stderr) == 0 {
		return stdout
	}
	if len(stdout) == 0 {
		return stderr
	}
	var buf bytes.Buffer
	buf.Write(stdout)
	if !bytes.HasSuffix(stdout, []byte("\n")) {
		buf.WriteByte('\n')
	}
	buf.Write(stderr)
	return buf.Bytes()
}

var cmdNotFoundRe = regexp.MustCompile(`(?m)^bash:(?: line \d+:)? (\S+): command not found$`)

func scanFirstCmdNotFound(stderr string) string {
	m := cmdNotFoundRe.FindStringSubmatch(stderr)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}
