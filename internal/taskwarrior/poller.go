package taskwarrior

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"

	"github.com/tta-lab/lenos/internal/session"
)

type twTask struct {
	Description string `json:"description"`
	Status      string `json:"status"`
	Start       string `json:"start"`
}

// PollSubtasks runs `task parent_id:<jobID> status.not:deleted export` and
// maps the results to []session.Todo.
func PollSubtasks(ctx context.Context, jobID string) ([]session.Todo, error) {
	cmd := exec.CommandContext(ctx, "task", "parent_id:"+jobID, "status.not:deleted", "export")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var todos []session.Todo
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var task twTask
		if err := json.Unmarshal([]byte(line), &task); err != nil {
			continue
		}

		status := session.TodoStatus(task.Status)
		// A started task has a non-empty start attribute.
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
