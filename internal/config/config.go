// Package config centralizes startup configuration and validates it
// (SPEC §18). Each process reads only the variables for its own role;
// the agent worker is the only process that receives the ModelRail key,
// and the web process receives no database credential at all.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aravinditte/benri-agent/internal/domain"
)

type Config struct {
	Env       string
	MockLLM   bool
	DemoMode  bool
	StoreKind string // "sqlite" for local use or "postgres" for deployment
	// SQLite path (local verification). PG URLs are per-role (§18).
	DBPath string

	APIDatabaseURL       string
	AgentDatabaseURL     string
	ExecutorDatabaseURL  string
	KBMCPDatabaseURL     string
	CRMMCPDatabaseURL    string
	TicketMCPDatabaseURL string
	MigratorDatabaseURL  string

	APIPort int

	ModelRailBaseURL string
	ModelRailAPIKey  string
	ModelRailModel   string

	KairoOTLPEndpoint   string
	KairoTraceUIBaseURL string
	OTELServiceName     string

	AgentConcurrency    int
	ExecutorConcurrency int
	JobLease            time.Duration
	JobHeartbeat        time.Duration

	MCPKBBin     string
	MCPCRMBin    string
	MCPTicketBin string

	KagomeDict string // "ipa" or "uni"
	RootDir    string // directory containing fixtures/ and evals/ (for seed/eval)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
func getint(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}
func getdur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

// Load reads and validates the process configuration.
func Load() (*Config, error) {
	c := &Config{
		Env:                 getenv("BENRI_ENV", "development"),
		DemoMode:            getenv("DEMO_MODE", "1") == "1",
		StoreKind:           getenv("BENRI_STORE", "sqlite"),
		DBPath:              getenv("BENRI_DB_PATH", "./data/benri.sqlite"),
		APIPort:             getint("API_PORT", 8080),
		ModelRailBaseURL:    os.Getenv("MODELRAIL_BASE_URL"),
		ModelRailAPIKey:     os.Getenv("MODELRAIL_API_KEY"),
		ModelRailModel:      getenv("MODELRAIL_CHAT_MODEL", "default"),
		KairoOTLPEndpoint:   os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		KairoTraceUIBaseURL: os.Getenv("KAIRO_TRACE_UI_BASE_URL"),
		OTELServiceName:     getenv("OTEL_SERVICE_NAME", "benri"),
		AgentConcurrency:    getint("BENRI_AGENT_WORKER_CONCURRENCY", 4),
		ExecutorConcurrency: getint("BENRI_EXECUTOR_WORKER_CONCURRENCY", 2),
		JobLease:            getdur("BENRI_JOB_LEASE_DURATION", 60*time.Second),
		JobHeartbeat:        getdur("BENRI_JOB_HEARTBEAT_INTERVAL", 10*time.Second),
		MCPKBBin:            getenv("BENRI_MCP_KB_BIN", ""),
		KagomeDict:          getenv("BENRI_KAGOME_DICT", "ipa"),
		RootDir:             getenv("BENRI_ROOT_DIR", "."),
	}
	c.MCPCRMBin = getenv("BENRI_MCP_CRM_BIN", "")
	c.MCPTicketBin = getenv("BENRI_MCP_TICKET_BIN", "")
	c.MockLLM = getenv("MOCK_LLM", "1") == "1" || c.ModelRailBaseURL == ""
	c.APIDatabaseURL = os.Getenv("BENRI_API_DATABASE_URL")
	c.AgentDatabaseURL = os.Getenv("BENRI_AGENT_DATABASE_URL")
	c.ExecutorDatabaseURL = os.Getenv("BENRI_EXECUTOR_DATABASE_URL")
	c.KBMCPDatabaseURL = os.Getenv("BENRI_KB_MCP_DATABASE_URL")
	c.CRMMCPDatabaseURL = os.Getenv("BENRI_CRM_MCP_DATABASE_URL")
	c.TicketMCPDatabaseURL = os.Getenv("BENRI_TICKET_MCP_DATABASE_URL")
	c.MigratorDatabaseURL = os.Getenv("BENRI_MIGRATOR_DATABASE_URL")
	if c.KagomeDict != "ipa" && c.KagomeDict != "uni" {
		return nil, fmt.Errorf("BENRI_KAGOME_DICT must be ipa or uni, got %q", c.KagomeDict)
	}
	if c.StoreKind != "sqlite" && c.StoreKind != "postgres" {
		return nil, fmt.Errorf("BENRI_STORE must be sqlite or postgres, got %q", c.StoreKind)
	}
	return c, nil
}

// Limits builds the per-run hard limits (SPEC §5.3) from the environment.
func (c *Config) Limits() domain.Limits {
	l := domain.Limits{
		MaxModelCallsPerRun: getint("BENRI_MAX_MODEL_CALLS_PER_RUN", 4),
		MaxToolCallsPerRun:  getint("BENRI_MAX_TOOL_CALLS_PER_RUN", 8),
		MaxRunDurationMS:    int(getdur("BENRI_MAX_RUN_DURATION", 30*time.Second) / time.Millisecond),
		MaxInputTokens:      getint("BENRI_MAX_INPUT_TOKENS", 8000),
		MaxOutputTokens:     getint("BENRI_MAX_OUTPUT_TOKENS", 1200),
	}
	if v := strings.TrimSpace(os.Getenv("BENRI_MAX_COST_PER_RUN_JPY")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			l.MaxCostPerRunJPY = &f
		}
	}
	return l
}
