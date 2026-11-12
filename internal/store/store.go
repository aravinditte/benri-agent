package store

// Repository is the contract every process codes against. Implementations
// must enforce the Role capability checks on write methods.
type Repository interface {
	Role() Role
	Close() error
	// Runs fn inside a single transaction; nested calls join the outer tx.
	Transaction(fn func(tx Repository) error) error

	// tenants / users / sessions
	InsertTenant(id, name string) error
	InsertUser(u User) error
	FindUserByEmail(email string) (*User, error)
	FindUserByID(id string) (*User, error)
	CreateSession(tokenHash, userID string) error
	FindUserBySession(tokenHash string) (*User, error)
	DeleteSession(tokenHash string) error

	// inquiries
	InsertInquiry(i Inquiry) (*Inquiry, error)
	GetInquiry(tenantID, id string) (*Inquiry, error)
	ListInquiries(tenantID, status string) ([]Inquiry, error)
	UpdateInquiryStatus(tenantID, id, status string) error

	// CRM fixtures
	InsertCustomer(c Customer) error
	InsertOrder(o Order) error
	FindOrderByNo(tenantID, orderNo string) (*Order, error)
	CountOrdersWithNoAnyTenant(orderNo string) (int, error)
	ListOrders(tenantID string) ([]Order, error)

	// KB
	InsertArticle(a Article, tokenizedText, indexVersion string) error
	GetArticle(tenantID, id string) (*Article, error)
	ListArticles(tenantID string) ([]Article, error)
	ListActiveArticles(tenantID string) ([]Article, error)
	UpdateArticle(tenantID, id string, patch ArticlePatch) (*Article, error)
	ListChunks(tenantID string) ([]Chunk, error)

	// runs & checkpoints
	CreateRun(r Run) (*Run, error)
	GetRun(tenantID, id string) (*Run, error)
	GetRunByID(id string) (*Run, error)
	ListRunsForInquiry(tenantID, inquiryID string) ([]Run, error)
	ClaimNextRun(workerID string, leaseMS int64) (*Run, error)
	HeartbeatRun(id string, leaseMS int64) error
	FinishRun(id string, p FinishRunPatch) error
	SaveCheckpoint(c CheckpointInput) error
	GetCheckpoints(tenantID, runID string) ([]RunStep, error)
	CompletedStepKeys(runID string) (map[string]bool, error)

	// tool calls
	InsertToolCall(t ToolCall) (string, error)
	ListToolCalls(tenantID, runID string) ([]ToolCall, error)

	// drafts & actions
	InsertDraft(d Draft) (*Draft, error)
	GetDraft(tenantID, id string) (*Draft, error)
	GetDraftByRun(tenantID, runID string) (*Draft, error)
	NextRevision(runID string) (int, error)
	SetDraftVerification(tenantID, id, verificationJSON string) error
	SetDraftState(tenantID, id, state string) error
	SupersedeOtherDrafts(tenantID, runID, keepID string) error
	InsertProposedAction(a ProposedAction) (*ProposedAction, error)
	GetAction(tenantID, id string) (*ProposedAction, error)
	GetActionByID(id string) (*ProposedAction, error)
	ListActionsByDraft(tenantID, draftID string) ([]ProposedAction, error)
	SetActionState(id, state string) error
	// TransitionAction atomically moves an action from->to; true only for the winner.
	TransitionAction(id, from, to string) (bool, error)

	// approvals & execution jobs
	InsertApproval(a Approval) error
	GetApprovalForAction(actionID string) (*Approval, error)
	InsertExecutionJob(j ExecutionJob) (*ExecutionJob, error)
	GetJob(id string) (*ExecutionJob, error)
	GetJobByAction(actionID string) (*ExecutionJob, error)
	ClaimNextJob(workerID string, leaseMS int64) (*ExecutionJob, error)
	HeartbeatJob(id string, leaseMS int64) error
	FinishJob(id, state string, resultJSON, failureCode *string) error

	// fixture writes (capability-guarded)
	InsertTicket(t Ticket) (*Ticket, error)
	FindTicketByAction(actionID string) (*Ticket, error)
	ListTickets(tenantID string) ([]Ticket, error)
	InsertTicketNote(n TicketNote) (*TicketNote, error)
	FindNoteByAction(actionID string) (*TicketNote, error)
	InsertOutbox(o OutboxMessage) (*OutboxMessage, error)
	FindOutboxByAction(actionID string) (*OutboxMessage, error)
	ListOutbox(tenantID string) ([]OutboxMessage, error)
	CountTickets() (int, error)
	CountOutbox() (int, error)

	// audit & eval
	InsertAudit(a AuditEvent) error
	ListAudit(tenantID, entityType, entityID string) ([]AuditEvent, error)
	InsertEvalRun(id string, meta EvalRunMeta, reportJSON string) error
	ListEvalRuns() ([]EvalRunRow, error)
	GetEvalRun(id string) (*EvalRunRow, error)
}

type ArticlePatch struct {
	Title         *string
	Body          *string
	Status        *string
	IntentTags    []string // nil = unchanged
	TokenizedText *string  // recomputed by caller when Body changes
	IndexVersion  *string
}

type FinishRunPatch struct {
	State            string
	EscalationReason *string
	HandoffSummary   *string
	FailureCode      *string
	UsageJSON        *string
	TraceID          *string
}

type CheckpointInput struct {
	TenantID       string
	RunID          string
	StepKey        string
	Attempt        int
	State          string
	InputRedacted  *string
	OutputRedacted *string
	PromptVersion  *string
	Model          *string
	DurationMS     *int64
	TraceID        *string
	SpanID         *string
	ErrorCode      *string
}

type EvalRunMeta struct {
	DatasetVersion *string
	PromptVersion  *string
	Model          *string
	IndexVersion   *string
}

// guardWrite enforces the §9 capability table for fixture/approval tables.
func guardWrite(role Role, table string) error {
	var allowed []Role
	switch table {
	case "tickets", "ticket_notes":
		allowed = []Role{RoleTicketMCP, RoleFull}
	case "outbox_messages":
		allowed = []Role{RoleExecutor, RoleFull}
	case "approvals":
		allowed = []Role{RoleAPI, RoleFull}
	case "execution_jobs_insert":
		allowed = []Role{RoleAPI, RoleFull}
	case "execution_jobs_update":
		allowed = []Role{RoleExecutor, RoleFull}
	default:
		return nil
	}
	for _, r := range allowed {
		if r == role {
			return nil
		}
	}
	return &CapabilityError{Msg: "store role " + string(role) + " cannot write to " + table}
}
