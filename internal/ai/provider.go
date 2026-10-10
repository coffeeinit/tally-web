// Package ai makes the assistant provider-agnostic.
//
// The product does not ship an AI and does not proxy one. The customer pastes
// their own token, picks a provider, and the assistant runs against it. That
// keeps cost, data residency and model choice with the customer.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"` // always a JSON object, as a string
}

type Message struct {
	Role       string     `json:"role"` // system | user | assistant | tool
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
}

type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
}

type ChatRequest struct {
	Model    string
	Messages []Message
	Tools    []ToolSpec
}

type ChatResponse struct {
	Content   string
	ToolCalls []ToolCall
}

// Provider is implemented by every supported vendor. Adding a vendor means
// adding one file, not touching the agent or the UI.
type Provider interface {
	Kind() string
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

// Settings is the customer's AI configuration. The token lives here and is
// never sent back to the browser.
type Settings struct {
	Kind         string `json:"kind"`
	BaseURL      string `json:"baseUrl"`
	Model        string `json:"model"`
	APIKey       string `json:"apiKey"`
	MaxSteps     int    `json:"maxSteps"`
	SystemPrompt string `json:"systemPrompt"`
	AllowWrites  bool   `json:"allowWrites"`
}

// SettingsView is the masked shape returned to the client.
type SettingsView struct {
	Kind         string `json:"kind"`
	BaseURL      string `json:"baseUrl"`
	Model        string `json:"model"`
	MaxSteps     int    `json:"maxSteps"`
	SystemPrompt string `json:"systemPrompt"`
	AllowWrites  bool   `json:"allowWrites"`
	APIKeySet    bool   `json:"apiKeySet"`
	APIKeyHint   string `json:"apiKeyHint,omitempty"`
}

func (s Settings) View() SettingsView {
	return SettingsView{
		Kind:         s.Kind,
		BaseURL:      s.BaseURL,
		Model:        s.Model,
		MaxSteps:     s.MaxSteps,
		SystemPrompt: s.SystemPrompt,
		AllowWrites:  s.AllowWrites,
		APIKeySet:    s.APIKey != "",
		APIKeyHint:   hint(s.APIKey),
	}
}

func hint(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// Preset is a known-good provider configuration offered in the UI.
type Preset struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	BaseURL  string `json:"baseUrl"`
	Model    string `json:"model"`
	Local    bool   `json:"local"`
	NeedsKey bool   `json:"needsKey"`
	Note     string `json:"note"`
}

var Presets = []Preset{
	{"deepseek", "DeepSeek", "openai", "https://api.deepseek.com/v1", "deepseek-chat", false, true, "OpenAI-compatible"},
	{"openai", "OpenAI", "openai", "https://api.openai.com/v1", "gpt-4o-mini", false, true, "OpenAI-compatible"},
	{"anthropic", "Anthropic Claude", "anthropic", "https://api.anthropic.com", "claude-sonnet-4-5", false, true, "Native Messages API"},
	{"gemini", "Google Gemini", "gemini", "https://generativelanguage.googleapis.com", "gemini-2.0-flash", false, true, "Native function calling"},
	{"openrouter", "OpenRouter", "openai", "https://openrouter.ai/api/v1", "anthropic/claude-3.5-sonnet", false, true, "Many models, one key"},
	{"groq", "Groq", "openai", "https://api.groq.com/openai/v1", "llama-3.3-70b-versatile", false, true, "Fast inference"},
	{"together", "Together AI", "openai", "https://api.together.xyz/v1", "meta-llama/Llama-3.3-70B-Instruct-Turbo", false, true, ""},
	{"mistral", "Mistral", "openai", "https://api.mistral.ai/v1", "mistral-large-latest", false, true, ""},
	{"ollama", "Ollama (local)", "openai", "http://localhost:11434/v1", "qwen2.5:14b", true, false, "Runs on your machine, no token needed"},
	{"lmstudio", "LM Studio (local)", "openai", "http://localhost:1234/v1", "local-model", true, false, "Runs on your machine"},
	{"vllm", "vLLM / any OpenAI-compatible", "openai", "http://localhost:8000/v1", "model-name", true, false, "Bring your own server"},
}

// Store persists settings to disk.
type Store struct {
	mu   sync.RWMutex
	path string
	cur  Settings
}

func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dataDir, "ai-settings.json")}
	s.cur = Settings{
		Kind:     "openai",
		BaseURL:  "https://api.deepseek.com/v1",
		Model:    "deepseek-chat",
		MaxSteps: 6,
		SystemPrompt: "You are the accounting copilot inside a Tally workspace. " +
			"Use the tools to read real data before answering; never invent figures. " +
			"Amounts are in the company's base currency. Be concise and concrete.",
	}

	if raw, err := os.ReadFile(s.path); err == nil {
		var loaded Settings
		if json.Unmarshal(raw, &loaded) == nil {
			s.merge(loaded)
		}
	}
	// Environment wins, so a headless deployment can be configured without the UI.
	if v := os.Getenv("TWUI_AI_KIND"); v != "" {
		s.cur.Kind = v
	}
	if v := os.Getenv("TWUI_AI_BASE_URL"); v != "" {
		s.cur.BaseURL = v
	}
	if v := os.Getenv("TWUI_AI_MODEL"); v != "" {
		s.cur.Model = v
	}
	if v := os.Getenv("TWUI_AI_API_KEY"); v != "" {
		s.cur.APIKey = v
	}
	if v := os.Getenv("TWUI_AI_SYSTEM_PROMPT"); v != "" {
		s.cur.SystemPrompt = v
	}
	return s, nil
}

func (s *Store) merge(in Settings) {
	if in.Kind != "" {
		s.cur.Kind = in.Kind
	}
	if in.BaseURL != "" {
		s.cur.BaseURL = in.BaseURL
	}
	if in.Model != "" {
		s.cur.Model = in.Model
	}
	if in.APIKey != "" {
		s.cur.APIKey = in.APIKey
	}
	if in.MaxSteps > 0 {
		s.cur.MaxSteps = in.MaxSteps
	}
	if in.SystemPrompt != "" {
		s.cur.SystemPrompt = in.SystemPrompt
	}
	s.cur.AllowWrites = in.AllowWrites
}

func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// Update applies a partial patch. An empty apiKey means "keep the current one",
// so the browser never has to round-trip the secret.
func (s *Store) Update(patch Settings, replaceKey bool) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if patch.Kind != "" && patch.Kind != s.cur.Kind {
		// Reset to the preset default when the vendor changes.
		for _, p := range Presets {
			if p.Kind == patch.Kind && (patch.BaseURL == "" || p.BaseURL == patch.BaseURL) {
				if patch.BaseURL == "" {
					patch.BaseURL = p.BaseURL
				}
				if patch.Model == "" || patch.Model == s.cur.Model {
					patch.Model = p.Model
				}
				break
			}
		}
	}
	if replaceKey {
		s.cur.APIKey = patch.APIKey
	}
	patch.APIKey = ""
	s.merge(patch)

	if err := s.persist(); err != nil {
		return s.cur, err
	}
	return s.cur, nil
}

func (s *Store) persist() error {
	raw, err := json.MarshalIndent(s.cur, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o600)
}

// New builds the provider described by the settings.
func New(s Settings) (Provider, error) {
	if strings.TrimSpace(s.Model) == "" {
		return nil, fmt.Errorf("no model configured; open AI settings and pick one")
	}
	switch s.Kind {
	case "", "openai":
		if s.APIKey == "" && !isLocal(s.BaseURL) {
			return nil, fmt.Errorf("no API token configured for %s", s.BaseURL)
		}
		return &OpenAIProvider{BaseURL: s.BaseURL, APIKey: s.APIKey}, nil
	case "anthropic":
		if s.APIKey == "" {
			return nil, fmt.Errorf("no API token configured for Anthropic")
		}
		return &AnthropicProvider{BaseURL: s.BaseURL, APIKey: s.APIKey}, nil
	case "gemini":
		if s.APIKey == "" {
			return nil, fmt.Errorf("no API token configured for Gemini")
		}
		return &GeminiProvider{BaseURL: s.BaseURL, APIKey: s.APIKey}, nil
	default:
		return nil, fmt.Errorf("unknown provider kind %q", s.Kind)
	}
}

func isLocal(baseURL string) bool {
	return strings.Contains(baseURL, "localhost") || strings.Contains(baseURL, "127.0.0.1") ||
		strings.Contains(baseURL, "0.0.0.0") || strings.Contains(baseURL, "host.docker.internal")
}
