// Package proc holds the shared process bootstrap helpers: store opening by
// role, tokenizer construction, and the ModelRail client (mock or live).
package proc

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aravinditte/benri-agent/internal/agent"
	"github.com/aravinditte/benri-agent/internal/config"
	"github.com/aravinditte/benri-agent/internal/japanese"
	"github.com/aravinditte/benri-agent/internal/modelrail"
	"github.com/aravinditte/benri-agent/internal/store"
)

// OpenStore opens the configured store with the given capability role.
// The SQLite path is the locally verified one; the PostgreSQL path returns
// the UNVERIFIED pg implementation handle.
func OpenStore(cfg *config.Config, role store.Role) (store.Repository, error) {
	switch cfg.StoreKind {
	case "sqlite":
		if dir := filepath.Dir(cfg.DBPath); dir != "." && dir != "" {
			_ = os.MkdirAll(dir, 0o755)
		}
		return store.OpenSQLite(cfg.DBPath, role)
	case "postgres":
		url := roleURL(cfg, role)
		if url == "" {
			return nil, fmt.Errorf("no database URL configured for role %s", role)
		}
		return store.OpenPG(url, role)
	}
	return nil, fmt.Errorf("unknown store kind %q", cfg.StoreKind)
}

func roleURL(cfg *config.Config, role store.Role) string {
	switch role {
	case store.RoleAPI:
		return cfg.APIDatabaseURL
	case store.RoleAgent:
		return cfg.AgentDatabaseURL
	case store.RoleExecutor:
		return cfg.ExecutorDatabaseURL
	case store.RoleKBMCP:
		return cfg.KBMCPDatabaseURL
	case store.RoleCRMMCP:
		return cfg.CRMMCPDatabaseURL
	case store.RoleTicketMCP:
		return cfg.TicketMCPDatabaseURL
	}
	return ""
}

// Tokenizer builds the configured Kagome tokenizer and registers it with the
// agent package (repair path uses the same dictionary as retrieval).
func Tokenizer(cfg *config.Config) japanese.Tokenizer {
	tk := japanese.ForDict(cfg.KagomeDict)
	agent.SetTokenizer(tk)
	return tk
}

// ModelClient builds the ModelRail client: mock mode (deterministic,
// MOCK_LLM=1 or no base URL) or live HTTP.
func ModelClient(cfg *config.Config, tk japanese.Tokenizer) *modelrail.Client {
	if cfg.MockLLM || cfg.ModelRailBaseURL == "" {
		return modelrail.New(modelrail.Config{}, agent.MockRespond(tk))
	}
	return modelrail.New(modelrail.Config{BaseURL: cfg.ModelRailBaseURL, APIKey: cfg.ModelRailAPIKey, Model: cfg.ModelRailModel}, nil)
}

// FixturesDir resolves the fixtures directory from the configured root.
func FixturesDir(cfg *config.Config) string {
	return filepath.Join(cfg.RootDir, "fixtures")
}
