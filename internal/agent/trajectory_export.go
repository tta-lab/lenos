package agent

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/tta-lab/lenos/internal/atif"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/version"
)

func ExportTrajectoryFile(path, sessionID, modelName string, messages []message.Message) error {
	return WriteTrajectoryFile(path, TrajectoryFromMessages(sessionID, modelName, messages))
}

func TrajectoryFromMessages(sessionID, modelName string, messages []message.Message) atif.Trajectory {
	traj := atif.Trajectory{
		SchemaVersion: "ATIF-v1.7",
		TrajectoryID:  sessionID,
		SessionID:     sessionID,
		Agent: atif.Agent{
			Name:      "lenos",
			Version:   version.Version,
			ModelName: modelName,
		},
		Extra: map[string]any{
			"export_source": "tui",
		},
	}

	for _, msg := range messages {
		step, ok := StepFromMessage(msg)
		if !ok {
			continue
		}
		step.StepID = len(traj.Steps) + 1
		traj.Steps = append(traj.Steps, step)
	}
	traj.FinalMetrics = finalMetrics(traj.Steps)
	return traj
}

func StepFromMessage(msg message.Message) (atif.Step, bool) {
	switch msg.Role {
	case message.User:
		content := cleanTrajectoryText(msg.Content().Text)
		if content == "" {
			return atif.Step{}, false
		}
		return atif.Step{
			Source:  "user",
			Message: content,
			Extra: map[string]any{
				"message_id": msg.ID,
			},
		}, true
	case message.System, message.Runtime:
		content := cleanTrajectoryText(msg.Content().Text)
		if content == "" {
			return atif.Step{}, false
		}
		return atif.Step{
			Source:  "system",
			Message: content,
			Extra: map[string]any{
				"message_id": msg.ID,
			},
		}, true
	case message.Assistant:
		content := cleanTrajectoryText(msg.Content().Text)
		if content == "" {
			return atif.Step{}, false
		}
		step := atif.Step{
			Source:    "agent",
			Message:   content,
			ModelName: msg.Model,
			Extra: map[string]any{
				"message_id": msg.ID,
			},
		}
		if msg.Provider != "" {
			step.Extra["provider"] = msg.Provider
		}
		return step, true
	case message.Result:
		command := msg.CommandContent()
		if command.Command == "" {
			return atif.Step{}, false
		}
		content := cleanTrajectoryText(command.Output)
		if command.Observation != "" {
			content = cleanTrajectoryText(command.Observation)
		}
		if content == "" {
			content = cleanTrajectoryText(command.String())
		}
		cleanCommand := cleanTrajectoryText(command.Command)
		extra := map[string]any{
			"message_id": msg.ID,
			"command":    cleanCommand,
			"pending":    command.Pending,
		}
		if command.ExitCode != nil {
			extra["exit_code"] = *command.ExitCode
		}
		return atif.Step{
			Source: "system",
			Observation: &atif.Observation{
				Results: []atif.ObservationResult{{
					Content:      content,
					SourceCallID: cleanCommand,
					Extra:        extra,
				}},
			},
			Extra: map[string]any{
				"message_id": msg.ID,
				"kind":       "result",
			},
		}, true
	default:
		return atif.Step{}, false
	}
}

func cleanTrajectoryText(text string) string {
	return ansi.Strip(text)
}
