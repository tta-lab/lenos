package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"charm.land/fantasy"
	"charm.land/x/vcr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/agent/tools"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/session"

	_ "github.com/joho/godotenv/autoload"
)

var modelPairs = []modelPair{
	{"anthropic-sonnet", anthropicBuilder("claude-sonnet-4-6"), anthropicBuilder("claude-haiku-4-5-20251001")},
	{"openai-gpt-5", openaiBuilder("gpt-5"), openaiBuilder("gpt-4o")},
	{"openrouter-kimi-k2", openRouterBuilder("moonshotai/kimi-k2-0905"), openRouterBuilder("qwen/qwen3-next-80b-a3b-instruct")},
	{"zai-glm4.6", zAIBuilder("glm-4.6"), zAIBuilder("glm-4.5-air")},
}

func getModels(t *testing.T, r *vcr.Recorder, pair modelPair) (fantasy.LanguageModel, fantasy.LanguageModel) {
	large, err := pair.largeModel(t, r)
	require.NoError(t, err)
	small, err := pair.smallModel(t, r)
	require.NoError(t, err)
	return large, small
}

func setupAgent(t *testing.T, pair modelPair) (SessionAgent, fakeEnv) {
	r := vcr.NewRecorder(t)
	large, small := getModels(t, r, pair)
	env := testEnv(t)

	createSimpleGoProject(t, env.workingDir)
	agent, err := coderAgent(r, env, large, small)
	require.NoError(t, err)
	return agent, env
}

func TestCoderAgent(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("skipping VCR integration test (set RUN_INTEGRATION_TESTS=1 to run)")
	}
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows for now")
	}

	for _, pair := range modelPairs {
		t.Run(pair.name, func(t *testing.T) {
			t.Run("simple test", func(t *testing.T) {
				agent, env := setupAgent(t, pair)

				session, err := env.sessions.Create(t.Context(), "New Session")
				require.NoError(t, err)

				res, err := agent.Run(t.Context(), SessionAgentCall{
					Prompt:          "Hello",
					SessionID:       session.ID,
					MaxOutputTokens: 10000,
				})
				require.NoError(t, err)
				assert.NotNil(t, res)

				msgs, err := env.messages.List(t.Context(), session.ID)
				require.NoError(t, err)
				// Should have the agent and user message
				assert.Equal(t, len(msgs), 2)
			})
			t.Run("read a file", func(t *testing.T) {
				agent, env := setupAgent(t, pair)

				session, err := env.sessions.Create(t.Context(), "New Session")
				require.NoError(t, err)
				res, err := agent.Run(t.Context(), SessionAgentCall{
					Prompt:          "Read the go mod",
					SessionID:       session.ID,
					MaxOutputTokens: 10000,
				})

				require.NoError(t, err)
				assert.NotNil(t, res)

				msgs, err := env.messages.List(t.Context(), session.ID)
				require.NoError(t, err)
				foundFile := false
				var tcID string
			out:
				for _, msg := range msgs {
					if msg.Role == message.Assistant {
						for _, tc := range msg.ToolCalls() {
							if tc.Name == tools.ViewToolName {
								tcID = tc.ID
							}
						}
					}
					if msg.Role == message.Tool {
						for _, tr := range msg.ToolResults() {
							if tr.ToolCallID == tcID {
								if strings.Contains(tr.Content, "module example.com/testproject") {
									foundFile = true
									break out
								}
							}
						}
					}
				}
				require.True(t, foundFile)
			})
			t.Run("update a file", func(t *testing.T) {
				agent, env := setupAgent(t, pair)

				session, err := env.sessions.Create(t.Context(), "New Session")
				require.NoError(t, err)

				res, err := agent.Run(t.Context(), SessionAgentCall{
					Prompt:          "update the main.go file by changing the print to say hello from crush",
					SessionID:       session.ID,
					MaxOutputTokens: 10000,
				})
				require.NoError(t, err)
				assert.NotNil(t, res)

				msgs, err := env.messages.List(t.Context(), session.ID)
				require.NoError(t, err)

				foundRead := false
				foundWrite := false
				var readTCID, writeTCID string

				for _, msg := range msgs {
					if msg.Role == message.Assistant {
						for _, tc := range msg.ToolCalls() {
							if tc.Name == tools.ViewToolName {
								readTCID = tc.ID
							}
							if tc.Name == tools.EditToolName || tc.Name == tools.WriteToolName {
								writeTCID = tc.ID
							}
						}
					}
					if msg.Role == message.Tool {
						for _, tr := range msg.ToolResults() {
							if tr.ToolCallID == readTCID {
								foundRead = true
							}
							if tr.ToolCallID == writeTCID {
								foundWrite = true
							}
						}
					}
				}

				require.True(t, foundRead, "Expected to find a read operation")
				require.True(t, foundWrite, "Expected to find a write operation")

				mainGoPath := filepath.Join(env.workingDir, "main.go")
				content, err := os.ReadFile(mainGoPath)
				require.NoError(t, err)
				require.Contains(t, strings.ToLower(string(content)), "hello from crush")
			})
			t.Run("bash tool", func(t *testing.T) {
				agent, env := setupAgent(t, pair)

				session, err := env.sessions.Create(t.Context(), "New Session")
				require.NoError(t, err)

				res, err := agent.Run(t.Context(), SessionAgentCall{
					Prompt:          "use bash to create a file named test.txt with content 'hello bash'. do not print its timestamp",
					SessionID:       session.ID,
					MaxOutputTokens: 10000,
				})
				require.NoError(t, err)
				assert.NotNil(t, res)

				msgs, err := env.messages.List(t.Context(), session.ID)
				require.NoError(t, err)

				foundBash := false
				var bashTCID string

				for _, msg := range msgs {
					if msg.Role == message.Assistant {
						for _, tc := range msg.ToolCalls() {
							if tc.Name == tools.BashToolName {
								bashTCID = tc.ID
							}
						}
					}
					if msg.Role == message.Tool {
						for _, tr := range msg.ToolResults() {
							if tr.ToolCallID == bashTCID {
								foundBash = true
							}
						}
					}
				}

				require.True(t, foundBash, "Expected to find a bash operation")

				testFilePath := filepath.Join(env.workingDir, "test.txt")
				content, err := os.ReadFile(testFilePath)
				require.NoError(t, err)
				require.Contains(t, string(content), "hello bash")
			})
			t.Run("download tool", func(t *testing.T) {
				agent, env := setupAgent(t, pair)

				session, err := env.sessions.Create(t.Context(), "New Session")
				require.NoError(t, err)

				res, err := agent.Run(t.Context(), SessionAgentCall{
					Prompt:          "download the file from https://example-files.online-convert.com/document/txt/example.txt and save it as example.txt",
					SessionID:       session.ID,
					MaxOutputTokens: 10000,
				})
				require.NoError(t, err)
				assert.NotNil(t, res)

				msgs, err := env.messages.List(t.Context(), session.ID)
				require.NoError(t, err)

				foundDownload := false
				var downloadTCID string

				for _, msg := range msgs {
					if msg.Role == message.Assistant {
						for _, tc := range msg.ToolCalls() {
							if tc.Name == tools.DownloadToolName {
								downloadTCID = tc.ID
							}
						}
					}
					if msg.Role == message.Tool {
						for _, tr := range msg.ToolResults() {
							if tr.ToolCallID == downloadTCID {
								foundDownload = true
							}
						}
					}
				}

				require.True(t, foundDownload, "Expected to find a download operation")

				examplePath := filepath.Join(env.workingDir, "example.txt")
				_, err = os.Stat(examplePath)
				require.NoError(t, err, "Expected example.txt file to exist")
			})
			t.Run("fetch tool", func(t *testing.T) {
				agent, env := setupAgent(t, pair)

				session, err := env.sessions.Create(t.Context(), "New Session")
				require.NoError(t, err)

				res, err := agent.Run(t.Context(), SessionAgentCall{
					Prompt:          "fetch the content from https://example-files.online-convert.com/website/html/example.html and tell me if it contains the word 'John Doe'",
					SessionID:       session.ID,
					MaxOutputTokens: 10000,
				})
				require.NoError(t, err)
				assert.NotNil(t, res)

				msgs, err := env.messages.List(t.Context(), session.ID)
				require.NoError(t, err)

				foundFetch := false
				var fetchTCID string

				for _, msg := range msgs {
					if msg.Role == message.Assistant {
						for _, tc := range msg.ToolCalls() {
							if tc.Name == tools.FetchToolName {
								fetchTCID = tc.ID
							}
						}
					}
					if msg.Role == message.Tool {
						for _, tr := range msg.ToolResults() {
							if tr.ToolCallID == fetchTCID {
								foundFetch = true
							}
						}
					}
				}

				require.True(t, foundFetch, "Expected to find a fetch operation")
			})
			t.Run("glob tool", func(t *testing.T) {
				agent, env := setupAgent(t, pair)

				session, err := env.sessions.Create(t.Context(), "New Session")
				require.NoError(t, err)

				res, err := agent.Run(t.Context(), SessionAgentCall{
					Prompt:          "use glob to find all .go files in the current directory",
					SessionID:       session.ID,
					MaxOutputTokens: 10000,
				})
				require.NoError(t, err)
				assert.NotNil(t, res)

				msgs, err := env.messages.List(t.Context(), session.ID)
				require.NoError(t, err)

				foundGlob := false
				var globTCID string

				for _, msg := range msgs {
					if msg.Role == message.Assistant {
						for _, tc := range msg.ToolCalls() {
							if tc.Name == tools.GlobToolName {
								globTCID = tc.ID
							}
						}
					}
					if msg.Role == message.Tool {
						for _, tr := range msg.ToolResults() {
							if tr.ToolCallID == globTCID {
								foundGlob = true
								require.Contains(t, tr.Content, "main.go", "Expected glob to find main.go")
							}
						}
					}
				}

				require.True(t, foundGlob, "Expected to find a glob operation")
			})
			t.Run("grep tool", func(t *testing.T) {
				agent, env := setupAgent(t, pair)

				session, err := env.sessions.Create(t.Context(), "New Session")
				require.NoError(t, err)

				res, err := agent.Run(t.Context(), SessionAgentCall{
					Prompt:          "use grep to search for the word 'package' in go files",
					SessionID:       session.ID,
					MaxOutputTokens: 10000,
				})
				require.NoError(t, err)
				assert.NotNil(t, res)

				msgs, err := env.messages.List(t.Context(), session.ID)
				require.NoError(t, err)

				foundGrep := false
				var grepTCID string

				for _, msg := range msgs {
					if msg.Role == message.Assistant {
						for _, tc := range msg.ToolCalls() {
							if tc.Name == tools.GrepToolName {
								grepTCID = tc.ID
							}
						}
					}
					if msg.Role == message.Tool {
						for _, tr := range msg.ToolResults() {
							if tr.ToolCallID == grepTCID {
								foundGrep = true
								require.Contains(t, tr.Content, "main.go", "Expected grep to find main.go")
							}
						}
					}
				}

				require.True(t, foundGrep, "Expected to find a grep operation")
			})
			t.Run("ls tool", func(t *testing.T) {
				agent, env := setupAgent(t, pair)

				session, err := env.sessions.Create(t.Context(), "New Session")
				require.NoError(t, err)

				res, err := agent.Run(t.Context(), SessionAgentCall{
					Prompt:          "use ls to list the files in the current directory",
					SessionID:       session.ID,
					MaxOutputTokens: 10000,
				})
				require.NoError(t, err)
				assert.NotNil(t, res)

				msgs, err := env.messages.List(t.Context(), session.ID)
				require.NoError(t, err)

				foundLS := false
				var lsTCID string

				for _, msg := range msgs {
					if msg.Role == message.Assistant {
						for _, tc := range msg.ToolCalls() {
							if tc.Name == tools.LSToolName {
								lsTCID = tc.ID
							}
						}
					}
					if msg.Role == message.Tool {
						for _, tr := range msg.ToolResults() {
							if tr.ToolCallID == lsTCID {
								foundLS = true
								require.Contains(t, tr.Content, "main.go", "Expected ls to list main.go")
								require.Contains(t, tr.Content, "go.mod", "Expected ls to list go.mod")
							}
						}
					}
				}

				require.True(t, foundLS, "Expected to find an ls operation")
			})
			t.Run("sourcegraph tool", func(t *testing.T) {
				agent, env := setupAgent(t, pair)

				session, err := env.sessions.Create(t.Context(), "New Session")
				require.NoError(t, err)

				res, err := agent.Run(t.Context(), SessionAgentCall{
					Prompt:          "use sourcegraph to search for 'func main' in Go repositories",
					SessionID:       session.ID,
					MaxOutputTokens: 10000,
				})
				require.NoError(t, err)
				assert.NotNil(t, res)

				msgs, err := env.messages.List(t.Context(), session.ID)
				require.NoError(t, err)

				foundSourcegraph := false
				var sourcegraphTCID string

				for _, msg := range msgs {
					if msg.Role == message.Assistant {
						for _, tc := range msg.ToolCalls() {
							if tc.Name == tools.SourcegraphToolName {
								sourcegraphTCID = tc.ID
							}
						}
					}
					if msg.Role == message.Tool {
						for _, tr := range msg.ToolResults() {
							if tr.ToolCallID == sourcegraphTCID {
								foundSourcegraph = true
							}
						}
					}
				}

				require.True(t, foundSourcegraph, "Expected to find a sourcegraph operation")
			})
			t.Run("write tool", func(t *testing.T) {
				agent, env := setupAgent(t, pair)

				session, err := env.sessions.Create(t.Context(), "New Session")
				require.NoError(t, err)

				res, err := agent.Run(t.Context(), SessionAgentCall{
					Prompt:          "use write to create a new file called config.json with content '{\"name\": \"test\", \"version\": \"1.0.0\"}'",
					SessionID:       session.ID,
					MaxOutputTokens: 10000,
				})
				require.NoError(t, err)
				assert.NotNil(t, res)

				msgs, err := env.messages.List(t.Context(), session.ID)
				require.NoError(t, err)

				foundWrite := false
				var writeTCID string

				for _, msg := range msgs {
					if msg.Role == message.Assistant {
						for _, tc := range msg.ToolCalls() {
							if tc.Name == tools.WriteToolName {
								writeTCID = tc.ID
							}
						}
					}
					if msg.Role == message.Tool {
						for _, tr := range msg.ToolResults() {
							if tr.ToolCallID == writeTCID {
								foundWrite = true
							}
						}
					}
				}

				require.True(t, foundWrite, "Expected to find a write operation")

				configPath := filepath.Join(env.workingDir, "config.json")
				content, err := os.ReadFile(configPath)
				require.NoError(t, err)
				require.Contains(t, string(content), "test", "Expected config.json to contain 'test'")
				require.Contains(t, string(content), "1.0.0", "Expected config.json to contain '1.0.0'")
			})
			t.Run("parallel tool calls", func(t *testing.T) {
				agent, env := setupAgent(t, pair)

				session, err := env.sessions.Create(t.Context(), "New Session")
				require.NoError(t, err)

				res, err := agent.Run(t.Context(), SessionAgentCall{
					Prompt:          "use glob to find all .go files and use ls to list the current directory, it is very important that you run both tool calls in parallel",
					SessionID:       session.ID,
					MaxOutputTokens: 10000,
				})
				require.NoError(t, err)
				assert.NotNil(t, res)

				msgs, err := env.messages.List(t.Context(), session.ID)
				require.NoError(t, err)

				var assistantMsg *message.Message
				var toolMsgs []message.Message

				for _, msg := range msgs {
					if msg.Role == message.Assistant && len(msg.ToolCalls()) > 0 {
						assistantMsg = &msg
					}
					if msg.Role == message.Tool {
						toolMsgs = append(toolMsgs, msg)
					}
				}

				require.NotNil(t, assistantMsg, "Expected to find an assistant message with tool calls")
				require.NotNil(t, toolMsgs, "Expected to find a tool message")

				toolCalls := assistantMsg.ToolCalls()
				require.GreaterOrEqual(t, len(toolCalls), 2, "Expected at least 2 tool calls in parallel")

				foundGlob := false
				foundLS := false
				var globTCID, lsTCID string

				for _, tc := range toolCalls {
					if tc.Name == tools.GlobToolName {
						foundGlob = true
						globTCID = tc.ID
					}
					if tc.Name == tools.LSToolName {
						foundLS = true
						lsTCID = tc.ID
					}
				}

				require.True(t, foundGlob, "Expected to find a glob tool call")
				require.True(t, foundLS, "Expected to find an ls tool call")

				require.GreaterOrEqual(t, len(toolMsgs), 2, "Expected at least 2 tool results in the same message")

				foundGlobResult := false
				foundLSResult := false

				for _, msg := range toolMsgs {
					for _, tr := range msg.ToolResults() {
						if tr.ToolCallID == globTCID {
							foundGlobResult = true
							require.Contains(t, tr.Content, "main.go", "Expected glob result to contain main.go")
							require.False(t, tr.IsError, "Expected glob result to not be an error")
						}
						if tr.ToolCallID == lsTCID {
							foundLSResult = true
							require.Contains(t, tr.Content, "main.go", "Expected ls result to contain main.go")
							require.False(t, tr.IsError, "Expected ls result to not be an error")
						}
					}
				}

				require.True(t, foundGlobResult, "Expected to find glob tool result")
				require.True(t, foundLSResult, "Expected to find ls tool result")
			})
		})
	}
}

func makeTestTodos(n int) []session.Todo {
	todos := make([]session.Todo, n)
	for i := range n {
		todos[i] = session.Todo{
			Status:  session.TodoStatusPending,
			Content: fmt.Sprintf("Task %d: Implement feature with some description that makes it realistic", i),
		}
	}
	return todos
}

func TestBuildSummaryPrompt(t *testing.T) {
	t.Parallel()

	t.Run("empty jobID returns base prompt without todos section", func(t *testing.T) {
		t.Parallel()
		result := buildSummaryPrompt(context.Background(), "")
		require.Contains(t, result, "Provide a detailed summary of our conversation above.")
		require.NotContains(t, result, "Current Todo List")
	})

	t.Run("cancelled context returns base prompt", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result := buildSummaryPrompt(ctx, "fake-nonexistent-job-id")
		require.Contains(t, result, "Provide a detailed summary of our conversation above.")
		require.NotContains(t, result, "Current Todo List")
	})

	t.Run("successful poll with real jobID returns prompt", func(t *testing.T) {
		t.Parallel()
		// Use a UUID unlikely to have subtasks in a fresh test environment.
		// task will return empty output, so todos section won't appear but
		// the base prompt will.
		result := buildSummaryPrompt(context.Background(), "00000000-0000-0000-0000-000000000000")
		require.Contains(t, result, "Provide a detailed summary of our conversation above.")
	})
}

func TestGenerateTitle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("task CLI not available on windows")
	}
	if _, err := os.LookupEnv("TTAL_JOB_ID"); !err {
		t.Setenv("TTAL_JOB_ID", "25620b89")
	}

	t.Run("uses task description as session title", func(t *testing.T) {
		env := testEnv(t)
		sess, err := env.sessions.Create(t.Context(), "Untitled Session")
		require.NoError(t, err)

		// Create a fake "task" binary that outputs a JSON array.
		tmp := t.TempDir()
		oldPath := os.Getenv("PATH")
		t.Cleanup(func() { os.Setenv("PATH", oldPath) })
		os.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath)

		fakeTask := filepath.Join(tmp, "task")
		require.NoError(t, os.WriteFile(fakeTask, []byte(fmt.Sprintf(`#!/bin/sh
printf '[{"description":"%s","status":"pending"}]' "$@"
`, "Fix the authentication bug")), 0o755))

		a := &sessionAgent{sessions: env.sessions}
		a.generateTitle(t.Context(), sess.ID, "fix auth bug")

		updated, err := env.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		assert.Equal(t, "Fix the authentication bug", updated.Title)
	})

	t.Run("empty array falls back to default", func(t *testing.T) {
		env := testEnv(t)
		sess, err := env.sessions.Create(t.Context(), "Untitled Session")
		require.NoError(t, err)

		tmp := t.TempDir()
		oldPath := os.Getenv("PATH")
		t.Cleanup(func() { os.Setenv("PATH", oldPath) })
		os.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath)

		fakeTask := filepath.Join(tmp, "task")
		require.NoError(t, os.WriteFile(fakeTask, []byte("#!/bin/sh\nprintf '[]' \"$@\""), 0o755))

		a := &sessionAgent{sessions: env.sessions}
		a.generateTitle(t.Context(), sess.ID, "")

		updated, err := env.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		assert.Equal(t, DefaultSessionName, updated.Title)
	})

	t.Run("no TTAL_JOB_ID uses default", func(t *testing.T) {
		env := testEnv(t)
		sess, err := env.sessions.Create(t.Context(), "Untitled Session")
		require.NoError(t, err)

		t.Setenv("TTAL_JOB_ID", "")

		a := &sessionAgent{sessions: env.sessions}
		a.generateTitle(t.Context(), sess.ID, "")

		updated, err := env.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		assert.Equal(t, DefaultSessionName, updated.Title)
	})

	t.Run("description over 100 chars is truncated", func(t *testing.T) {
		env := testEnv(t)
		sess, err := env.sessions.Create(t.Context(), "Untitled Session")
		require.NoError(t, err)

		tmp := t.TempDir()
		oldPath := os.Getenv("PATH")
		t.Cleanup(func() { os.Setenv("PATH", oldPath) })
		os.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath)

		longDesc := strings.Repeat("x", 150)
		fakeTask := filepath.Join(tmp, "task")
		require.NoError(t, os.WriteFile(fakeTask, []byte(fmt.Sprintf("#!/bin/sh\nprintf '[{\"description\":\"%s\",\"status\":\"pending\"}]' \"$@\"", longDesc)), 0o755))

		a := &sessionAgent{sessions: env.sessions}
		a.generateTitle(t.Context(), sess.ID, "")

		updated, err := env.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		assert.Len(t, updated.Title, 100)
		assert.Equal(t, longDesc[:100], updated.Title)
	})
}
