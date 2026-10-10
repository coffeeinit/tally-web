package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIProvider covers every OpenAI-compatible endpoint: OpenAI, DeepSeek,
// OpenRouter, Groq, Together, Mistral, vLLM, LM Studio, Ollama and friends.
type OpenAIProvider struct {
	BaseURL string
	APIKey  string
	client  *http.Client
}

func (p *OpenAIProvider) Kind() string { return "openai" }

func (p *OpenAIProvider) httpClient() *http.Client {
	if p.client == nil {
		p.client = &http.Client{Timeout: 180 * time.Second}
	}
	return p.client
}

func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	msgs := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		switch m.Role {
		case "assistant":
			entry := map[string]any{"role": "assistant"}
			if m.Content != "" {
				entry["content"] = m.Content
			}
			if len(m.ToolCalls) > 0 {
				calls := make([]map[string]any, 0, len(m.ToolCalls))
				for _, c := range m.ToolCalls {
					calls = append(calls, map[string]any{
						"id":   c.ID,
						"type": "function",
						"function": map[string]any{
							"name":      c.Name,
							"arguments": orEmptyJSON(c.Args),
						},
					})
				}
				entry["tool_calls"] = calls
			}
			msgs = append(msgs, entry)
		case "tool":
			msgs = append(msgs, map[string]any{
				"role":         "tool",
				"tool_call_id": m.ToolCallID,
				"content":      m.Content,
			})
		default:
			msgs = append(msgs, map[string]any{"role": m.Role, "content": m.Content})
		}
	}

	body := map[string]any{"model": req.Model, "messages": msgs}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.Schema,
				},
			})
		}
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, err
	}

	url := strings.TrimRight(orDefault(p.BaseURL, "https://api.openai.com/v1"), "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := p.httpClient().Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("calling %s: %w", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		return ChatResponse{}, fmt.Errorf("%s returned HTTP %d: %s", url, resp.StatusCode, trim(string(raw), 500))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ChatResponse{}, fmt.Errorf("unreadable response from %s: %s", url, trim(string(raw), 300))
	}
	if parsed.Error != nil {
		return ChatResponse{}, fmt.Errorf("%s: %s", url, parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("%s returned no choices", url)
	}

	msg := parsed.Choices[0].Message
	out := ChatResponse{Content: strings.TrimSpace(msg.Content)}
	for _, c := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:   orDefault(c.ID, randomID()),
			Name: c.Function.Name,
			Args: orEmptyJSON(c.Function.Arguments),
		})
	}
	return out, nil
}

func orEmptyJSON(s string) string {
	if strings.TrimSpace(s) == "" {
		return "{}"
	}
	return s
}

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func randomID() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	now := time.Now().UnixNano()
	for i := range b {
		b[i] = alphabet[(now+int64(i)*7919)%int64(len(alphabet))]
		if b[i] < 0 {
			b[i] = 'a'
		}
	}
	return "call_" + string(b)
}
