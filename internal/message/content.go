package message

import (
	"encoding/base64"
	"fmt"
	"html"
	"log/slog"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"

	"github.com/tta-lab/lenos/internal/agent/lenosbash"
)

type MessageRole string

const (
	Assistant MessageRole = "assistant"
	User      MessageRole = "user"
	System    MessageRole = "system"
	Result    MessageRole = "result"
)

type FinishReason string

const (
	FinishReasonEndTurn          FinishReason = "end_turn"
	FinishReasonMaxTokens        FinishReason = "max_tokens"
	FinishReasonToolUse          FinishReason = "tool_use"
	FinishReasonCanceled         FinishReason = "canceled"
	FinishReasonError            FinishReason = "error"
	FinishReasonPermissionDenied FinishReason = "permission_denied"

	// Should never happen
	FinishReasonUnknown FinishReason = "unknown"
)

type ContentPart interface {
	isPart()
}

type TextContentKind string

const (
	TextContentKindMessageBlock TextContentKind = "message_block"
)

type ReasoningContent struct {
	Thinking         string                             `json:"thinking"`
	Signature        string                             `json:"signature"`
	ThoughtSignature string                             `json:"thought_signature"` // Used for google
	ToolID           string                             `json:"tool_id"`           // Used for openrouter google models
	ResponsesData    *openai.ResponsesReasoningMetadata `json:"responses_data"`
	StartedAt        int64                              `json:"started_at,omitempty"`
	FinishedAt       int64                              `json:"finished_at,omitempty"`
}

func (tc ReasoningContent) String() string {
	return tc.Thinking
}
func (ReasoningContent) isPart() {}

type TextContent struct {
	Text string          `json:"text"`
	Kind TextContentKind `json:"kind,omitempty"`
}

func (tc TextContent) String() string {
	return tc.Text
}

func (TextContent) isPart() {}

// CommandContent represents a command execution result in a message.
type CommandContent struct {
	Command     string `json:"command"`
	Output      string `json:"output"`
	ExitCode    *int   `json:"exit_code,omitempty"`
	Pending     bool   `json:"pending"`
	Observation string `json:"observation,omitempty"`
}

func (CommandContent) isPart() {}
func (cc CommandContent) String() string {
	if cc.Command == "" {
		return ""
	}
	if cc.Pending {
		return "$ " + cc.Command + " (running...)"
	}
	s := "$ " + cc.Command
	if cc.Output != "" {
		s += "\n" + cc.Output
	}
	if cc.ExitCode != nil {
		s += fmt.Sprintf("\nexit code: %d", *cc.ExitCode)
	}
	return s
}

type ImageURLContent struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

func (iuc ImageURLContent) String() string {
	return iuc.URL
}

func (ImageURLContent) isPart() {}

type BinaryContent struct {
	Path     string
	MIMEType string
	Data     []byte
}

func (bc BinaryContent) String(p catwalk.InferenceProvider) string {
	base64Encoded := base64.StdEncoding.EncodeToString(bc.Data)
	if p == catwalk.InferenceProviderOpenAI {
		return "data:" + bc.MIMEType + ";base64," + base64Encoded
	}
	return base64Encoded
}

func (BinaryContent) isPart() {}

type Finish struct {
	Reason  FinishReason `json:"reason"`
	Time    int64        `json:"time"`
	Message string       `json:"message,omitempty"`
	Details string       `json:"details,omitempty"`
	Title   string       `json:"title,omitempty"`
}

func (Finish) isPart() {}

type Message struct {
	ID        string
	Role      MessageRole
	SessionID string
	Parts     []ContentPart
	Model     string
	// Provider is the config provider identifier (e.g. "minimax-china"). This
	// value is looked up by the UI via cfg.GetModel(provider, model) to render
	// the model display name in the footer. Must be a config-level provider ID,
	// NOT a fantasy protocol family name (e.g. "anthropic").
	Provider         string
	CreatedAt        int64
	UpdatedAt        int64
	IsSummaryMessage bool
}

func (m *Message) Content() TextContent {
	for _, part := range m.Parts {
		if c, ok := part.(TextContent); ok {
			return c
		}
	}
	return TextContent{}
}

// CommandContent returns the first command content part.
func (m *Message) CommandContent() CommandContent {
	for _, part := range m.Parts {
		if c, ok := part.(CommandContent); ok {
			return c
		}
	}
	return CommandContent{}
}

func (m *Message) ReasoningContent() ReasoningContent {
	for _, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			return c
		}
	}
	return ReasoningContent{}
}

func (m *Message) ImageURLContent() []ImageURLContent {
	imageURLContents := make([]ImageURLContent, 0)
	for _, part := range m.Parts {
		if c, ok := part.(ImageURLContent); ok {
			imageURLContents = append(imageURLContents, c)
		}
	}
	return imageURLContents
}

func (m *Message) BinaryContent() []BinaryContent {
	binaryContents := make([]BinaryContent, 0)
	for _, part := range m.Parts {
		if c, ok := part.(BinaryContent); ok {
			binaryContents = append(binaryContents, c)
		}
	}
	return binaryContents
}

func (m *Message) IsFinished() bool {
	for _, part := range m.Parts {
		if _, ok := part.(Finish); ok {
			return true
		}
	}
	return false
}

func (m *Message) FinishPart() *Finish {
	for _, part := range m.Parts {
		if c, ok := part.(Finish); ok {
			return &c
		}
	}
	return nil
}

func (m *Message) FinishReason() FinishReason {
	for _, part := range m.Parts {
		if c, ok := part.(Finish); ok {
			return c.Reason
		}
	}
	return ""
}

func (m *Message) IsThinking() bool {
	if m.ReasoningContent().Thinking != "" && m.Content().Text == "" && !m.IsFinished() {
		return true
	}
	return false
}

func (m *Message) AppendContent(delta string) {
	found := false
	for i, part := range m.Parts {
		if c, ok := part.(TextContent); ok {
			m.Parts[i] = TextContent{Text: c.Text + delta, Kind: c.Kind}
			found = true
		}
	}
	if !found {
		m.Parts = append(m.Parts, TextContent{Text: delta})
	}
}

func (m *Message) AppendReasoningContent(delta string) {
	found := false
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			c.Thinking += delta
			m.Parts[i] = c
			found = true
		}
	}
	if !found {
		m.Parts = append(m.Parts, ReasoningContent{
			Thinking:  delta,
			StartedAt: time.Now().Unix(),
		})
	}
}

func (m *Message) AppendThoughtSignature(signature string, toolCallID string) {
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			c.ThoughtSignature += signature
			c.ToolID = toolCallID
			m.Parts[i] = c
			return
		}
	}
	m.Parts = append(m.Parts, ReasoningContent{ThoughtSignature: signature})
}

func (m *Message) AppendReasoningSignature(signature string) {
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			c.Signature += signature
			m.Parts[i] = c
			return
		}
	}
	m.Parts = append(m.Parts, ReasoningContent{Signature: signature})
}

func (m *Message) SetReasoningResponsesData(data *openai.ResponsesReasoningMetadata) {
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			c.ResponsesData = data
			m.Parts[i] = c
			return
		}
	}
	m.Parts = append(m.Parts, ReasoningContent{
		ResponsesData: data,
		StartedAt:     time.Now().Unix(),
	})
}

func (m *Message) FinishThinking() {
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			if c.FinishedAt == 0 {
				c.FinishedAt = time.Now().Unix()
				m.Parts[i] = c
			}
			return
		}
	}
}

func (m *Message) ThinkingDuration() time.Duration {
	reasoning := m.ReasoningContent()
	if reasoning.StartedAt == 0 {
		return 0
	}

	endTime := reasoning.FinishedAt
	if endTime == 0 {
		endTime = time.Now().Unix()
	}

	return time.Duration(endTime-reasoning.StartedAt) * time.Second
}

// Clone returns a deep copy of the message with an independent Parts slice.
// This prevents race conditions when the message is modified concurrently.
func (m *Message) Clone() Message {
	clone := *m
	clone.Parts = make([]ContentPart, len(m.Parts))
	copy(clone.Parts, m.Parts)
	return clone
}

func (m *Message) AddFinish(reason FinishReason, message, details string) {
	// remove any existing finish part
	for i, part := range m.Parts {
		if _, ok := part.(Finish); ok {
			m.Parts = slices.Delete(m.Parts, i, i+1)
			break
		}
	}
	m.Parts = append(m.Parts, Finish{Reason: reason, Time: time.Now().Unix(), Message: message, Details: details})
}

func (m *Message) AddImageURL(url, detail string) {
	m.Parts = append(m.Parts, ImageURLContent{URL: url, Detail: detail})
}

func (m *Message) AddBinary(mimeType string, data []byte) {
	m.Parts = append(m.Parts, BinaryContent{MIMEType: mimeType, Data: data})
}

func PromptWithTextAttachments(prompt string, attachments []Attachment) string {
	var sb strings.Builder
	sb.WriteString(prompt)
	addedAttachments := false
	textIndex := 0
	for _, content := range attachments {
		if !content.IsText() {
			continue
		}
		if !addedAttachments {
			sb.WriteString("\n\n# Attached Files\n")
			sb.WriteString("The files below have been attached by the user; consider them in your response.\n\n")
			addedAttachments = true
		}
		var body strings.Builder
		if content.FilePath != "" {
			fmt.Fprintf(&body, "# File: %s\n\n", content.FilePath)
		} else {
			body.WriteString("# File\n\n")
		}
		body.Write(content.Content)
		fmt.Fprintf(&sb, "<attachment id=\"%d\">\n%s\n</attachment>\n", textIndex, body.String())
		textIndex++
	}
	return sb.String()
}

func (m *Message) ToAIMessage() []fantasy.Message {
	var messages []fantasy.Message
	switch m.Role {
	case User:
		var parts []fantasy.MessagePart
		text := strings.TrimSpace(m.Content().Text)
		var textAttachments []Attachment
		for _, content := range m.BinaryContent() {
			if !strings.HasPrefix(content.MIMEType, "text/") {
				continue
			}
			textAttachments = append(textAttachments, Attachment{
				FilePath: content.Path,
				MimeType: content.MIMEType,
				Content:  content.Data,
			})
		}
		text = PromptWithTextAttachments(text, textAttachments)
		if text != "" {
			parts = append(parts, fantasy.TextPart{Text: text})
		}
		for _, content := range m.BinaryContent() {
			// skip text attachements
			if strings.HasPrefix(content.MIMEType, "text/") {
				continue
			}
			parts = append(parts, fantasy.FilePart{
				Filename:  content.Path,
				Data:      content.Data,
				MediaType: content.MIMEType,
			})
		}
		messages = append(messages, fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: parts,
		})
	case Assistant:
		var parts []fantasy.MessagePart
		// Reasoning first (required for Anthropic extended thinking signature validation).
		reasoning := m.ReasoningContent()
		if reasoning.Thinking != "" {
			reasoningPart := fantasy.ReasoningPart{Text: reasoning.Thinking, ProviderOptions: fantasy.ProviderOptions{}}
			if reasoning.Signature != "" {
				reasoningPart.ProviderOptions[anthropic.Name] = &anthropic.ReasoningOptionMetadata{
					Signature: reasoning.Signature,
				}
			}
			if reasoning.ResponsesData != nil {
				reasoningPart.ProviderOptions[openai.Name] = reasoning.ResponsesData
			}
			if reasoning.ThoughtSignature != "" {
				reasoningPart.ProviderOptions[google.Name] = &google.ReasoningMetadata{
					Signature: reasoning.ThoughtSignature,
					ToolID:    reasoning.ToolID,
				}
			}
			parts = append(parts, reasoningPart)
		}
		// Then text.
		text := strings.TrimSpace(m.Content().Text)
		if text != "" {
			parts = append(parts, fantasy.TextPart{Text: text})
		}
		messages = append(messages, fantasy.Message{
			Role:    fantasy.MessageRoleAssistant,
			Content: parts,
		})
	case Result:
		var results []CommandContent
		var texts []string
		hasParts := len(m.Parts) > 0
		for _, part := range m.Parts {
			switch part := part.(type) {
			case TextContent:
				if text := strings.TrimSpace(part.Text); text != "" {
					texts = append(texts, text)
				}
			case CommandContent:
				if part.Command == "" || part.Pending {
					continue
				}
				results = append(results, part)
			}
		}
		var replay []string
		replay = append(replay, texts...)
		if len(results) > 0 {
			replay = append(replay, FormatResults(results))
		}
		if len(replay) == 0 {
			if hasParts {
				// Parts existed but none were replayable — warn only in this case.
				slog.Warn("Result message has no replayable content parts")
			}
			break
		}
		messages = append(messages, fantasy.Message{
			Role: fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{
				fantasy.TextPart{Text: strings.Join(replay, "\n\n")},
			},
		})
	}
	return messages
}

// FormatResults renders completed CommandContent as the result block the
// next-turn user message carries back to the model.
func FormatResults(results []CommandContent) string {
	if len(results) == 0 {
		return ""
	}
	parts := make([]string, len(results))
	for i, r := range results {
		parts[i] = formatOneResult(r)
	}
	return lenosbash.ResultBlock(strings.Join(parts, "\n"))
}

func formatOneResult(r CommandContent) string {
	if r.Observation != "" {
		return r.Observation
	}
	output := html.EscapeString(r.Output)
	if output == "" {
		output = "(no output)"
	}
	if r.ExitCode != nil && *r.ExitCode != 0 && *r.ExitCode != -1 {
		output += fmt.Sprintf("\n(exit code: %d)", *r.ExitCode)
	}
	return output
}
