package tools

import (
	"bytes"
	"context"
	_ "embed"
	"html/template"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/fsext"
	"github.com/tta-lab/lenos/internal/shell"
)

type BashParams struct {
	Description string `json:"description" description:"A brief description of what the command does, try to keep it under 30 characters or so"`
	Command     string `json:"command" description:"The command to execute"`
	WorkingDir  string `json:"working_dir,omitempty" description:"The working directory to execute the command in (defaults to current directory)"`
}

type BashResponseMetadata struct {
	StartTime        int64  `json:"start_time"`
	EndTime          int64  `json:"end_time"`
	Output           string `json:"output"`
	Description      string `json:"description"`
	WorkingDirectory string `json:"working_directory"`
}

const (
	BashToolName   = "bash"
	MaxOutputLength = 30000
	BashNoOutput   = "no output"
)

//go:embed bash.tpl
var bashDescriptionTmpl []byte

var bashDescriptionTpl = template.Must(
	template.New("bashDescription").
		Parse(string(bashDescriptionTmpl)),
)

type bashDescriptionData struct {
	MaxOutputLength int
	Attribution    config.Attribution
	ModelName      string
}

func bashDescription(attribution *config.Attribution, modelName string) string {
	var out bytes.Buffer
	if err := bashDescriptionTpl.Execute(&out, bashDescriptionData{
		MaxOutputLength: MaxOutputLength,
		Attribution:    *attribution,
		ModelName:      modelName,
	}); err != nil {
		// this should never happen.
		panic("failed to execute bash description template: " + err.Error())
	}
	return out.String()
}

func NewBashTool(workingDir string, attribution *config.Attribution, modelName string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		BashToolName,
		bashDescription(attribution, modelName),
		func(ctx context.Context, params BashParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Command == "" {
				return fantasy.NewTextErrorResponse("missing command"), nil
			}

			execWorkingDir := params.WorkingDir
			if execWorkingDir == "" {
				execWorkingDir = workingDir
			}

			startTime := time.Now()

			sh := shell.NewShell(&shell.Options{
				WorkingDir: execWorkingDir,
			})

			stdout, stderr, execErr := sh.Exec(ctx, params.Command)

			elapsed := time.Since(startTime)

			stdout = formatOutput(stdout, stderr, execErr)

			metadata := BashResponseMetadata{
				StartTime:        startTime.UnixMilli(),
				EndTime:          time.Now().UnixMilli(),
				Output:           stdout,
				Description:      params.Description,
				WorkingDirectory: normalizeWorkingDir(execWorkingDir),
			}

			if stdout == "" {
				return fantasy.WithResponseMetadata(fantasy.NewTextResponse(BashNoOutput), metadata), nil
			}

			_ = elapsed // silence unused variable warning
			stdout += fmt.Sprintf("\n\n<cwd>%s</cwd>", normalizeWorkingDir(execWorkingDir))
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(stdout), metadata), nil
		})
}

// formatOutput formats the output of a completed command with error handling.
func formatOutput(stdout, stderr string, execErr error) string {
	interrupted := shell.IsInterrupt(execErr)
	exitCode := shell.ExitCode(execErr)

	stdout = truncateOutput(stdout)
	stderr = truncateOutput(stderr)

	errorMessage := stderr
	if errorMessage == "" && execErr != nil {
		errorMessage = execErr.Error()
	}

	if interrupted {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += "Command was aborted before completion"
	} else if exitCode != 0 {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += fmt.Sprintf("Exit code %d", exitCode)
	}

	hasBothOutputs := stdout != "" && stderr != ""

	if hasBothOutputs {
		stdout += "\n"
	}

	if errorMessage != "" {
		stdout += "\n" + errorMessage
	}

	return stdout
}

func truncateOutput(content string) string {
	if len(content) <= MaxOutputLength {
		return content
	}

	halfLength := MaxOutputLength / 2
	start := content[:halfLength]
	end := content[len(content)-halfLength:]

	truncatedLinesCount := countLines(content[halfLength : len(content)-halfLength])
	return fmt.Sprintf("%s\n\n... [%d lines truncated] ...\n\n%s", start, truncatedLinesCount, end)
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

func normalizeWorkingDir(path string) string {
	if runtime.GOOS == "windows" {
		path = strings.ReplaceAll(path, fsext.WindowsWorkingDirDrive(), "")
	}
	return filepath.ToSlash(path)
}
