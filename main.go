// Command tally-web-ui is the whole product in one binary.
//
//	go run .
//
// It serves the web UI, the HTTP API and a Model Context Protocol server on a
// single port, backed by one engine registry. Point it at a running Tally, at
// on-disk .1800/.900 files, or at the built-in demo company.
//
//	go run . --mcp-stdio
//
// additionally speaks MCP over stdio, which is how desktop MCP clients launch a
// server.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"tallywebui/internal/ai"
	"tallywebui/internal/config"
	"tallywebui/internal/engine"
	"tallywebui/internal/server"
)

func main() {
	cfg := config.Load()

	registryPath := flag.String("registry", cfg.RegistryPath, "path to tally-instances.json")
	port := flag.Int("port", cfg.Port, "HTTP port")
	dataDir := flag.String("data", cfg.DataDir, "directory for settings and local state")
	allowWrites := flag.Bool("allow-writes", cfg.AllowWrites, "allow the UI, the assistant and MCP to write to Tally")
	mcpStdio := flag.Bool("mcp-stdio", false, "also serve MCP over stdio, for desktop MCP clients")
	flag.Parse()

	cfg.RegistryPath = *registryPath
	cfg.Port = *port
	cfg.DataDir = *dataDir
	cfg.AllowWrites = *allowWrites

	if err := ensureRegistry(cfg.RegistryPath); err != nil {
		log.Fatalf("registry: %v", err)
	}

	registry, err := engine.LoadRegistry(cfg.RegistryPath)
	if err != nil {
		log.Fatalf("registry: %v", err)
	}
	settings, err := ai.NewStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("ai settings: %v", err)
	}

	handler := server.New(cfg, registry, settings)
	httpServer := &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
	}

	// The web UI and the HTTP MCP endpoint always run.
	go func() {
		banner(os.Stderr, cfg, registry, handler)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	// stdio MCP takes over the process: stdin and stdout belong to the protocol,
	// so nothing else may ever be written to stdout.
	if *mcpStdio {
		if err := handler.MCP().ServeStdio(context.Background(), os.Stdin, os.Stdout); err != nil && !errors.Is(err, io.EOF) {
			fmt.Fprintln(os.Stderr, "mcp stdio:", err)
		}
		return
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	fmt.Fprintln(os.Stderr, "stopped")
}

func banner(w io.Writer, cfg config.Config, registry *engine.Registry, handler *server.Server) {
	write := "disabled (read-only)"
	if cfg.AllowWrites {
		write = "ENABLED"
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "  tally-web-ui")
	fmt.Fprintf(w, "  web UI     %s\n", base)
	fmt.Fprintf(w, "  HTTP API   %s/api\n", base)
	fmt.Fprintf(w, "  MCP        %s/mcp   (%d tools)\n", base, handler.MCP().Count())
	fmt.Fprintf(w, "  registry   %s\n", registry.Path())
	fmt.Fprintf(w, "  instances  %d (default: %s)\n", len(registry.List(context.Background())), registry.Default())
	fmt.Fprintf(w, "  writes     %s\n", write)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Open the URL above, pick a company, then set your AI token in AI settings.")
	fmt.Fprintln(w, "  Add --mcp-stdio to also serve MCP over stdio.")
	fmt.Fprintln(w)
}

// ensureRegistry writes a usable registry on first run so that starting the
// server is genuinely the only step.
func ensureRegistry(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(absoluteOrDot(path)), 0o755); err != nil {
		return err
	}
	def := engine.RegistryFile{
		DefaultInstance: "demo",
		Server:          engine.ServerSettings{Port: 8787},
		Instances: []engine.InstanceConfig{
			{
				ID:      "demo",
				Kind:    "demo",
				Label:   "Demo company (works with no Tally installed)",
				Product: "demo",
			},
			{
				ID:      "prime",
				Kind:    "tally-xml",
				Label:   "TallyPrime or ERP 9 on this machine (XML port 9000)",
				Product: "tallyprime",
				Host:    "127.0.0.1",
				Port:    9000,
			},
			{
				ID:      "archive",
				Kind:    "tallyweb-file",
				Label:   "On-disk .1800/.900 files via the TallyWeb backend (port 9090)",
				Product: "erp9",
				BaseURL: "http://127.0.0.1:9090",
			},
		},
	}
	raw, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func absoluteOrDot(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "."
	}
	return abs
}
