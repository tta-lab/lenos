package agent

import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/message"
)

func TestGoalDir(t *testing.T) {
	t.Parallel()
	dir := GoalDir("/tmp/test")
	assert.Equal(t, filepath.Join("/tmp/test", ".lenos", "goals"), dir)
}

func TestGoalPath(t *testing.T) {
	t.Parallel()
	path := GoalPath("/tmp/test", "session-123")
	assert.Equal(t, filepath.Join("/tmp/test", ".lenos", "goals", "session-123.md"), path)
}

func TestCreateGoal(t *testing.T) {
	dir := t.TempDir()
	logPath := installFakeGoalCLI(t)

	path, err := CreateGoal(t.Context(), dir, "session-1", "# My Goal\nDo something.", GoalActive)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, ".lenos", "goals", "session-1.md"), path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "status: active")
	assert.Contains(t, content, "created_at:")
	assert.Contains(t, content, "updated_at:")
	assert.Contains(t, content, "# My Goal")
	assert.Contains(t, content, "Do something.")
	assert.Contains(t, readFile(t, logPath), "add --status active")
}

func TestCreateGoal_Draft(t *testing.T) {
	dir := t.TempDir()
	installFakeGoalCLI(t)

	path, err := CreateGoal(t.Context(), dir, "session-1", "# Draft\n", GoalDraft)
	require.NoError(t, err)

	status, err := ReadGoalStatus(t.Context(), path)
	require.NoError(t, err)
	assert.Equal(t, GoalDraft, status)
	assert.False(t, IsGoalRuntimeActive(status))
	assert.False(t, IsGoalTerminal(status))
}

func TestCreateGoal_Overwrites(t *testing.T) {
	dir := t.TempDir()
	logPath := installFakeGoalCLI(t)

	_, err := CreateGoal(t.Context(), dir, "session-1", "first", GoalActive)
	require.NoError(t, err)

	_, err = CreateGoal(t.Context(), dir, "session-1", "second", GoalActive)
	require.NoError(t, err)

	status, err := ReadGoalStatus(t.Context(), GoalPath(dir, "session-1"))
	require.NoError(t, err)
	assert.Equal(t, GoalActive, status)

	data, err := os.ReadFile(GoalPath(dir, "session-1"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "second")
	assert.NotContains(t, string(data), "first")

	log := readFile(t, logPath)
	assert.Contains(t, log, "add --status active")
	assert.Contains(t, log, "update")
	assert.Contains(t, log, "status active")
	assert.NotContains(t, log, "--force")
}

func TestReadGoalStatus_Active(t *testing.T) {
	dir := t.TempDir()
	installFakeGoalCLI(t)

	_, err := CreateGoal(t.Context(), dir, "session-1", "body", GoalActive)
	require.NoError(t, err)

	status, err := ReadGoalStatus(t.Context(), GoalPath(dir, "session-1"))
	require.NoError(t, err)
	assert.Equal(t, GoalActive, status)
}

func TestReadGoalStatus_Complete(t *testing.T) {
	dir := t.TempDir()
	installFakeGoalCLI(t)

	path := GoalPath(dir, "session-1")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := "---\nstatus: complete\ncreated_at: 2026-06-09T12:00:00Z\n---\n\n# Done"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	status, err := ReadGoalStatus(t.Context(), path)
	require.NoError(t, err)
	assert.Equal(t, GoalComplete, status)
}

func TestReadGoalStatus_Blocked(t *testing.T) {
	dir := t.TempDir()
	installFakeGoalCLI(t)

	path := GoalPath(dir, "session-1")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := "---\nstatus: blocked\ncreated_at: 2026-06-09T12:00:00Z\n---\n\n# Blocked"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	status, err := ReadGoalStatus(t.Context(), path)
	require.NoError(t, err)
	assert.Equal(t, GoalBlocked, status)
}

func TestReadGoalStatus_FileNotFound_ReturnsActive(t *testing.T) {
	installFakeGoalCLI(t)
	status, err := ReadGoalStatus(t.Context(), "/nonexistent/path/goal.md")
	assert.Equal(t, GoalActive, status)
	assert.Error(t, err)
}

func TestReadGoalStatus_MissingFrontmatter_ReturnsActive(t *testing.T) {
	dir := t.TempDir()
	installFakeGoalCLI(t)

	path := GoalPath(dir, "session-1")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("# Just a goal\nNo frontmatter."), 0o644))

	status, err := ReadGoalStatus(t.Context(), path)
	assert.Equal(t, GoalActive, status)
	assert.Error(t, err)
}

func TestReadGoalStatus_MissingStatus_ReturnsActive(t *testing.T) {
	dir := t.TempDir()
	installFakeGoalCLI(t)

	path := GoalPath(dir, "session-1")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := "---\ncreated_at: 2026-06-09T12:00:00Z\n---\n\n# Goal without status"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	status, err := ReadGoalStatus(t.Context(), path)
	assert.Equal(t, GoalActive, status)
	assert.Error(t, err)
}

func TestReadGoalStatus_InvalidYAML_ReturnsActive(t *testing.T) {
	dir := t.TempDir()
	installFakeGoalCLI(t)

	path := GoalPath(dir, "session-1")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := "---\n{this is not valid yaml]\n---\n\n# Bad frontmatter"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	status, err := ReadGoalStatus(t.Context(), path)
	assert.Equal(t, GoalActive, status)
	assert.Error(t, err)
}

func TestGoalCheckHint_ContainsKeyInstructions(t *testing.T) {
	t.Parallel()
	hint := goalCheckHint()
	assert.Contains(t, hint, "LENOS_GOAL")
	assert.Contains(t, hint, "goal get")
	assert.Contains(t, hint, "goal status complete")
	assert.Contains(t, hint, "goal status blocked")
	assert.Contains(t, hint, "edit `$LENOS_GOAL` frontmatter")
	assert.Contains(t, hint, "LENOS_JOURNAL")
}

func TestGoalUpdateHint_ContainsKeyInstructions(t *testing.T) {
	t.Parallel()
	hint := GoalUpdateHint()
	assert.Contains(t, hint, "LENOS_GOAL")
	assert.Contains(t, hint, "goal get")
	assert.Contains(t, hint, "runtime_goal_updated")
}

func TestGoalStartupHint_ContainsKeyInstructions(t *testing.T) {
	t.Parallel()
	hint := goalStartupHint()
	assert.Contains(t, hint, "LENOS_GOAL")
	assert.Contains(t, hint, "runtime_goal")
	assert.Contains(t, hint, "LENOS_JOURNAL")
	assert.Contains(t, hint, "goal get")
	assert.Contains(t, hint, "goal status complete")
	assert.Contains(t, hint, "goal status blocked")
}

func TestTryEndTurn_GatesOnlyActiveGoals(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  GoalStatus
		wantEnd bool
	}{
		{name: "active gates", status: GoalActive, wantEnd: false},
		{name: "draft exits", status: GoalDraft, wantEnd: true},
		{name: "complete exits", status: GoalComplete, wantEnd: true},
		{name: "blocked exits", status: GoalBlocked, wantEnd: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			installFakeGoalCLI(t)
			path, err := CreateGoal(t.Context(), dir, "session-1", "# Goal\n", tc.status)
			require.NoError(t, err)
			msgs := newMockMessageService()
			assistantMsg := &message.Message{ID: "assistant-1", Role: message.Assistant}

			gotMsgs, gotEnd, err := tryEndTurn(t.Context(), loopDeps{
				messages:  msgs,
				sessionID: "session-1",
				goalPath:  path,
			}, []fantasy.Message{}, "final", assistantMsg)

			require.NoError(t, err)
			assert.Equal(t, tc.wantEnd, gotEnd)
			joined := fantasyMessagesText(gotMsgs)
			if tc.wantEnd {
				assert.NotContains(t, joined, "runtime_goal_check")
			} else {
				assert.Contains(t, joined, "runtime_goal_check")
				assert.Contains(t, joined, "goal status complete")
			}
		})
	}
}

func installFakeGoalCLI(t *testing.T) string {
	t.Helper()

	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "goal.log")
	script := filepath.Join(binDir, "goal")
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$GOAL_TEST_LOG"
if [ -z "${LENOS_GOAL:-}" ]; then
  echo "LENOS_GOAL is unset" >&2
  exit 2
fi
cmd="${1:-}"
if [ "$#" -gt 0 ]; then
  shift
fi
case "$cmd" in
  add)
    status="active"
    if [ "${1:-}" = "--status" ]; then
      status="$2"
    fi
    mkdir -p "$(dirname "$LENOS_GOAL")"
    body="$(cat)"
    printf -- "---\nstatus: %s\ncreated_at: 2026-06-11T00:00:00Z\nupdated_at: 2026-06-11T00:00:00Z\n---\n%s" "$status" "$body" > "$LENOS_GOAL"
    ;;
  get)
    if [ ! -f "$LENOS_GOAL" ]; then
      echo "goal file not found" >&2
      exit 1
    fi
    status="$(sed -n 's/^status: //p' "$LENOS_GOAL" | head -n 1)"
    if [ -z "$status" ]; then
      echo "goal status missing" >&2
      exit 1
    fi
    if [ "${1:-}" = "--json" ]; then
      printf '{"status":"%s"}\n' "$status"
    else
      cat "$LENOS_GOAL"
    fi
    ;;
  status)
    if [ ! -f "$LENOS_GOAL" ]; then
      echo "goal file not found" >&2
      exit 1
    fi
    new_status="$1"
    tmp="$LENOS_GOAL.tmp"
    sed "s/^status: .*/status: $new_status/" "$LENOS_GOAL" > "$tmp"
    mv "$tmp" "$LENOS_GOAL"
    ;;
  update)
    if [ ! -f "$LENOS_GOAL" ]; then
      echo "goal file not found" >&2
      exit 1
    fi
    body="$(cat)"
    tmp="$LENOS_GOAL.tmp"
    sed -n '1,/^---$/p' "$LENOS_GOAL" > "$tmp"
    printf '%s' "$body" >> "$tmp"
    mv "$tmp" "$LENOS_GOAL"
    ;;
  *)
    echo "unknown command" >&2
    exit 1
    ;;
esac
`), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOAL_TEST_LOG", logPath)
	return logPath
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func fantasyMessagesText(msgs []fantasy.Message) string {
	var out string
	for _, msg := range msgs {
		for _, part := range msg.Content {
			if text, ok := part.(fantasy.TextPart); ok {
				out += text.Text + "\n"
			}
		}
	}
	return out
}
