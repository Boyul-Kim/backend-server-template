package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/boyulkim/api-template/internal/config"
)

// ServeArgs fills in `serve` defaults from the config so the binary can be
// started with no arguments (e.g. as a container entrypoint).
//
// Explicit CLI flags always win over the environment.
func ServeArgs(args []string, cfg config.Config) []string {
	if len(args) == 0 {
		args = []string{"serve"}
	}
	if args[0] != "serve" {
		return args
	}

	out := slices.Clone(args)
	if !hasFlag(args, "--http") {
		out = append(out, "--http="+cfg.HTTPAddr)
	}
	if cfg.CORSOrigins != "" && !hasFlag(args, "--origins") {
		out = append(out, "--origins="+cfg.CORSOrigins)
	}
	return out
}

func hasFlag(args []string, name string) bool {
	return slices.ContainsFunc(args, func(a string) bool {
		return a == name || strings.HasPrefix(a, name+"=")
	})
}

// Healthcheck probes the local server's liveness endpoint. It lets container
// images without a shell or curl (distroless) define a HEALTHCHECK, and runs
// without bootstrapping PocketBase so probes never touch the database files.
func Healthcheck(cfg config.Config) error {
	_, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("parse HTTP_ADDR: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	url := "http://127.0.0.1:" + port + "/api/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy: %s returned %d", url, res.StatusCode)
	}
	return nil
}
