package taskwarrior

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/tta-lab/lenos/internal/session"
)

type twTask struct {
	Description string `json:"description"`
	Status      string `json:"status"`
	Start       string `json:"start"`
}

// parseSubtasks parses a JSON array of task objects and returns a list of todos.
func parseSubtasks(data []byte) ([]session.Todo, error) {
	var tasks []twTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, err
	}

	todos := make([]session.Todo, 0, len(tasks))
	for _, task := range tasks {
		status := session.TodoStatus(task.Status)
		if status == session.TodoStatusPending && task.Start != "" {
			status = session.TodoStatusInProgress
		}
		todos = append(todos, session.Todo{
			Content:    task.Description,
			Status:     status,
			ActiveForm: task.Description,
		})
	}
	return todos, nil
}

// PollSubtasks runs `task parent_id:<jobID> status.not:deleted export` and
// maps the results to []session.Todo.
func PollSubtasks(ctx context.Context, jobID string) ([]session.Todo, error) {
	cmd := exec.CommandContext(ctx, "task",
		"rc.verbose=nothing", "rc.hooks=off", "rc.confirmation=no", "rc.json.array=on",
		"parent_id:"+jobID, "status.not:deleted", "export")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("task export failed: %w", err)
	}

	todos, err := parseSubtasks(out)
	if err != nil {
		return nil, fmt.Errorf("failed to parse task export JSON: %w", err)
	}
	return todos, nil
}
