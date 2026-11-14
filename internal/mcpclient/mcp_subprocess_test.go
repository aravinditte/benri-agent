package mcpclient_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/aravinditte/benri-agent/internal/mcpclient"
	"github.com/aravinditte/benri-agent/internal/store"
	"github.com/aravinditte/benri-agent/internal/testutil"
)

// TestMCPSubprocessStdio spawns the REAL MCP server binaries over stdio
// (official SDK CommandTransport) and exercises the read gateway end to
// end — the same path BENRI_USE_MCP=1 takes in the workers.
func TestMCPSubprocessStdio(t *testing.T) {
	if os.Getenv("BENRI_SKIP_SUBPROCESS_TEST") == "1" {
		t.Skip("subprocess test disabled")
	}
	root := testutil.RootDir()
	binDir := t.TempDir()
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	for _, name := range []string{"mcp-kb", "mcp-crm", "mcp-ticket"} {
		cmd := exec.Command("go", "build", "-o", filepath.Join(binDir, name+exe), "./cmd/"+name)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, out)
		}
	}
	path, _ := testutil.SeededDB(t)
	t.Setenv("BENRI_DB_PATH", path)
	t.Setenv("BENRI_STORE", "sqlite")
	ctx := context.Background()
	gw, err := mcpclient.SpawnReadGateway(ctx, filepath.Join(binDir, "mcp-kb"+exe), filepath.Join(binDir, "mcp-crm"+exe), "tenant-hinata")
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	hits, err := gw.SearchKB(ctx, "tenant-hinata", "配送状況を教えてください", "delivery_status", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Error("subprocess search_kb returned no hits")
	}
	order, err := gw.LookupOrder(ctx, "tenant-hinata", "HIN-2026-0001")
	if err != nil {
		t.Fatal(err)
	}
	if order == nil || order.Item == "" {
		t.Errorf("subprocess lookup_order: %+v", order)
	}
	// Ticket gateway subprocess: unapproved action id must be refused.
	tg, err := mcpclient.SpawnTicketGateway(ctx, filepath.Join(binDir, "mcp-ticket"+exe))
	if err != nil {
		t.Fatal(err)
	}
	defer tg.Close()
	if _, _, err := tg.CreateTicket(ctx, "action_does_not_exist"); err == nil {
		t.Error("ticket subprocess must refuse an unknown/unapproved action")
	}
	var _ store.Repository
}
