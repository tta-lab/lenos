package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/config"
)

func TestRunUsageSummaryExportsCostAndCacheFields(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 6, 4, 4, 26, 15, 0, time.UTC)
	finished := time.Date(2026, 6, 4, 4, 28, 39, 0, time.UTC)
	summary := NewRunUsageSummary("session-1", started)
	summary.AddUsage(Model{
		ModelCfg: config.SelectedModel{Provider: "deepseek", Model: "deepseek-v4-flash"},
		CatwalkCfg: catwalk.Model{
			ID:   "deepseek-v4-flash",
			Name: "DeepSeek-V4-Flash",
		},
	}, fantasy.Usage{
		InputTokens:         1632,
		OutputTokens:        3972,
		ReasoningTokens:     2754,
		CacheCreationTokens: 17,
		CacheReadTokens:     39296,
	}, 0.1234)
	summary.Finish(finished)

	data, err := json.Marshal(summary)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	require.Equal(t, float64(1), got["version"])
	require.Equal(t, "run_summary", got["event"])
	require.Equal(t, "session-1", got["session_id"])
	require.Equal(t, "deepseek", got["provider_id"])
	require.Equal(t, "deepseek-v4-flash", got["model_id"])
	require.Equal(t, float64(40928), got["input_tokens"])
	require.Equal(t, float64(1632), got["raw_input_tokens"])
	require.Equal(t, float64(39296), got["input_cache_hit_tokens"])
	require.Equal(t, float64(1649), got["input_cache_miss_tokens"])
	require.Equal(t, float64(17), got["cache_creation_tokens"])
	require.Equal(t, float64(39296), got["cache_read_tokens"])
	require.Equal(t, float64(3972), got["output_tokens"])
	require.Equal(t, float64(2754), got["reasoning_tokens"])
	require.Equal(t, float64(44900), got["total_tokens"])
	require.Equal(t, 0.1234, got["cost_usd"])
	require.Equal(t, "2026-06-04T04:26:15Z", got["started_at"])
	require.Equal(t, "2026-06-04T04:28:39Z", got["finished_at"])
}

func TestWriteRunUsageSummaryFileErrorsForMissingParent(t *testing.T) {
	t.Parallel()

	summary := NewRunUsageSummary("session-1", time.Date(2026, 6, 4, 4, 26, 15, 0, time.UTC))
	path := filepath.Join(t.TempDir(), "missing", "usage.json")

	err := WriteRunUsageSummaryFile(path, summary)

	require.ErrorContains(t, err, "write usage summary")
}
