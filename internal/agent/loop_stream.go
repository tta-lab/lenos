package agent

import (
	"context"
	"log/slog"
	"strings"

	"charm.land/fantasy"
	"github.com/tta-lab/lenos/internal/agent/codex"
	"github.com/tta-lab/lenos/internal/agent/lenosbash"
	"github.com/tta-lab/lenos/internal/message"

	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
)

// streamOne pumps a single model stream into assistantMsg.
func streamOne(
	ctx context.Context,
	deps loopDeps,
	msgs []fantasy.Message,
	assistantMsg *message.Message,
) (string, fantasy.Usage, fantasy.ProviderMetadata, error) {
	// Attach session identity to context for transport-layer headers
	// (prompt cache key, client request ID).
	streamCtx := ctx
	if deps.sessionID != "" {
		streamCtx = codex.WithSessionID(ctx, deps.sessionID)
	}

	baseline := assistantMsg.Clone()
	result, err := retryModelStream(streamCtx,
		func() (streamOneResult, error) {
			return streamOneAttempt(streamCtx, deps, msgs, assistantMsg)
		},
		func() {
			resetMessageForStreamRetry(streamCtx, deps.messages, assistantMsg, baseline, "loop: reset assistant message for stream retry")
		},
	)
	if err != nil {
		return "", result.usage, result.meta, err
	}
	return result.emit, result.usage, result.meta, nil
}

type streamOneResult struct {
	emit  string
	usage fantasy.Usage
	meta  fantasy.ProviderMetadata
}

func streamOneAttempt(
	ctx context.Context,
	deps loopDeps,
	msgs []fantasy.Message,
	assistantMsg *message.Message,
) (streamOneResult, error) {
	call := fantasy.Call{
		Prompt:          msgs,
		ProviderOptions: deps.provOpts,
		UserAgent:       userAgent,
	}
	stream, err := deps.model.Model.Stream(ctx, call)
	if err != nil {
		return streamOneResult{}, err
	}
	var (
		rawText strings.Builder
		usage   fantasy.Usage
		meta    fantasy.ProviderMetadata
	)
	for part := range stream {
		switch part.Type {
		case fantasy.StreamPartTypeTextDelta:
			rawText.WriteString(part.Delta)
			if rc := assistantMsg.ReasoningContent(); rc.Thinking != "" && rc.FinishedAt == 0 {
				assistantMsg.FinishThinking()
			}
			assistantMsg.AppendContent(part.Delta)
			trimAssistantPostBashTail(assistantMsg)
			if uerr := deps.messages.Update(ctx, *assistantMsg); uerr != nil {
				slog.Warn("loop: persist text delta", "error", uerr)
			}
		case fantasy.StreamPartTypeReasoningDelta:
			assistantMsg.AppendReasoningContent(part.Delta)
			if uerr := deps.messages.Update(ctx, *assistantMsg); uerr != nil {
				slog.Warn("loop: persist reasoning delta", "error", uerr)
			}
		case fantasy.StreamPartTypeReasoningEnd:
			if anthropicData, ok := part.ProviderMetadata[anthropic.Name]; ok {
				if sig, ok := anthropicData.(*anthropic.ReasoningOptionMetadata); ok && sig.Signature != "" {
					assistantMsg.AppendReasoningSignature(sig.Signature)
				}
			}
			if openaiData, ok := part.ProviderMetadata[openai.Name]; ok {
				if rd, ok := openaiData.(*openai.ResponsesReasoningMetadata); ok {
					assistantMsg.SetReasoningResponsesData(rd)
				}
			}
			if googleData, ok := part.ProviderMetadata[google.Name]; ok {
				if rd, ok := googleData.(*google.ReasoningMetadata); ok && rd.Signature != "" {
					assistantMsg.AppendThoughtSignature(rd.Signature, rd.ToolID)
				}
			}
			assistantMsg.FinishThinking()
			if uerr := deps.messages.Update(ctx, *assistantMsg); uerr != nil {
				slog.Warn("loop: persist reasoning end", "error", uerr)
			}
		case fantasy.StreamPartTypeFinish:
			usage = part.Usage
			meta = part.ProviderMetadata
		case fantasy.StreamPartTypeError:
			return streamOneResult{usage: usage, meta: meta}, part.Error
		}
	}
	return streamOneResult{
		emit:  rawText.String(),
		usage: usage,
		meta:  meta,
	}, nil
}

func trimAssistantPostBashTail(assistantMsg *message.Message) {
	parsed, diag := lenosbash.Parse(assistantMsg.Content().Text)
	if diag != nil || len(parsed.Bash) == 0 || parsed.Accepted == "" || parsed.Accepted == assistantMsg.Content().Text {
		return
	}
	replaceAssistantText(assistantMsg, parsed.Accepted)
}

func replaceAssistantText(msg *message.Message, text string) {
	parts := make([]message.ContentPart, 0, len(msg.Parts)+1)
	replaced := false
	for _, part := range msg.Parts {
		switch part.(type) {
		case message.TextContent:
			if !replaced {
				parts = append(parts, message.TextContent{Text: text})
				replaced = true
			}
		case message.Finish:
			continue
		default:
			parts = append(parts, part)
		}
	}
	if !replaced {
		parts = append(parts, message.TextContent{Text: text})
	}
	msg.Parts = parts
}
