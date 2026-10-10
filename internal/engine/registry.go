package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// InstanceConfig is one entry in tally-instances.json.
//
//	kind: "tally-xml"      -> a running Tally (Prime or ERP 9) on its XML port
//	kind: "tallyweb-file"  -> the TallyWeb Go backend reading .1800/.900 files
//	kind: "demo"           -> in-memory sample company, no Tally needed
type InstanceConfig struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label,omitempty"`
	Product  string `json:"product,omitempty"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Company  string `json:"company,omitempty"`
	BaseURL  string `json:"baseUrl,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Folder   string `json:"companyDir,omitempty"`
	Timeout  int    `json:"timeoutSeconds,omitempty"`
}

// RegistryFile is the on-disk registry.
type RegistryFile struct {
	DefaultInstance string           `json:"defaultInstance"`
	Server          ServerSettings   `json:"server"`
	Instances       []InstanceConfig `json:"instances"`
}

type ServerSettings struct {
	Port int `json:"port"`
}

// Registry owns the engines and resolves the one a request targets.
type Registry struct {
	mu       sync.RWMutex
	config   RegistryFile
	engines  map[string]Engine
	path     string
	loadedAt time.Time
}

func LoadRegistry(path string) (*Registry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg RegistryFile
	if err := jsonUnmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	if len(cfg.Instances) == 0 {
		return nil, fmt.Errorf("%s has no instances", path)
	}

	r := &Registry{config: cfg, engines: map[string]Engine{}, path: path, loadedAt: time.Now()}
	for _, inst := range cfg.Instances {
		e, err := buildEngine(inst)
		if err != nil {
			return nil, fmt.Errorf("instance %q: %w", inst.ID, err)
		}
		r.engines[inst.ID] = e
	}
	if cfg.DefaultInstance == "" {
		r.config.DefaultInstance = cfg.Instances[0].ID
	}
	if _, ok := r.engines[r.config.DefaultInstance]; !ok {
		return nil, fmt.Errorf("defaultInstance %q is not defined", r.config.DefaultInstance)
	}
	return r, nil
}

func buildEngine(inst InstanceConfig) (Engine, error) {
	switch inst.Kind {
	case "demo":
		return NewDemoEngine(), nil
	case "tally-xml":
		if inst.Host == "" || inst.Port == 0 {
			return nil, fmt.Errorf("tally-xml needs host and port")
		}
		return NewTallyXMLEngine(TallyXMLConfig{
			ID:      inst.ID,
			Label:   inst.Label,
			Product: inst.Product,
			Host:    inst.Host,
			Port:    inst.Port,
			Company: inst.Company,
			Timeout: seconds(inst.Timeout, 30),
		}), nil
	case "tallyweb-file":
		if inst.BaseURL == "" {
			return nil, fmt.Errorf("tallyweb-file needs baseUrl")
		}
		return NewFileEngine(FileConfig{
			ID:         inst.ID,
			Label:      inst.Label,
			Product:    inst.Product,
			BaseURL:    inst.BaseURL,
			Username:   inst.Username,
			Password:   inst.Password,
			CompanyDir: inst.Folder,
			Timeout:    seconds(inst.Timeout, 30),
		}), nil
	default:
		return nil, fmt.Errorf("unknown kind %q (want tally-xml, tallyweb-file or demo)", inst.Kind)
	}
}

func seconds(v, def int) time.Duration {
	if v <= 0 {
		v = def
	}
	return time.Duration(v) * time.Second
}

func (r *Registry) Path() string { return r.path }

func (r *Registry) Default() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config.DefaultInstance
}

func (r *Registry) ServerPort() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.config.Server.Port == 0 {
		return 8787
	}
	return r.config.Server.Port
}

// Resolve returns the engine for an instance id, or the default one.
func (r *Registry) Resolve(id string) (Engine, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id == "" {
		id = r.config.DefaultInstance
	}
	e, ok := r.engines[id]
	if !ok {
		return nil, fmt.Errorf("unknown instance %q", id)
	}
	return e, nil
}

// InstanceInfo is the UI-facing description of an instance.
type InstanceInfo struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Product   string `json:"product"`
	Address   string `json:"address"`
	Company   string `json:"company,omitempty"`
	IsDefault bool   `json:"is_default"`
	Health    Health `json:"health"`
}

func (r *Registry) List(ctx context.Context) []InstanceInfo {
	r.mu.RLock()
	instances := append([]InstanceConfig(nil), r.config.Instances...)
	def := r.config.DefaultInstance
	engines := make(map[string]Engine, len(r.engines))
	for k, v := range r.engines {
		engines[k] = v
	}
	r.mu.RUnlock()

	out := make([]InstanceInfo, 0, len(instances))
	for _, inst := range instances {
		e := engines[inst.ID]
		hctx, cancel := context.WithTimeout(ctx, 9*time.Second)
		health := e.Health(hctx)
		cancel()

		address := inst.BaseURL
		if inst.Kind == "tally-xml" {
			address = fmt.Sprintf("%s:%d", inst.Host, inst.Port)
		}
		if inst.Kind == "demo" {
			address = "in-memory"
		}
		out = append(out, InstanceInfo{
			ID:        inst.ID,
			Kind:      inst.Kind,
			Label:     orDefault(inst.Label, inst.ID),
			Product:   e.Product(),
			Address:   address,
			Company:   inst.Company,
			IsDefault: inst.ID == def,
			Health:    health,
		})
	}
	return out
}

// WriteRegistryDefault persists a new default instance so the choice sticks.
func (r *Registry) WriteRegistryDefault(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.engines[id]; !ok {
		return fmt.Errorf("unknown instance %q", id)
	}
	r.config.DefaultInstance = id
	raw, err := jsonMarshalIndent(r.config)
	if err != nil {
		return err
	}
	return os.WriteFile(r.path, raw, 0o644)
}

func ensureDir(path string) error { return os.MkdirAll(filepath.Dir(path), 0o755) }
