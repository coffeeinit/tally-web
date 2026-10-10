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

// AnthropicProvider speaks the native Messages API, including tool use.
type AnthropicProvider struct {
	BaseURL string
	APIKey  string
	client  *http.Client
}

func (p *AnthropicProvider) Kind() string { return "anthropic" }

func (p *AnthropicProvider) httpClient() *http.Client {
	if p.client == nil {
		p.client = &http.Client{Timeout: 180 * time.Second}
	}
	return p.client
}

func (p *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	system := ""
	var msgs []map[string]any

	for i := 0; i < len(req.Messages); i++ {
		m := req.Messages[i]
		switch m.Role {
		case "system":
			system = strings.TrimSpace(system + "\n" + m.Content)
		case "tool":
			// Anthropic requires every tool_result for one assistant turn to
			// arrive in a single user message, so collect consecutive results.
			var blocks []map[string]any
			for i < len(req.Messages) && req.Messages[i].Role == "tool" {
				t := req.Messages[i]
				blocks = append(blocks, map[string]any{
					"type":        "tool_result",
					"tool_use_id": t.ToolCallID,
					"content":     t.Content,
				})
				i++
			}
			i--
			msgs = append(msgs, map[string]any{"role": "user", "content": blocks})
		case "assistant":
			var blocks []map[string]any
			if m.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, c := range m.ToolCalls {
				var input any = map[string]any{}
				_ = json.Unmarshal([]byte(orEmptyJSON(c.Args)), &input)
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    c.ID,
					"name":  c.Name,
					"input": input,
				})
			}
			if len(blocks) == 0 {
				blocks = append(blocks, map[string]any{"type": "text", "text": ""})
			}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": blocks})
		default:
			msgs = append(msgs, map[string]any{"role": "user", "content": m.Content})
		}
	}

	body := map[string]any{"model": req.Model, "max_tokens": 4096, "messages": msgs}
	if system != "" {
		body["system"] = system
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.Schema,
			})
		}
		body["tools"] = tools
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, err
	}
	base := strings.TrimRight(orDefault(p.BaseURL, "https://api.anthropic.com"), "/")
	url := base + "/v1/messages"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

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
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
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

	var out ChatResponse
	for _, block := range parsed.Content {
		switch block.Type {
		case "text":
			out.Content += block.Text
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:   orDefault(block.ID, randomID()),
				Name: block.Name,
				Args: orEmptyJSON(string(block.Input)),
			})
		}
	}
	out.Content = strings.TrimSpace(out.Content)
	return out, nil
}
