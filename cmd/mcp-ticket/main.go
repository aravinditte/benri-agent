// Benri ticket MCP server (stdio): the ONLY process that inserts tickets.
// Every call re-loads the persisted approval and validates the payload hash
// before writing (SPEC §9). Spawned only by the executor worker.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aravinditte/benri-agent/internal/config"
	"github.com/aravinditte/benri-agent/internal/mcpserver"
	"github.com/aravinditte/benri-agent/internal/proc"
	"github.com/aravinditte/benri-agent/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcp-ticket:", err)
		os.Exit(1)
	}
	st, err := proc.OpenStore(cfg, store.RoleTicketMCP)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcp-ticket:", err)
		os.Exit(1)
	}
	defer st.Close()
	srv := mcpserver.NewTicketServer(mcpserver.TicketTools{Store: st})
	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "mcp-ticket:", err)
		os.Exit(1)
	}
}
