package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	t.Parallel()
	dir := t.TempDir()

	path, err := CreateGoal(dir, "session-1", "# My Goal\nDo something.", "2026-06-09T12:00:00Z")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, ".lenos", "goals", "session-1.md"), path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "status: active")
	assert.Contains(t, content, "created_at:")
	assert.Contains(t, content, "# My Goal")
	assert.Contains(t, content, "Do something.")
}

func TestCreateGoal_Overwrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, err := CreateGoal(dir, "session-1", "first", "2026-01-01T00:00:00Z")
	require.NoError(t, err)

	_, err = CreateGoal(dir, "session-1", "second", "2026-06-09T12:00:00Z")
	require.NoError(t, err)

	status, err := ReadGoalStatus(GoalPath(dir, "session-1"))
	require.NoError(t, err)
	assert.Equal(t, GoalActive, status)

	data, err := os.ReadFile(GoalPath(dir, "session-1"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "second")
	assert.NotContains(t, string(data), "first")
}

func TestReadGoalStatus_Active(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, err := CreateGoal(dir, "session-1", "body", "2026-06-09T12:00:00Z")
	require.NoError(t, err)

	status, err := ReadGoalStatus(GoalPath(dir, "session-1"))
	require.NoError(t, err)
	assert.Equal(t, GoalActive, status)
}

func TestReadGoalStatus_Complete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	path := GoalPath(dir, "session-1")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := "---\nstatus: complete\ncreated_at: 2026-06-09T12:00:00Z\n---\n\n# Done"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	status, err := ReadGoalStatus(path)
	require.NoError(t, err)
	assert.Equal(t, GoalComplete, status)
}

func TestReadGoalStatus_Blocked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	path := GoalPath(dir, "session-1")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := "---\nstatus: blocked\ncreated_at: 2026-06-09T12:00:00Z\n---\n\n# Blocked"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	status, err := ReadGoalStatus(path)
	require.NoError(t, err)
	assert.Equal(t, GoalBlocked, status)
}

func TestReadGoalStatus_FileNotFound_ReturnsActive(t *testing.T) {
	t.Parallel()
	status, err := ReadGoalStatus("/nonexistent/path/goal.md")
	assert.Equal(t, GoalActive, status)
	assert.Error(t, err)
}

func TestReadGoalStatus_MissingFrontmatter_ReturnsActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	path := GoalPath(dir, "session-1")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("# Just a goal\nNo frontmatter."), 0o644))

	status, err := ReadGoalStatus(path)
	assert.Equal(t, GoalActive, status)
	assert.Error(t, err)
}

func TestReadGoalStatus_MissingStatus_ReturnsActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	path := GoalPath(dir, "session-1")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := "---\ncreated_at: 2026-06-09T12:00:00Z\n---\n\n# Goal without status"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	status, err := ReadGoalStatus(path)
	assert.Equal(t, GoalActive, status)
	assert.Error(t, err)
}

func TestReadGoalStatus_InvalidYAML_ReturnsActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	path := GoalPath(dir, "session-1")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := "---\n{this is not valid yaml]\n---\n\n# Bad frontmatter"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	status, err := ReadGoalStatus(path)
	assert.Equal(t, GoalActive, status)
	assert.Error(t, err)
}

func TestGoalCheckHint_ContainsKeyInstructions(t *testing.T) {
	t.Parallel()
	hint := goalCheckHint()
	assert.Contains(t, hint, "LENOS_GOAL")
	assert.Contains(t, hint, "still active")
	assert.Contains(t, hint, "status: complete")
	assert.Contains(t, hint, "status: blocked")
	assert.Contains(t, hint, "LENOS_JOURNAL")
}

func TestGoalUpdateHint_ContainsKeyInstructions(t *testing.T) {
	t.Parallel()
	hint := GoalUpdateHint()
	assert.Contains(t, hint, "LENOS_GOAL")
	assert.Contains(t, hint, "was modified")
	assert.Contains(t, hint, "Re-read")
	assert.Contains(t, hint, "Adjust your task")
}

func TestGoalStartupHint_ContainsKeyInstructions(t *testing.T) {
	t.Parallel()
	hint := goalStartupHint()
	assert.Contains(t, hint, "LENOS_GOAL")
	assert.Contains(t, hint, "completion contract")
	assert.Contains(t, hint, "LENOS_JOURNAL")
	assert.Contains(t, hint, "status: complete")
	assert.Contains(t, hint, "status: blocked")
}
