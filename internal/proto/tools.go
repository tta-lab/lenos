package proto

// ToolResponseType represents the type of tool response.
type ToolResponseType string

const (
	ToolResponseTypeText  ToolResponseType = "text"
	ToolResponseTypeImage ToolResponseType = "image"
)

// ToolResponse represents a response from a tool.
type ToolResponse struct {
	Type     ToolResponseType `json:"type"`
	Content  string           `json:"content"`
	Metadata string           `json:"metadata,omitempty"`
	IsError  bool             `json:"is_error"`
}

const BashToolName = "bash"

// BashParams represents the parameters for the bash tool.
type BashParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// BashPermissionsParams represents the permission parameters for the bash tool.
type BashPermissionsParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// BashResponseMetadata represents the metadata for a bash tool response.
type BashResponseMetadata struct {
	StartTime        int64  `json:"start_time"`
	EndTime          int64  `json:"end_time"`
	Output           string `json:"output"`
	WorkingDirectory string `json:"working_directory"`
}
