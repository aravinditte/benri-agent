// Benri API process (SPEC §13): chi + oapi-codegen handlers over an
// "api"-role store handle. Seeds demo data on first boot when empty.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	apihttp "github.com/aravinditte/benri-agent/internal/api"
	"github.com/aravinditte/benri-agent/internal/config"
	"github.com/aravinditte/benri-agent/internal/observability"
	"github.com/aravinditte/benri-agent/internal/proc"
	"github.com/aravinditte/benri-agent/internal/seed"
	"github.com/aravinditte/benri-agent/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	shutdown, _ := observability.Setup(ctx, cfg.OTELServiceName+"-api", cfg.KairoOTLPEndpoint)
	defer func() { _ = shutdown(ctx) }()
	st, err := proc.OpenStore(cfg, store.RoleAPI)
	if err != nil {
		return err
	}
	defer st.Close()
	tk := proc.Tokenizer(cfg)
	if !seed.Seeded(st) {
		if cfg.StoreKind == "postgres" {
			return fmt.Errorf("postgres database is not seeded; run the dedicated seed command before the API")
		}
		// Seeding needs fixture-write access beyond the api role; use a
		// short-lived full handle on the same SQLite database for first boot.
		full, err := proc.OpenStore(cfg, store.RoleFull)
		if err != nil {
			return err
		}
		if err := seed.Seed(full, proc.FixturesDir(cfg), tk); err != nil {
			full.Close()
			return err
		}
		full.Close()
		fmt.Println("seeded demo fixtures")
	}
	srv := &apihttp.Server{Store: st, Cfg: cfg, TK: tk}
	addr := fmt.Sprintf(":%d", cfg.APIPort)
	fmt.Printf("benri api listening on %s (store=%s, mock_llm=%v)\n", addr, cfg.StoreKind, cfg.MockLLM)
	return http.ListenAndServe(addr, srv.Router())
}
