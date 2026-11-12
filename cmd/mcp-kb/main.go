// Benri KB MCP server (stdio): read-only search/get over the tenant KB.
// Tenant is bound at launch via BENRI_TENANT_ID, never from tool arguments.
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
		fmt.Fprintln(os.Stderr, "mcp-kb:", err)
		os.Exit(1)
	}
	st, err := proc.OpenStore(cfg, store.RoleKBMCP)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcp-kb:", err)
		os.Exit(1)
	}
	defer st.Close()
	tenant := os.Getenv("BENRI_TENANT_ID")
	if tenant == "" {
		tenant = "tenant-hinata"
	}
	srv := mcpserver.NewKBServer(mcpserver.KBTools{Store: st, TK: proc.Tokenizer(cfg)}, tenant)
	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "mcp-kb:", err)
		os.Exit(1)
	}
}
