package taskwarrior

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/session"
)

func TestParseSubtasks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "empty array",
			input:     `[]`,
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "two pending tasks",
			input:     `[{ "description": "First task", "status": "pending" }, { "description": "Second task", "status": "pending" }]`,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "task with start field is in progress",
			input:     `[{ "description": "Working on it", "status": "pending", "start": "20260407T120000" }]`,
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "long description preserved",
			input:     `[{ "description": "` + longDesc(150) + `", "status": "pending" }]`,
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:    "malformed JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			todos, err := parseSubtasks([]byte(tc.input))
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, todos, tc.wantCount)
		})
	}
}

func longDesc(n int) string {
	result := make([]byte, n)
	for i := range result {
		result[i] = 'a'
	}
	return string(result)
}

func TestParseSubtasks_StatusMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		wantStatus session.TodoStatus
	}{
		{"pending", `[{ "description": "Do it", "status": "pending" }]`, session.TodoStatusPending},
		{"completed", `[{ "description": "Done", "status": "completed" }]`, session.TodoStatusCompleted},
		{"in_progress_via_start_field", `[{ "description": "Busy", "status": "pending", "start": "20260407" }]`, session.TodoStatusInProgress},
		{"unknown_deleted_passes_through", `[{ "description": "Gone", "status": "deleted" }]`, session.TodoStatus("deleted")},
		{"unknown_waiting_passes_through", `[{ "description": "Waiting", "status": "waiting" }]`, session.TodoStatus("waiting")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			todos, err := parseSubtasks([]byte(tc.input))
			require.NoError(t, err)
			require.Len(t, todos, 1)
			require.Equal(t, tc.wantStatus, todos[0].Status)
		})
	}
}

func TestPollSubtasks_ExecFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("task CLI not available on windows")
	}

	t.Parallel()

	tmp := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })
	os.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath)

	fakeTask := filepath.Join(tmp, "task")
	require.NoError(t, os.WriteFile(fakeTask, []byte("#!/bin/sh\nexit 1"), 0o755))

	_, err := PollSubtasks(context.Background(), "fake-job-id")
	require.Error(t, err)
	require.Contains(t, err.Error(), "task export failed")
}
