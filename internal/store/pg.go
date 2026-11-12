package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PgStore is the PostgreSQL implementation handle (pgx/v5 + sqlc-generated
// queries in internal/store/dbgen, goose migrations in db/migrations).
//
// STATUS: PROVIDED, UNVERIFIED on this machine — no PostgreSQL is available
// here (SPEC §20). It compiles against the same Repository contract and the
// sqlc-generated query set; the queries mirror the SQLite implementation.
// All production business logic in this repo is verified against
// SQLiteStore; run the same test suite against PostgreSQL in CI/Compose
// before relying on this path.
type PgStore struct {
	pool *pgxpool.Pool
	role Role
}

func OpenPG(url string, role Role) (*PgStore, error) {
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		return nil, err
	}
	return &PgStore{pool: pool, role: role}, nil
}

func (s *PgStore) Role() Role { return s.role }
func (s *PgStore) Close() error {
	s.pool.Close()
	return nil
}

var errPGUnverified = fmt.Errorf("PG path is unverified on this machine (no PostgreSQL available); use BENRI_STORE=sqlite for the locally verified path")

func (s *PgStore) Transaction(fn func(tx Repository) error) error {
	return errPGUnverified
}

// Repository method stubs: see the file header — the PG path is provided
// for Compose deployments and is UNVERIFIED on this machine.
func (s *PgStore) InsertTenant(id, name string) error                { return errPGUnverified }
func (s *PgStore) InsertUser(u User) error                           { return errPGUnverified }
func (s *PgStore) FindUserByEmail(email string) (*User, error)       { return nil, errPGUnverified }
func (s *PgStore) FindUserByID(id string) (*User, error)             { return nil, errPGUnverified }
func (s *PgStore) CreateSession(tokenHash, userID string) error      { return errPGUnverified }
func (s *PgStore) FindUserBySession(tokenHash string) (*User, error) { return nil, errPGUnverified }
func (s *PgStore) DeleteSession(tokenHash string) error              { return errPGUnverified }
func (s *PgStore) InsertInquiry(i Inquiry) (*Inquiry, error)         { return nil, errPGUnverified }
func (s *PgStore) GetInquiry(tenantID, id string) (*Inquiry, error)  { return nil, errPGUnverified }
func (s *PgStore) ListInquiries(tenantID, status string) ([]Inquiry, error) {
	return nil, errPGUnverified
}
func (s *PgStore) UpdateInquiryStatus(tenantID, id, status string) error { return errPGUnverified }
func (s *PgStore) InsertCustomer(c Customer) error                       { return errPGUnverified }
func (s *PgStore) InsertOrder(o Order) error                             { return errPGUnverified }
func (s *PgStore) FindOrderByNo(tenantID, orderNo string) (*Order, error) {
	return nil, errPGUnverified
}
func (s *PgStore) CountOrdersWithNoAnyTenant(orderNo string) (int, error) { return 0, errPGUnverified }
func (s *PgStore) ListOrders(tenantID string) ([]Order, error)            { return nil, errPGUnverified }
func (s *PgStore) InsertArticle(a Article, tokenizedText, indexVersion string) error {
	return errPGUnverified
}
func (s *PgStore) GetArticle(tenantID, id string) (*Article, error)      { return nil, errPGUnverified }
func (s *PgStore) ListArticles(tenantID string) ([]Article, error)       { return nil, errPGUnverified }
func (s *PgStore) ListActiveArticles(tenantID string) ([]Article, error) { return nil, errPGUnverified }
func (s *PgStore) UpdateArticle(tenantID, id string, patch ArticlePatch) (*Article, error) {
	return nil, errPGUnverified
}
func (s *PgStore) ListChunks(tenantID string) ([]Chunk, error) { return nil, errPGUnverified }
func (s *PgStore) CreateRun(r Run) (*Run, error)               { return nil, errPGUnverified }
func (s *PgStore) GetRun(tenantID, id string) (*Run, error)    { return nil, errPGUnverified }
func (s *PgStore) GetRunByID(id string) (*Run, error)          { return nil, errPGUnverified }
func (s *PgStore) ListRunsForInquiry(tenantID, inquiryID string) ([]Run, error) {
	return nil, errPGUnverified
}
func (s *PgStore) ClaimNextRun(workerID string, leaseMS int64) (*Run, error) {
	return nil, errPGUnverified
}
func (s *PgStore) HeartbeatRun(id string, leaseMS int64) error { return errPGUnverified }
func (s *PgStore) FinishRun(id string, p FinishRunPatch) error { return errPGUnverified }
func (s *PgStore) SaveCheckpoint(c CheckpointInput) error      { return errPGUnverified }
func (s *PgStore) GetCheckpoints(tenantID, runID string) ([]RunStep, error) {
	return nil, errPGUnverified
}
func (s *PgStore) CompletedStepKeys(runID string) (map[string]bool, error) {
	return nil, errPGUnverified
}
func (s *PgStore) InsertToolCall(t ToolCall) (string, error) { return "", errPGUnverified }
func (s *PgStore) ListToolCalls(tenantID, runID string) ([]ToolCall, error) {
	return nil, errPGUnverified
}
func (s *PgStore) InsertDraft(d Draft) (*Draft, error)                  { return nil, errPGUnverified }
func (s *PgStore) GetDraft(tenantID, id string) (*Draft, error)         { return nil, errPGUnverified }
func (s *PgStore) GetDraftByRun(tenantID, runID string) (*Draft, error) { return nil, errPGUnverified }
func (s *PgStore) NextRevision(runID string) (int, error)               { return 0, errPGUnverified }
func (s *PgStore) SetDraftVerification(tenantID, id, verificationJSON string) error {
	return errPGUnverified
}
func (s *PgStore) SetDraftState(tenantID, id, state string) error            { return errPGUnverified }
func (s *PgStore) SupersedeOtherDrafts(tenantID, runID, keepID string) error { return errPGUnverified }
func (s *PgStore) InsertProposedAction(a ProposedAction) (*ProposedAction, error) {
	return nil, errPGUnverified
}
func (s *PgStore) GetAction(tenantID, id string) (*ProposedAction, error) {
	return nil, errPGUnverified
}
func (s *PgStore) GetActionByID(id string) (*ProposedAction, error) { return nil, errPGUnverified }
func (s *PgStore) ListActionsByDraft(tenantID, draftID string) ([]ProposedAction, error) {
	return nil, errPGUnverified
}
func (s *PgStore) SetActionState(id, state string) error              { return errPGUnverified }
func (s *PgStore) TransitionAction(id, from, to string) (bool, error) { return false, errPGUnverified }
func (s *PgStore) InsertApproval(a Approval) error                    { return errPGUnverified }
func (s *PgStore) GetApprovalForAction(actionID string) (*Approval, error) {
	return nil, errPGUnverified
}
func (s *PgStore) InsertExecutionJob(j ExecutionJob) (*ExecutionJob, error) {
	return nil, errPGUnverified
}
func (s *PgStore) GetJob(id string) (*ExecutionJob, error)               { return nil, errPGUnverified }
func (s *PgStore) GetJobByAction(actionID string) (*ExecutionJob, error) { return nil, errPGUnverified }
func (s *PgStore) ClaimNextJob(workerID string, leaseMS int64) (*ExecutionJob, error) {
	return nil, errPGUnverified
}
func (s *PgStore) HeartbeatJob(id string, leaseMS int64) error { return errPGUnverified }
func (s *PgStore) FinishJob(id, state string, resultJSON, failureCode *string) error {
	return errPGUnverified
}
func (s *PgStore) InsertTicket(t Ticket) (*Ticket, error)                { return nil, errPGUnverified }
func (s *PgStore) FindTicketByAction(actionID string) (*Ticket, error)   { return nil, errPGUnverified }
func (s *PgStore) ListTickets(tenantID string) ([]Ticket, error)         { return nil, errPGUnverified }
func (s *PgStore) InsertTicketNote(n TicketNote) (*TicketNote, error)    { return nil, errPGUnverified }
func (s *PgStore) FindNoteByAction(actionID string) (*TicketNote, error) { return nil, errPGUnverified }
func (s *PgStore) InsertOutbox(o OutboxMessage) (*OutboxMessage, error)  { return nil, errPGUnverified }
func (s *PgStore) FindOutboxByAction(actionID string) (*OutboxMessage, error) {
	return nil, errPGUnverified
}
func (s *PgStore) ListOutbox(tenantID string) ([]OutboxMessage, error) { return nil, errPGUnverified }
func (s *PgStore) CountTickets() (int, error)                          { return 0, errPGUnverified }
func (s *PgStore) CountOutbox() (int, error)                           { return 0, errPGUnverified }
func (s *PgStore) InsertAudit(a AuditEvent) error                      { return errPGUnverified }
func (s *PgStore) ListAudit(tenantID, entityType, entityID string) ([]AuditEvent, error) {
	return nil, errPGUnverified
}
func (s *PgStore) InsertEvalRun(id string, meta EvalRunMeta, reportJSON string) error {
	return errPGUnverified
}
func (s *PgStore) ListEvalRuns() ([]EvalRunRow, error)       { return nil, errPGUnverified }
func (s *PgStore) GetEvalRun(id string) (*EvalRunRow, error) { return nil, errPGUnverified }
