package project

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// Project holds a ttal project entry.
type Project struct {
	Alias string `json:"alias"`
	Name  string `json:"name"`
	Path  string `json:"path"`
}

// List calls ttal project list --json and returns all active projects.
func List(ctx context.Context) ([]Project, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ttal", "project", "list", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("ttal project list: %w", err)
	}
	var projects []Project
	if err := json.Unmarshal(out, &projects); err != nil {
		return nil, fmt.Errorf("parse ttal project list output: %w", err)
	}
	return projects, nil
}
