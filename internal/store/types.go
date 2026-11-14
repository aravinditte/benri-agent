// Package store defines Benri's repository contracts and two implementations:
//   - SQLiteStore (pure-Go modernc driver): lightweight local/test path.
//   - PgStore (pgx/v5 stdlib driver): PostgreSQL/Compose path, verified by
//     the opt-in PostgreSQL repository contract test and Compose smoke flow.
//
// Capability model (SPEC §3.3/§6/§9) — a handle carries a Role and write
// methods check it before touching fixture/approval tables:
//
//	agent:    runs/run_steps/tool_calls/drafts/proposed_actions/audit writes;
//	          CANNOT approve, insert execution_jobs, or write tickets/outbox.
//	api:      approvals + execution_jobs insertion, inquiries, KB, sessions;
//	          CANNOT write tickets/outbox/ticket_notes.
//	executor: claim/update execution_jobs, insert outbox_messages;
//	          CANNOT insert tickets (only the ticket MCP role can).
//	kb-mcp:   read-only. crm-mcp: read-only.
//	ticket-mcp: insert tickets/ticket_notes after approval validation only.
//	full:     seed/tests only.
package store

import (
	"fmt"
	"time"
)

type Role string

const (
	RoleAgent     Role = "agent"
	RoleAPI       Role = "api"
	RoleExecutor  Role = "executor"
	RoleKBMCP     Role = "kb-mcp"
	RoleCRMMCP    Role = "crm-mcp"
	RoleTicketMCP Role = "ticket-mcp"
	RoleFull      Role = "full"
)

// CapabilityError is returned when a role attempts a write its process must
// structurally be unable to perform (SPEC §9 invariants).
type CapabilityError struct{ Msg string }

func (e *CapabilityError) Error() string { return "CAPABILITY_DENIED: " + e.Msg }
func (e *CapabilityError) Code() string  { return "CAPABILITY_DENIED" }

type NotFoundError struct{ What string }

func (e *NotFoundError) Error() string { return "NOT_FOUND: " + e.What }
func (e *NotFoundError) Code() string  { return "NOT_FOUND" }

func NowISO() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00") }

var idCounter uint64

// NewID returns a prefixed unique id (prefix_<12 hex>), same shape as v2.
func NewID(prefix string) string {
	idCounter++
	return fmt.Sprintf("%s_%012x", prefix, (uint64(time.Now().UnixNano())^(idCounter*0x9e3779b97f4a7c15))&0xffffffffffff)
}

type Tenant struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type User struct {
	ID           string `json:"id"`
	TenantID     string `json:"tenant_id"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
	Status       string `json:"status"`
}

type Inquiry struct {
	ID                 string  `json:"id"`
	TenantID           string  `json:"tenant_id"`
	Source             string  `json:"source"`
	Subject            string  `json:"subject"`
	RedactedBody       string  `json:"redacted_body"`
	NormalizedBodyHash string  `json:"normalized_body_hash"`
	OrderNoHint        *string `json:"order_no_hint"`
	Status             string  `json:"status"`
	CreatedAt          string  `json:"created_at"`
}

type Customer struct {
	ID                  string `json:"id"`
	TenantID            string `json:"tenant_id"`
	SyntheticName       string `json:"synthetic_name"`
	SyntheticContactRef string `json:"synthetic_contact_ref"`
}

type Order struct {
	ID             string `json:"id"`
	TenantID       string `json:"tenant_id"`
	CustomerID     string `json:"customer_id"`
	OrderNo        string `json:"order_no"`
	Item           string `json:"item"`
	OrderedAt      string `json:"ordered_at"`
	DeliveryStatus string `json:"delivery_status"`
	CustomerName   string `json:"customer_name,omitempty"`
}

type Article struct {
	ID         string   `json:"id"`
	TenantID   string   `json:"tenant_id"`
	Title      string   `json:"title"`
	IntentTags []string `json:"intent_tags"`
	Body       string   `json:"body"`
	Version    int      `json:"version"`
	Status     string   `json:"status"`
	UpdatedAt  string   `json:"updated_at"`
}

type Chunk struct {
	ID            string `json:"id"`
	TenantID      string `json:"tenant_id"`
	ArticleID     string `json:"article_id"`
	ChunkIndex    int    `json:"chunk_index"`
	Text          string `json:"text"`
	TokenizedText string `json:"tokenized_text"`
	IndexVersion  string `json:"index_version"`
}

type Run struct {
	ID               string   `json:"id"`
	TenantID         string   `json:"tenant_id"`
	InquiryID        string   `json:"inquiry_id"`
	State            string   `json:"state"`
	ModelRoute       *string  `json:"model_route"`
	PromptVersion    *string  `json:"prompt_version"`
	ConfigSnapshot   *string  `json:"config_snapshot"`
	UsageJSON        *string  `json:"usage_json"`
	CostJPY          *float64 `json:"cost_jpy"`
	TraceID          *string  `json:"trace_id"`
	LeaseOwner       *string  `json:"lease_owner"`
	LeaseExpiresAt   *string  `json:"lease_expires_at"`
	HeartbeatAt      *string  `json:"heartbeat_at"`
	AttemptCount     int      `json:"attempt_count"`
	LastErrorCode    *string  `json:"last_error_code"`
	IdempotencyKey   *string  `json:"idempotency_key"`
	EscalationReason *string  `json:"escalation_reason"`
	HandoffSummary   *string  `json:"handoff_summary"`
	StartedAt        *string  `json:"started_at"`
	FinishedAt       *string  `json:"finished_at"`
	CreatedAt        string   `json:"created_at"`
}

type RunStep struct {
	ID             string  `json:"id"`
	TenantID       string  `json:"tenant_id"`
	RunID          string  `json:"run_id"`
	StepKey        string  `json:"step_key"`
	Attempt        int     `json:"attempt"`
	State          string  `json:"state"`
	InputRedacted  *string `json:"input_redacted"`
	OutputRedacted *string `json:"output_redacted"`
	PromptVersion  *string `json:"prompt_version"`
	Model          *string `json:"model"`
	StartedAt      *string `json:"started_at"`
	FinishedAt     *string `json:"finished_at"`
	DurationMS     *int64  `json:"duration_ms"`
	TraceID        *string `json:"trace_id"`
	SpanID         *string `json:"span_id"`
	ErrorCode      *string `json:"error_code"`
}

type ToolCall struct {
	ID           string  `json:"id"`
	TenantID     string  `json:"tenant_id"`
	RunID        *string `json:"run_id"`
	Server       string  `json:"server"`
	Tool         string  `json:"tool"`
	RiskLevel    string  `json:"risk_level"`
	ArgsRedacted *string `json:"args_redacted"`
	Status       string  `json:"status"`
	LatencyMS    *int64  `json:"latency_ms"`
	TraceID      *string `json:"trace_id"`
	SpanID       *string `json:"span_id"`
	CreatedAt    string  `json:"created_at"`
}

type Draft struct {
	ID                string  `json:"id"`
	TenantID          string  `json:"tenant_id"`
	RunID             string  `json:"run_id"`
	Revision          int     `json:"revision"`
	Body              string  `json:"body"`
	CitedArticleIDs   string  `json:"cited_article_ids"`  // JSON array string at rest
	FactualReferences string  `json:"factual_references"` // JSON array string at rest
	VerificationJSON  *string `json:"verification_json"`
	ContentHash       string  `json:"content_hash"`
	State             string  `json:"state"`
	CreatedAt         string  `json:"created_at"`
}

type ProposedAction struct {
	ID               string  `json:"id"`
	TenantID         string  `json:"tenant_id"`
	DraftID          string  `json:"draft_id"`
	RunID            *string `json:"run_id"`
	ActionType       string  `json:"action_type"`
	CanonicalPayload string  `json:"canonical_payload"` // JSON string at rest
	PayloadHash      string  `json:"payload_hash"`
	State            string  `json:"state"`
	CreatedAt        string  `json:"created_at"`
}

type Approval struct {
	ID                string  `json:"id"`
	TenantID          string  `json:"tenant_id"`
	DraftID           string  `json:"draft_id"`
	DraftRevision     int     `json:"draft_revision"`
	DraftHash         string  `json:"draft_hash"`
	OperatorID        string  `json:"operator_id"`
	SelectedActionIDs string  `json:"selected_action_ids"` // JSON array string
	PolicyVersion     string  `json:"policy_version"`
	ApprovedAt        string  `json:"approved_at"`
	RejectedReason    *string `json:"rejected_reason"`
}

type ExecutionJob struct {
	ID               string  `json:"id"`
	TenantID         string  `json:"tenant_id"`
	ProposedActionID string  `json:"proposed_action_id"`
	IdempotencyKey   string  `json:"idempotency_key"`
	State            string  `json:"state"`
	LeaseOwner       *string `json:"lease_owner"`
	LeaseExpiresAt   *string `json:"lease_expires_at"`
	HeartbeatAt      *string `json:"heartbeat_at"`
	Attempts         int     `json:"attempts"`
	ResultJSON       *string `json:"result_json"`
	FailureCode      *string `json:"failure_code"`
	CreatedAt        string  `json:"created_at"`
}

type Ticket struct {
	ID             string  `json:"id"`
	TenantID       string  `json:"tenant_id"`
	InquiryID      string  `json:"inquiry_id"`
	Status         string  `json:"status"`
	Priority       string  `json:"priority"`
	SourceActionID *string `json:"source_action_id"`
	CreatedAt      string  `json:"created_at"`
}

type TicketNote struct {
	ID             string  `json:"id"`
	TenantID       string  `json:"tenant_id"`
	TicketID       string  `json:"ticket_id"`
	Body           string  `json:"body"`
	SourceActionID *string `json:"source_action_id"`
	CreatedAt      string  `json:"created_at"`
}

type OutboxMessage struct {
	ID             string  `json:"id"`
	TenantID       string  `json:"tenant_id"`
	InquiryID      string  `json:"inquiry_id"`
	DraftRevision  int     `json:"draft_revision"`
	Body           string  `json:"body"`
	DeliveryState  string  `json:"delivery_state"`
	SourceActionID *string `json:"source_action_id"`
	CreatedAt      string  `json:"created_at"`
}

type AuditEvent struct {
	ID         string  `json:"id"`
	TenantID   string  `json:"tenant_id"`
	ActorType  string  `json:"actor_type"`
	ActorID    *string `json:"actor_id"`
	Action     string  `json:"action"`
	EntityType string  `json:"entity_type"`
	EntityID   string  `json:"entity_id"`
	BeforeHash *string `json:"before_hash"`
	AfterHash  *string `json:"after_hash"`
	TraceID    *string `json:"trace_id"`
	CreatedAt  string  `json:"created_at"`
}

type EvalRunRow struct {
	ID             string  `json:"id"`
	DatasetVersion *string `json:"dataset_version"`
	PromptVersion  *string `json:"prompt_version"`
	Model          *string `json:"model"`
	IndexVersion   *string `json:"index_version"`
	ReportJSON     *string `json:"report_json,omitempty"`
	CreatedAt      string  `json:"created_at"`
}

// SelectedAction is one operator-selected action with the payload hash shown
// at review time (approval binding, SPEC §8.6).
type SelectedAction struct {
	ActionID    string `json:"action_id"`
	PayloadHash string `json:"payload_hash"`
}
