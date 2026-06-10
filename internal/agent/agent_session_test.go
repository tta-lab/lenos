package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/session"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
)

func TestUpdateSessionUsage_AccumulatesLifetimeTotals(t *testing.T) {
	t.Parallel()

	s := &session.Session{
		ID: "session-1",
	}
	model := Model{
		ModelCfg: config.SelectedModel{Provider: "openai", Model: "gpt-4o"},
		CatwalkCfg: catwalk.Model{
			ID:   "gpt-4o",
			Name: "GPT-4o",
		},
	}

	sa := &sessionAgent{}

	// First call: 100 input, 50 output, 10 cache read.
	sa.updateSessionUsage(model, s, fantasy.Usage{
		InputTokens:         100,
		OutputTokens:        50,
		CacheReadTokens:     10,
		CacheCreationTokens: 5,
	}, nil)

	require.Equal(t, int64(110), s.TotalPromptTokens,
		"TotalPromptTokens should accumulate InputTokens + CacheReadTokens")
	require.Equal(t, int64(50), s.TotalCompletionTokens,
		"TotalCompletionTokens should accumulate OutputTokens")
	require.Equal(t, int64(0), s.TotalReasoningTokens,
		"TotalReasoningTokens should be zero when not set")
	require.Equal(t, int64(5), s.CacheCreationTokens)
	require.Equal(t, int64(10), s.CacheReadTokens)
	require.Equal(t, int64(105), s.CacheMissTokens,
		"CacheMissTokens still uses old InputTokens + CacheCreationTokens semantics")

	// Second call: 200 input, 80 output, 15 cache read, 20 reasoning.
	sa.updateSessionUsage(model, s, fantasy.Usage{
		InputTokens:         200,
		OutputTokens:        80,
		CacheReadTokens:     15,
		CacheCreationTokens: 5,
		ReasoningTokens:     20,
	}, nil)

	require.Equal(t, int64(110+215), s.TotalPromptTokens,
		"TotalPromptTokens should accumulate across calls")
	require.Equal(t, int64(50+80), s.TotalCompletionTokens,
		"TotalCompletionTokens should accumulate across calls")
	require.Equal(t, int64(20), s.TotalReasoningTokens,
		"TotalReasoningTokens should accumulate")
	require.Equal(t, int64(10), s.CacheCreationTokens,
		"CacheCreationTokens should accumulate")
	require.Equal(t, int64(25), s.CacheReadTokens,
		"CacheReadTokens should accumulate")
	require.Equal(t, int64(105+205), s.CacheMissTokens,
		"CacheMissTokens should accumulate with old semantics")
}

func TestUpdateSessionUsage_SkipsZeroTokenTurn(t *testing.T) {
	t.Parallel()

	s := &session.Session{
		ID: "session-1",
	}
	model := Model{
		ModelCfg: config.SelectedModel{Provider: "openai", Model: "gpt-4o"},
		CatwalkCfg: catwalk.Model{
			ID:   "gpt-4o",
			Name: "GPT-4o",
		},
	}

	sa := &sessionAgent{}

	sa.updateSessionUsage(model, s, fantasy.Usage{
		InputTokens:         50,
		OutputTokens:        20,
		CacheReadTokens:     5,
		CacheCreationTokens: 3,
	}, nil)

	require.Equal(t, int64(55), s.TotalPromptTokens)

	// Zero-token turn should not change totals.
	sa.updateSessionUsage(model, s, fantasy.Usage{
		InputTokens:         0,
		OutputTokens:        0,
		CacheReadTokens:     0,
		CacheCreationTokens: 0,
	}, nil)

	require.Equal(t, int64(55), s.TotalPromptTokens,
		"Zero-token turn should not accumulate")
	require.Equal(t, int64(20), s.TotalCompletionTokens,
		"Zero-token turn should not accumulate")
}

func TestUpdateSessionUsage_CompactionDoesNotEraseLifetimeTotals(t *testing.T) {
	t.Parallel()

	s := &session.Session{
		ID: "session-1",
	}
	model := Model{
		ModelCfg: config.SelectedModel{Provider: "openai", Model: "gpt-4o"},
		CatwalkCfg: catwalk.Model{
			ID:   "gpt-4o",
			Name: "GPT-4o",
		},
	}

	sa := &sessionAgent{}

	// Pre-compaction usage.
	sa.updateSessionUsage(model, s, fantasy.Usage{
		InputTokens:         500,
		OutputTokens:        200,
		CacheReadTokens:     50,
		CacheCreationTokens: 10,
		ReasoningTokens:     30,
	}, nil)

	require.Equal(t, int64(550), s.TotalPromptTokens)
	require.Equal(t, int64(200), s.TotalCompletionTokens)
	require.Equal(t, int64(30), s.TotalReasoningTokens)

	// Simulate compaction reset on current-context fields (model does this).
	s.PromptTokens = 100
	s.CompletionTokens = 50

	// Post-compaction usage — totals keep accumulating.
	sa.updateSessionUsage(model, s, fantasy.Usage{
		InputTokens:         300,
		OutputTokens:        100,
		CacheReadTokens:     20,
		CacheCreationTokens: 5,
		ReasoningTokens:     15,
	}, nil)

	require.Equal(t, int64(550+320), s.TotalPromptTokens,
		"Lifetime total_prompt_tokens should survive compaction")
	require.Equal(t, int64(200+100), s.TotalCompletionTokens,
		"Lifetime total_completion_tokens should survive compaction")
	require.Equal(t, int64(30+15), s.TotalReasoningTokens,
		"Lifetime total_reasoning_tokens should survive compaction")

	// Current-context fields reflect only the latest turn values,
	// not cumulative.
	require.Equal(t, int64(320), s.PromptTokens)
	require.Equal(t, int64(100), s.CompletionTokens)
}

func TestUpdateSessionUsage_ProviderNormalizationGuard(t *testing.T) {
	t.Parallel()
	// Two real provider shapes from fantasy:
	//
	// OpenAI (language_model_hooks.go): InputTokens = prompt_tokens - cached_tokens.
	//   CacheReadTokens = cached_tokens. CacheCreationTokens = 0.
	//   → InputTokens is non-cached input only.
	//
	// Anthropic (anthropic.go): InputTokens = response.Usage.InputTokens (full input),
	//   CacheReadTokens = response.Usage.CacheReadInputTokens (cached portion).
	//   CacheCreationTokens = response.Usage.CacheCreationInputTokens.
	//   → InputTokens already includes the cached portion.
	//
	// TotalPromptTokens = total input to the model. For OpenAI that's
	// InputTokens + CacheReadTokens. For Anthropic that's max(InputTokens,
	// CacheReadTokens) since CacheReadTokens is a subset of InputTokens.

	sa := &sessionAgent{}
	model := Model{
		ModelCfg:   config.SelectedModel{Provider: "test", Model: "test"},
		CatwalkCfg: catwalk.Model{ID: "test", Name: "Test"},
	}

	// ---- OpenAI style: non-cached input + separate cached read ----
	s := &session.Session{ID: "openai-style"}
	sa.updateSessionUsage(model, s, fantasy.Usage{
		InputTokens:     7000, // non-cached only
		OutputTokens:    4000,
		CacheReadTokens: 3000,
	}, nil)
	require.Equal(t, int64(10000), s.TotalPromptTokens,
		"OpenAI-style: total_prompt = non-cached input + cached read")

	// ---- Anthropic style: InputTokens includes cached, CacheReadTokens is subset ----
	anModel := Model{
		ModelCfg:   config.SelectedModel{Provider: "anthropic", Model: "claude-sonnet"},
		CatwalkCfg: catwalk.Model{ID: "claude-sonnet", Name: "Claude Sonnet"},
	}
	s2 := &session.Session{ID: "anthropic-style"}
	sa.updateSessionUsage(anModel, s2, fantasy.Usage{
		InputTokens:         10000, // full input
		OutputTokens:        4000,
		CacheReadTokens:     3000, // subset of InputTokens
		CacheCreationTokens: 500,
	}, nil)
	// Anthropic-style: InputTokens is full prompt. CacheReadTokens is a subset.
	// TotalPromptTokens = InputTokens (not InputTokens + CacheReadTokens).
	require.Equal(t, int64(10000), s2.TotalPromptTokens,
		"Anthropic-style: TotalPromptTokens = InputTokens (CacheReadTokens is a subset)")

	// ---- Regression: CacheCreationTokens never leak into TotalPromptTokens ----
	s3 := &session.Session{ID: "no-leak"}
	sa.updateSessionUsage(model, s3, fantasy.Usage{
		InputTokens:         100,
		OutputTokens:        50,
		CacheReadTokens:     20,
		CacheCreationTokens: 999,
	}, nil)
	require.Equal(t, int64(120), s3.TotalPromptTokens,
		"CacheCreationTokens must NOT leak into TotalPromptTokens")
	require.Equal(t, int64(999), s3.CacheCreationTokens)
}
