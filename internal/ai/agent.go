package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Event is one step of the assistant's work, streamed to the browser so the
// user watches the AI actually use Tally rather than staring at a spinner.
type Event struct {
	Type    string `json:"type"` // status | assistant | tool_call | tool_result | error | done
	Step    int    `json:"step,omitempty"`
	Text    string `json:"text,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Args    string `json:"args,omitempty"`
	Preview string `json:"preview,omitempty"`
	IsError bool   `json:"isError,omitempty"`
}

const maxToolResultBytes = 24000

// Agent runs the provider/tool loop.
type Agent struct {
	Provider Provider
	Settings Settings
	Tools    *Toolbox
}

func (a *Agent) maxSteps() int {
	if a.Settings.MaxSteps <= 0 {
		return 6
	}
	return a.Settings.MaxSteps
}

// Run executes the loop and streams events. It returns the final assistant text.
func (a *Agent) Run(ctx context.Context, history []Message, emit func(Event)) (string, error) {
	system := strings.TrimSpace(a.Settings.SystemPrompt)
	if system == "" {
		system = "You are the accounting copilot inside a Tally workspace. Use the tools to read real data before answering. Never invent figures."
	}
	system += fmt.Sprintf("\n\nConnected instance: %s. Selected company: %s. Writes are %s.",
		orDefault(a.Tools.Instance, "default"),
		orDefault(a.Tools.Company, "(none selected)"),
		enabledWord(a.Tools.AllowWrites))

	messages := []Message{{Role: "system", Content: system}}
	messages = append(messages, history...)
	specs := a.Tools.Specs()

	final := ""
	for step := 0; step < a.maxSteps(); step++ {
		if err := ctx.Err(); err != nil {
			return final, err
		}
		emit(Event{Type: "status", Step: step + 1, Text: "thinking"})

		resp, err := a.Provider.Chat(ctx, ChatRequest{
			Model:    a.Settings.Model,
			Messages: messages,
			Tools:    specs,
		})
		if err != nil {
			emit(Event{Type: "error", Text: err.Error()})
			return final, err
		}
		if strings.TrimSpace(resp.Content) != "" {
			final = strings.TrimSpace(resp.Content)
			emit(Event{Type: "assistant", Step: step + 1, Text: final})
		}
		if len(resp.ToolCalls) == 0 {
			emit(Event{Type: "done", Text: final})
			return final, nil
		}

		messages = append(messages, Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		for _, call := range resp.ToolCalls {
			emit(Event{Type: "tool_call", Step: step + 1, Tool: call.Name, Args: call.Args})
			result, err := a.Tools.Execute(ctx, call.Name, call.Args)
			if err != nil {
				payload, _ := json.Marshal(map[string]any{"error": err.Error()})
				result = string(payload)
				emit(Event{Type: "tool_result", Step: step + 1, Tool: call.Name, Preview: err.Error(), IsError: true})
			} else {
				emit(Event{Type: "tool_result", Step: step + 1, Tool: call.Name, Preview: preview(result)})
			}
			if len(result) > maxToolResultBytes {
				result = result[:maxToolResultBytes] + "...(truncated)"
			}
			messages = append(messages, Message{
				Role:       "tool",
				ToolCallID: call.ID,
				ToolName:   call.Name,
				Content:    result,
			})
		}
	}

	emit(Event{Type: "error", Text: fmt.Sprintf("stopped after %d steps", a.maxSteps())})
	return final, nil
}

func preview(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 220 {
		return s
	}
	return s[:220] + "..."
}

func enabledWord(b bool) string {
	if b {
		return "ENABLED (the assistant may create ledgers and vouchers)"
	}
	return "disabled (read-only)"
}
