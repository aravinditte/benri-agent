// Benri agent worker (SPEC §7/§13): claims queued runs and executes the
// pipeline with an "agent"-role store handle. It connects ONLY to the KB
// and CRM MCP servers (read-only) when BENRI_USE_MCP=1; otherwise it uses
// the direct gateway (same tool logic, in-process). It has NO route to the
// ticket MCP server or the executor, and its store handle cannot approve,
// enqueue execution jobs, or write tickets/outbox.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aravinditte/benri-agent/internal/agent"
	"github.com/aravinditte/benri-agent/internal/config"
	"github.com/aravinditte/benri-agent/internal/jobs"
	"github.com/aravinditte/benri-agent/internal/mcpclient"
	"github.com/aravinditte/benri-agent/internal/observability"
	"github.com/aravinditte/benri-agent/internal/proc"
	"github.com/aravinditte/benri-agent/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "agent-worker:", err)
		os.Exit(1)
	}
}

func run() error {
	once := flag.Bool("once", false, "process queued runs once and exit")
	flag.Parse()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	shutdown, _ := observability.Setup(ctx, cfg.OTELServiceName+"-agent", cfg.KairoOTLPEndpoint)
	defer func() { _ = shutdown(ctx) }()
	st, err := proc.OpenStore(cfg, store.RoleAgent)
	if err != nil {
		return err
	}
	defer st.Close()
	tk := proc.Tokenizer(cfg)
	llm := proc.ModelClient(cfg, tk)
	handler := func(ctx context.Context, runID string) error {
		var tools agent.ToolGateway
		if os.Getenv("BENRI_USE_MCP") == "1" {
			run, err := st.GetRunByID(runID)
			if err != nil || run == nil {
				return fmt.Errorf("run not found")
			}
			gw, err := mcpclient.SpawnReadGateway(ctx, cfg.MCPKBBin, cfg.MCPCRMBin, run.TenantID)
			if err != nil {
				return err
			}
			defer gw.Close()
			tools = gw
		} else {
			tools = agent.DirectToolGateway{Store: st, TK: tk}
		}
		_, err := agent.RunPipeline(ctx, st, runID, tools, llm, cfg.Limits(), nil)
		if _, ok := err.(*agent.CrashSimulatedError); ok {
			return jobs.WrapCrash(err)
		}
		return err
	}
	for {
		n, err := jobs.ProcessQueuedRuns(ctx, st, handler, cfg.AgentConcurrency)
		if err != nil {
			return err
		}
		if *once {
			fmt.Printf("agent-worker: processed %d run(s)\n", n)
			return nil
		}
		if n == 0 {
			time.Sleep(500 * time.Millisecond)
		}
	}
}
