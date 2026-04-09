package project

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// Project holds a ttal project entry.
type Project struct {
	Alias string `json:"alias"`
	Name  string `json:"name"`
	Path  string `json:"path"`
}

// List calls ttal project list --json and returns all active projects.
func List() ([]Project, error) {
	out, err := exec.CommandContext(context.Background(), "ttal", "project", "list", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("ttal project list: %w", err)
	}
	var projects []Project
	if err := json.Unmarshal(out, &projects); err != nil {
		return nil, fmt.Errorf("parse ttal project list output: %w", err)
	}
	return projects, nil
}
