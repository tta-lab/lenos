package chat

// CommandParams holds parameters for a command execution.
type CommandParams struct {
	Description string `json:"description"`
	Command     string `json:"command"`
	WorkingDir  string `json:"working_dir,omitempty"`
}

// CommandResponseMetadata holds metadata from a command execution.
type CommandResponseMetadata struct {
	StartTime        int64  `json:"start_time"`
	EndTime          int64  `json:"end_time"`
	Output           string `json:"output"`
	Description      string `json:"description"`
	WorkingDirectory string `json:"working_directory"`
}

// CommandToolName is the name used for command tool items in the UI.
const CommandToolName = "command"

// CommandNoOutput is the placeholder when a command has no output.
const CommandNoOutput = "no output"
