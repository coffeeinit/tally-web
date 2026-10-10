package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GeminiProvider speaks generateContent with function declarations.
type GeminiProvider struct {
	BaseURL string
	APIKey  string
	client  *http.Client
}

func (p *GeminiProvider) Kind() string { return "gemini" }

func (p *GeminiProvider) httpClient() *http.Client {
	if p.client == nil {
		p.client = &http.Client{Timeout: 180 * time.Second}
	}
	return p.client
}

func (p *GeminiProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	system := ""
	type part map[string]any
	var contents []map[string]any

	appendParts := func(role string, parts []part) {
		if len(parts) == 0 {
			return
		}
		if n := len(contents); n > 0 && contents[n-1]["role"] == role {
			existing, _ := contents[n-1]["parts"].([]part)
			contents[n-1]["parts"] = append(existing, parts...)
			return
		}
		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}

	for i := 0; i < len(req.Messages); i++ {
		m := req.Messages[i]
		switch m.Role {
		case "system":
			system = strings.TrimSpace(system + "\n" + m.Content)
		case "tool":
			var parts []part
			for i < len(req.Messages) && req.Messages[i].Role == "tool" {
				t := req.Messages[i]
				var payload any = map[string]any{"result": t.Content}
				if json.Unmarshal([]byte(t.Content), &payload) != nil {
					payload = map[string]any{"result": t.Content}
				}
				parts = append(parts, part{"functionResponse": map[string]any{
					"name":     t.ToolName,
					"response": payload,
				}})
				i++
			}
			i--
			appendParts("user", parts)
		case "assistant":
			var parts []part
			if m.Content != "" {
				parts = append(parts, part{"text": m.Content})
			}
			for _, c := range m.ToolCalls {
				var args any = map[string]any{}
				_ = json.Unmarshal([]byte(orEmptyJSON(c.Args)), &args)
				parts = append(parts, part{"functionCall": map[string]any{"name": c.Name, "args": args}})
			}
			appendParts("model", parts)
		default:
			appendParts("user", []part{{"text": m.Content}})
		}
	}

	body := map[string]any{"contents": contents}
	if system != "" {
		body["systemInstruction"] = map[string]any{"parts": []part{{"text": system}}}
	}
	if len(req.Tools) > 0 {
		decls := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			decls = append(decls, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  geminiSchema(t.Schema),
			})
		}
		body["tools"] = []map[string]any{{"functionDeclarations": decls}}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, err
	}
	base := strings.TrimRight(orDefault(p.BaseURL, "https://generativelanguage.googleapis.com"), "/")
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", base, url.PathEscape(req.Model))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", p.APIKey)

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
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name string          `json:"name"`
						Args json.RawMessage `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
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
	if len(parsed.Candidates) == 0 {
		return ChatResponse{}, fmt.Errorf("%s returned no candidates", url)
	}

	var out ChatResponse
	for _, p := range parsed.Candidates[0].Content.Parts {
		if p.FunctionCall != nil {
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:   randomID(),
				Name: p.FunctionCall.Name,
				Args: orEmptyJSON(string(p.FunctionCall.Args)),
			})
			continue
		}
		out.Content += p.Text
	}
	out.Content = strings.TrimSpace(out.Content)
	return out, nil
}

// geminiSchema strips JSON Schema keywords Gemini rejects.
func geminiSchema(schema map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range schema {
		switch k {
		case "$schema", "additionalProperties", "strict":
			continue
		}
		switch t := v.(type) {
		case map[string]any:
			out[k] = geminiSchema(t)
		case []any:
			items := make([]any, 0, len(t))
			for _, item := range t {
				if m, ok := item.(map[string]any); ok {
					items = append(items, geminiSchema(m))
				} else {
					items = append(items, item)
				}
			}
			out[k] = items
		default:
			out[k] = v
		}
	}
	return out
}
