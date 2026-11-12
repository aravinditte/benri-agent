// Package mcpclient hosts the MCP client gateways (SPEC §7): the agent
// worker connects ONLY to the KB and CRM servers (read-only); the executor
// connects ONLY to the ticket server. Servers are spawned as stdio
// subprocesses with the official MCP Go SDK; the tenant is bound via the
// server's environment, never via tool arguments from the model.
package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/aravinditte/benri-agent/internal/agent"
	"github.com/aravinditte/benri-agent/internal/executor"
	"github.com/aravinditte/benri-agent/internal/mcpserver"
	"github.com/aravinditte/benri-agent/internal/retrieval"
	"github.com/aravinditte/benri-agent/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const callTimeout = 15 * time.Second

type session struct {
	cs  *mcp.ClientSession
	cmd *exec.Cmd
}

func spawn(ctx context.Context, name, bin string, env []string) (*session, error) {
	if bin == "" {
		return nil, fmt.Errorf("MCP server binary for %s is not configured", name)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), env...)
	client := mcp.NewClient(&mcp.Implementation{Name: "benri-" + name + "-client", Version: "3.0.0"}, nil)
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect %s MCP server: %w", name, err)
	}
	return &session{cs: cs, cmd: cmd}, nil
}

func (s *session) close() {
	if s.cs != nil {
		_ = s.cs.Close()
	}
}

// callTool invokes a tool and decodes its structured output. A transport
// error (the request may have been processed) is reported as ambiguous.
func (s *session) callTool(ctx context.Context, name string, args map[string]any, out any) (toolErr string, ambigErr error, err error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	res, err := s.cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", &executor.OutcomeUnknownError{Msg: "transport error calling " + name + " (outcome may already be committed)"}, nil
	}
	if res.IsError {
		return textOf(res), nil, nil
	}
	raw, merr := marshalResult(res)
	if merr != nil {
		return "", nil, merr
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return "", nil, fmt.Errorf("decode %s result: %w", name, err)
		}
	}
	return "", nil, nil
}

func textOf(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return "tool error"
}

func marshalResult(res *mcp.CallToolResult) (json.RawMessage, error) {
	if res.StructuredContent != nil {
		return json.Marshal(res.StructuredContent)
	}
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return json.RawMessage(tc.Text), nil
		}
	}
	return json.RawMessage("null"), nil
}

// SpawnEnv builds the environment for a spawned MCP server process: the
// store configuration is inherited; the tenant is bound here.
func SpawnEnv(tenantID string) []string {
	return []string{"BENRI_TENANT_ID=" + tenantID}
}

// ---- Read gateway (agent worker): KB + CRM only ----

type ReadGateway struct {
	kb  *session
	crm *session
}

var _ agent.ToolGateway = (*ReadGateway)(nil)

func SpawnReadGateway(ctx context.Context, kbBin, crmBin, tenantID string) (*ReadGateway, error) {
	kb, err := spawn(ctx, "kb", kbBin, SpawnEnv(tenantID))
	if err != nil {
		return nil, err
	}
	crm, err := spawn(ctx, "crm", crmBin, SpawnEnv(tenantID))
	if err != nil {
		kb.close()
		return nil, err
	}
	return &ReadGateway{kb: kb, crm: crm}, nil
}

func (g *ReadGateway) Close() { g.kb.close(); g.crm.close() }

func (g *ReadGateway) SearchKB(ctx context.Context, tenantID, query, intent string, limit int) ([]retrieval.SearchHit, error) {
	var hits []retrieval.SearchHit
	toolErr, _, err := g.kb.callTool(ctx, "search_kb", map[string]any{"tenant_id": tenantID, "query": query, "intent": intent, "limit": limit}, &hits)
	if err != nil {
		return nil, err
	}
	if toolErr != "" {
		return nil, fmt.Errorf("search_kb: %s", toolErr)
	}
	if hits == nil {
		hits = []retrieval.SearchHit{}
	}
	return hits, nil
}

func (g *ReadGateway) LookupOrder(ctx context.Context, tenantID, orderNo string) (*store.Order, error) {
	var view mcpserver.OrderView
	toolErr, _, err := g.crm.callTool(ctx, "lookup_order", map[string]any{"tenant_id": tenantID, "order_no": orderNo}, &view)
	if err != nil {
		return nil, err
	}
	if toolErr != "" {
		if strings.Contains(toolErr, "ORDER_NOT_FOUND") {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup_order: %s", toolErr)
	}
	return &store.Order{OrderNo: orderNo, Item: view.Item, DeliveryStatus: view.DeliveryStatus, OrderedAt: view.OrderedAt, CustomerID: view.CustomerRef}, nil
}

// ---- Ticket gateway (executor worker): ticket server only ----

type TicketGateway struct {
	ticket *session
}

var _ executor.TicketGateway = (*TicketGateway)(nil)

func SpawnTicketGateway(ctx context.Context, ticketBin string) (*TicketGateway, error) {
	t, err := spawn(ctx, "ticket", ticketBin, SpawnEnv(""))
	if err != nil {
		return nil, err
	}
	return &TicketGateway{ticket: t}, nil
}

func (g *TicketGateway) Close() { g.ticket.close() }

func (g *TicketGateway) CreateTicket(ctx context.Context, approvedActionID string) (string, bool, error) {
	var res mcpserver.TicketResult
	toolErr, ambig, err := g.ticket.callTool(ctx, "create_ticket", map[string]any{"approved_action_id": approvedActionID}, &res)
	if ambig != nil {
		return "", false, ambig
	}
	if err != nil {
		return "", false, err
	}
	if toolErr != "" {
		return "", false, fmt.Errorf("create_ticket refused: %s", toolErr)
	}
	return res.TicketID, res.Replayed, nil
}

func (g *TicketGateway) AddTicketNote(ctx context.Context, approvedActionID string) (string, error) {
	var res mcpserver.TicketResult
	toolErr, ambig, err := g.ticket.callTool(ctx, "add_ticket_note", map[string]any{"approved_action_id": approvedActionID}, &res)
	if ambig != nil {
		return "", ambig
	}
	if err != nil {
		return "", err
	}
	if toolErr != "" {
		return "", fmt.Errorf("add_ticket_note refused: %s", toolErr)
	}
	return res.TicketID, nil
}

// DirectTicketGateway calls the ticket tool logic in-process (tests, eval,
// and single-process smoke). It preserves the same validation path.
type DirectTicketGateway struct {
	Tools mcpserver.TicketTools
}

var _ executor.TicketGateway = DirectTicketGateway{}

func (g DirectTicketGateway) CreateTicket(ctx context.Context, approvedActionID string) (string, bool, error) {
	res, err := g.Tools.CreateTicket(ctx, mcpserver.ActionInput{ApprovedActionID: approvedActionID})
	if err != nil {
		return "", false, err
	}
	return res.TicketID, res.Replayed, nil
}

func (g DirectTicketGateway) AddTicketNote(ctx context.Context, approvedActionID string) (string, error) {
	res, err := g.Tools.AddTicketNote(ctx, mcpserver.ActionInput{ApprovedActionID: approvedActionID})
	if err != nil {
		return "", err
	}
	return res.TicketID, nil
}
