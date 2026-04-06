package skills

import (
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
)

// TTALPrefix is the path prefix for ttal-backed skill files.
const TTALPrefix = "ttal://skills/"

// ttalSkillEntry matches the JSON output of `ttal skill list --json`.
type ttalSkillEntry struct {
	Name        string `json:"name"`
	FlicknoteID string `json:"flicknote_id"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

// DiscoverTTAL finds skills registered in the ttal skill registry.
// Returns nil (no error) if the ttal binary is not available.
func DiscoverTTAL(ctx context.Context) []*Skill {
	out, err := exec.CommandContext(ctx, "ttal", "skill", "list", "--all", "--json").Output()
	if err != nil {
		slog.Debug("ttal skill discovery unavailable", "error", err)
		return nil
	}
	return parseTTALSkills(out)
}

// parseTTALSkills converts raw JSON from `ttal skill list --json` into Skill structs.
func parseTTALSkills(data []byte) []*Skill {
	var entries []ttalSkillEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	skills := make([]*Skill, 0, len(entries))
	for _, e := range entries {
		skills = append(skills, &Skill{
			Name:          e.Name,
			Description:   e.Description,
			SkillFilePath: TTALPrefix + e.Name,
			Path:          TTALPrefix + e.Name,
		})
	}
	return skills
}
