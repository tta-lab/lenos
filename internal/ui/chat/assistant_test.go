package chat

import (
	"testing"
)

func TestStripCmdBlocks(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single block",
			input: "<cmd>ls</cmd>",
			want:  "",
		},
		{
			name:  "prose before and after",
			input: "Here is the plan:\n<cmd>ls -la</cmd>\nDone.",
			want:  "Here is the plan:\n\nDone.",
		},
		{
			name:  "multiple blocks",
			input: "<cmd>ls</cmd> then <cmd>cat foo</cmd>",
			want:  "then",
		},
		{
			name:  "multiline cmd",
			input: "<cmd>ls\n-la</cmd>",
			want:  "",
		},
		{
			name:  "empty block",
			input: "<cmd></cmd>",
			want:  "",
		},
		{
			name:  "no cmd blocks",
			input: "just prose",
			want:  "just prose",
		},
		{
			name:  "partial unclosed tag",
			input: "<cmd>ls",
			want:  "<cmd>ls",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripCmdBlocks(tt.input)
			if got != tt.want {
				t.Errorf("stripCmdBlocks(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
