package taskwarrior

import (
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
