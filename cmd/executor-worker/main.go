// Benri executor worker (SPEC §7/§13): claims approved execution jobs and
// executes them with an "executor"-role store handle. It can insert outbox
// records but CANNOT insert tickets directly — ticket writes happen only
// inside the ticket MCP server (spawned per job when BENRI_USE_MCP=1, or
// via the same tool logic in-process otherwise).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aravinditte/benri-agent/internal/config"
	"github.com/aravinditte/benri-agent/internal/executor"
	"github.com/aravinditte/benri-agent/internal/jobs"
	"github.com/aravinditte/benri-agent/internal/mcpclient"
	"github.com/aravinditte/benri-agent/internal/mcpserver"
	"github.com/aravinditte/benri-agent/internal/observability"
	"github.com/aravinditte/benri-agent/internal/proc"
	"github.com/aravinditte/benri-agent/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "executor-worker:", err)
		os.Exit(1)
	}
}

func run() error {
	once := flag.Bool("once", false, "process queued jobs once and exit")
	flag.Parse()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	shutdown, _ := observability.Setup(ctx, cfg.OTELServiceName+"-executor", cfg.KairoOTLPEndpoint)
	defer func() { _ = shutdown(ctx) }()
	st, err := proc.OpenStore(cfg, store.RoleExecutor)
	if err != nil {
		return err
	}
	defer st.Close()
	_ = proc.Tokenizer(cfg)
	handler := func(ctx context.Context, job *store.ExecutionJob) error {
		var tickets executor.TicketGateway
		if os.Getenv("BENRI_USE_MCP") == "1" {
			gw, err := mcpclient.SpawnTicketGateway(ctx, cfg.MCPTicketBin)
			if err != nil {
				return err
			}
			defer gw.Close()
			tickets = gw
		} else {
			ticketStore, err := proc.OpenStore(cfg, store.RoleTicketMCP)
			if err != nil {
				return err
			}
			defer ticketStore.Close()
			tickets = mcpclient.DirectTicketGateway{Tools: mcpserver.TicketTools{Store: ticketStore}}
		}
		outcome, err := executor.ExecuteClaimedJob(ctx, st, job, tickets)
		if err != nil {
			return err
		}
		fmt.Printf("executor: job %s -> %s %s\n", outcome.JobID, outcome.Status, outcome.Detail)
		return nil
	}
	for {
		n, err := jobs.ProcessQueuedJobs(ctx, st, handler, cfg.ExecutorConcurrency)
		if err != nil {
			return err
		}
		if *once {
			fmt.Printf("executor-worker: processed %d job(s)\n", n)
			return nil
		}
		if n == 0 {
			time.Sleep(500 * time.Millisecond)
		}
	}
}
