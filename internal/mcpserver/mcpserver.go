// Package mcpserver implements the three MCP servers' tool logic (SPEC §7)
// on top of capability-scoped store handles, plus stdio server constructors
// using the official MCP Go SDK. Tenant context is bound at server launch
// (BENRI_TENANT_ID) and NEVER taken from tool arguments.
package mcpserver

import (
	"context"
	"fmt"

	"github.com/aravinditte/benri-agent/internal/agent"
	"github.com/aravinditte/benri-agent/internal/executor"
	"github.com/aravinditte/benri-agent/internal/japanese"
	"github.com/aravinditte/benri-agent/internal/retrieval"
	"github.com/aravinditte/benri-agent/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- KB server (read-only) ----

type KBTools struct {
	Store store.Repository // kb-mcp role handle
	TK    japanese.Tokenizer
}

type SearchKBInput struct {
	TenantID string `json:"tenant_id"`
	Query    string `json:"query"`
	Intent   string `json:"intent,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

func (t KBTools) SearchKB(ctx context.Context, boundTenant string, in SearchKBInput) ([]retrieval.SearchHit, error) {
	if err := checkTenant(boundTenant, in.TenantID); err != nil {
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 5
	}
	articles, err := t.Store.ListActiveArticles(boundTenant)
	if err != nil {
		return nil, err
	}
	idx := retrieval.NewIndex(retrieval.DocsFromArticles(articles), t.TK)
	return idx.Search(in.Query, in.Intent, limit), nil
}

type GetArticleInput struct {
	TenantID  string `json:"tenant_id"`
	ArticleID string `json:"article_id"`
}

func (t KBTools) GetArticle(ctx context.Context, boundTenant string, in GetArticleInput) (*store.Article, error) {
	if err := checkTenant(boundTenant, in.TenantID); err != nil {
		return nil, err
	}
	art, err := t.Store.GetArticle(boundTenant, in.ArticleID)
	if err != nil {
		return nil, err
	}
	if art == nil {
		return nil, fmt.Errorf("ARTICLE_NOT_FOUND")
	}
	return art, nil
}

// ---- CRM server (read-only) ----

type CRMTools struct {
	Store store.Repository // crm-mcp role handle
}

type LookupOrderInput struct {
	TenantID string `json:"tenant_id"`
	OrderNo  string `json:"order_no"`
}

type OrderView struct {
	OrderNoMasked  string `json:"order_no_masked"`
	Item           string `json:"item"`
	DeliveryStatus string `json:"delivery_status"`
	OrderedAt      string `json:"ordered_at"`
	CustomerRef    string `json:"customer_ref"`
}

func (t CRMTools) LookupOrder(ctx context.Context, boundTenant string, in LookupOrderInput) (*OrderView, error) {
	if err := checkTenant(boundTenant, in.TenantID); err != nil {
		return nil, err
	}
	order, err := t.Store.FindOrderByNo(boundTenant, in.OrderNo)
	if err != nil {
		return nil, err
	}
	if order == nil {
		// Do not leak whether the number exists in another tenant.
		if n, _ := t.Store.CountOrdersWithNoAnyTenant(in.OrderNo); n > 0 {
			return nil, fmt.Errorf("CROSS_TENANT_ORDER: order number belongs to a different tenant")
		}
		return nil, fmt.Errorf("ORDER_NOT_FOUND")
	}
	return &OrderView{OrderNoMasked: agent.MaskOrderNo(order.OrderNo), Item: order.Item, DeliveryStatus: order.DeliveryStatus, OrderedAt: order.OrderedAt, CustomerRef: order.CustomerID}, nil
}

// ---- Ticket server (the ONLY ticket writer; ticket-mcp role) ----

type TicketTools struct {
	Store store.Repository // ticket-mcp role handle
}

type ActionInput struct {
	ApprovedActionID string `json:"approved_action_id"`
}

type TicketResult struct {
	TicketID string `json:"ticket_id"`
	Replayed bool   `json:"replayed"`
}

// CreateTicket inserts a ticket ONLY for an action covered by a persisted
// approval whose payload hash validates (executor.LoadVerifiedAction).
func (t TicketTools) CreateTicket(ctx context.Context, in ActionInput) (*TicketResult, error) {
	action, detail := executor.LoadVerifiedAction(t.Store, in.ApprovedActionID)
	if action == nil {
		return nil, fmt.Errorf("APPROVAL_VALIDATION_FAILED: %s", detail)
	}
	if action.ActionType != "create_ticket" {
		return nil, fmt.Errorf("ACTION_TYPE_MISMATCH: expected create_ticket")
	}
	if existing, _ := t.Store.FindTicketByAction(action.ID); existing != nil {
		return &TicketResult{TicketID: existing.ID, Replayed: true}, nil
	}
	won, err := t.Store.TransitionAction(action.ID, "queued", "running")
	if err != nil {
		return nil, err
	}
	if !won {
		if existing, _ := t.Store.FindTicketByAction(action.ID); existing != nil {
			return &TicketResult{TicketID: existing.ID, Replayed: true}, nil
		}
		cur, _ := t.Store.GetActionByID(action.ID)
		if cur != nil && cur.State == "running" {
			// Lost the transition race but no ticket yet: the winner is
			// mid-creation. Treat as replay-safe duplicate.
			if existing, _ := t.Store.FindTicketByAction(action.ID); existing != nil {
				return &TicketResult{TicketID: existing.ID, Replayed: true}, nil
			}
		}
		return nil, fmt.Errorf("ACTION_TRANSITION_LOST")
	}
	payload, err := agent.ParsePayload(action.CanonicalPayload)
	if err != nil {
		return nil, fmt.Errorf("PAYLOAD_UNPARSABLE")
	}
	subject, _ := payload.Arguments["subject"].(string)
	priority, _ := payload.Arguments["priority"].(string)
	_ = subject
	ticket, err := t.Store.InsertTicket(store.Ticket{
		ID: store.NewID("ticket"), TenantID: action.TenantID, InquiryID: payload.InquiryID,
		Status: "open", Priority: priority, SourceActionID: &action.ID,
	})
	if err != nil {
		if existing, _ := t.Store.FindTicketByAction(action.ID); existing != nil {
			return &TicketResult{TicketID: existing.ID, Replayed: true}, nil
		}
		return nil, err
	}
	_ = t.Store.SetActionState(action.ID, "succeeded")
	ah := action.PayloadHash
	_ = t.Store.InsertAudit(store.AuditEvent{TenantID: action.TenantID, ActorType: "mcp", ActorID: strp("ticket-mcp"), Action: "ticket.created", EntityType: "ticket", EntityID: ticket.ID, AfterHash: &ah})
	return &TicketResult{TicketID: ticket.ID}, nil
}

func (t TicketTools) AddTicketNote(ctx context.Context, in ActionInput) (*TicketResult, error) {
	action, detail := executor.LoadVerifiedAction(t.Store, in.ApprovedActionID)
	if action == nil {
		return nil, fmt.Errorf("APPROVAL_VALIDATION_FAILED: %s", detail)
	}
	if action.ActionType != "add_ticket_note" {
		return nil, fmt.Errorf("ACTION_TYPE_MISMATCH: expected add_ticket_note")
	}
	if existing, _ := t.Store.FindNoteByAction(action.ID); existing != nil {
		return &TicketResult{TicketID: existing.TicketID, Replayed: true}, nil
	}
	payload, err := agent.ParsePayload(action.CanonicalPayload)
	if err != nil {
		return nil, fmt.Errorf("PAYLOAD_UNPARSABLE")
	}
	// Attach to the ticket created for the same inquiry/draft.
	actions, _ := t.Store.ListActionsByDraft(action.TenantID, action.DraftID)
	var ticket *store.Ticket
	for i := range actions {
		if actions[i].ActionType == "create_ticket" {
			if tk, _ := t.Store.FindTicketByAction(actions[i].ID); tk != nil {
				ticket = tk
			}
		}
	}
	if ticket == nil {
		return nil, fmt.Errorf("TICKET_NOT_FOUND: no ticket exists for this inquiry yet")
	}
	noteText, _ := payload.Arguments["note"].(string)
	note, err := t.Store.InsertTicketNote(store.TicketNote{
		ID: store.NewID("note"), TenantID: action.TenantID, TicketID: ticket.ID,
		Body: noteText, SourceActionID: &action.ID,
	})
	if err != nil {
		return nil, err
	}
	_ = t.Store.SetActionState(action.ID, "succeeded")
	_ = note
	return &TicketResult{TicketID: ticket.ID}, nil
}

func checkTenant(bound, requested string) error {
	if requested != "" && requested != bound {
		return fmt.Errorf("TENANT_MISMATCH: server is bound to a different tenant")
	}
	return nil
}

func strp(s string) *string { return &s }

// ---- stdio server constructors (official MCP Go SDK) ----

func NewKBServer(tools KBTools, boundTenant string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "benri-kb-mcp", Version: "3.0.0"}, nil)
	mcp.AddTool[SearchKBInput, []retrieval.SearchHit](s, &mcp.Tool{Name: "search_kb", Description: "Read-only lexical search over the tenant knowledge base."}, func(ctx context.Context, req *mcp.CallToolRequest, in SearchKBInput) (*mcp.CallToolResult, []retrieval.SearchHit, error) {
		out, err := tools.SearchKB(ctx, boundTenant, in)
		return nil, out, err
	})
	mcp.AddTool[GetArticleInput, *store.Article](s, &mcp.Tool{Name: "get_article", Description: "Fetch one KB article by id (tenant-bound)."}, func(ctx context.Context, req *mcp.CallToolRequest, in GetArticleInput) (*mcp.CallToolResult, *store.Article, error) {
		out, err := tools.GetArticle(ctx, boundTenant, in)
		return nil, out, err
	})
	return s
}

func NewCRMServer(tools CRMTools, boundTenant string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "benri-crm-mcp", Version: "3.0.0"}, nil)
	mcp.AddTool[LookupOrderInput, *OrderView](s, &mcp.Tool{Name: "lookup_order", Description: "Read-only order lookup by order number (tenant-bound)."}, func(ctx context.Context, req *mcp.CallToolRequest, in LookupOrderInput) (*mcp.CallToolResult, *OrderView, error) {
		out, err := tools.LookupOrder(ctx, boundTenant, in)
		return nil, out, err
	})
	return s
}

func NewTicketServer(tools TicketTools) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "benri-ticket-mcp", Version: "3.0.0"}, nil)
	mcp.AddTool[ActionInput, *TicketResult](s, &mcp.Tool{Name: "create_ticket", Description: "Create a ticket for an APPROVED action id only; validates the persisted approval and payload hash."}, func(ctx context.Context, req *mcp.CallToolRequest, in ActionInput) (*mcp.CallToolResult, *TicketResult, error) {
		out, err := tools.CreateTicket(ctx, in)
		return nil, out, err
	})
	mcp.AddTool[ActionInput, *TicketResult](s, &mcp.Tool{Name: "add_ticket_note", Description: "Add an internal note for an APPROVED action id only."}, func(ctx context.Context, req *mcp.CallToolRequest, in ActionInput) (*mcp.CallToolResult, *TicketResult, error) {
		out, err := tools.AddTicketNote(ctx, in)
		return nil, out, err
	})
	return s
}
