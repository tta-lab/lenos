package agent

import (
	"strings"

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
		if msg.Role == message.Result && !isBackgroundCompletionStep(step) {
			attached := attachObservationToLastAgentStep(&traj, step)
			if attached {
				continue
			}
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
		kind := "result"
		stepMessage := "Command result."
		if backgroundKind := backgroundKindFromText(content); backgroundKind != "" {
			kind = backgroundKind
			stepMessage = backgroundRuntimeMessage(backgroundKind)
			extra["background"] = true
			extra["job_id"] = extractBackgroundJobID(content)
		}
		return atif.Step{
			Source:  "system",
			Message: stepMessage,
			Observation: &atif.Observation{
				Results: []atif.ObservationResult{{
					Content: content,
					Extra:   extra,
				}},
			},
			Extra: map[string]any{
				"message_id": msg.ID,
				"kind":       kind,
			},
		}, true
	default:
		return atif.Step{}, false
	}
}

func attachObservationToLastAgentStep(traj *atif.Trajectory, step atif.Step) bool {
	if step.Observation == nil {
		return false
	}
	for i := len(traj.Steps) - 1; i >= 0; i-- {
		if traj.Steps[i].Source != "agent" {
			continue
		}
		if traj.Steps[i].Observation == nil {
			traj.Steps[i].Observation = &atif.Observation{}
		}
		traj.Steps[i].Observation.Results = append(traj.Steps[i].Observation.Results, step.Observation.Results...)
		return true
	}
	return false
}

func isBackgroundCompletionStep(step atif.Step) bool {
	if step.Extra == nil {
		return false
	}
	kind, _ := step.Extra["kind"].(string)
	return isBackgroundRuntimeKind(kind)
}

func backgroundKindFromText(text string) string {
	switch {
	case strings.Contains(text, "background job completed"):
		return "background_job_completed"
	case strings.Contains(text, "background job killed"):
		return "background_job_killed"
	default:
		return ""
	}
}

func isBackgroundRuntimeKind(kind string) bool {
	return kind == "background_job_completed" || kind == "background_job_killed"
}

func cleanTrajectoryText(text string) string {
	return ansi.Strip(text)
}
