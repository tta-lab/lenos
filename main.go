// Package main is the entry point for the Lenos CLI.
//
//	@title			Lenos API
//	@version		1.0
//	@description	Lenos is a terminal-based AI coding assistant and interactive runtime for the ttal ecosystem. This API is served over a Unix socket (or Windows named pipe) and provides programmatic access to workspaces, sessions, agents, and more.
//	@contact.name	tta-lab
//	@contact.url	https://github.com/tta-lab/lenos
//	@license.name	FSL-1.1-MIT
//	@license.url	https://github.com/tta-lab/lenos/blob/main/LICENSE.md
//	@BasePath		/v1
package main

import (
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"

	_ "github.com/joho/godotenv/autoload"
	"github.com/tta-lab/lenos/internal/cmd"
)

func main() {
	if os.Getenv("LENOS_PROFILE") != "" {
		go func() {
			slog.Info("Serving pprof at localhost:6060")
			if httpErr := http.ListenAndServe("localhost:6060", nil); httpErr != nil {
				slog.Error("Failed to pprof listen", "error", httpErr)
			}
		}()
	}

	cmd.Execute()
}
