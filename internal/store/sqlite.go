package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore is the locally verified store implementation (pure-Go driver,
// no cgo). It implements the same Repository contract as the PostgreSQL path.
type SQLiteStore struct {
	db   *sql.DB
	role Role
	q    querier // db or active tx
	tx   bool
	// dialect is "sqlite" for the local store and "postgres" when this
	// implementation is reused behind PgStore. Keeping the repository logic
	// in one place prevents the two database paths from drifting.
	dialect string
}

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const sqliteDDL = `
CREATE TABLE IF NOT EXISTS tenants (id TEXT PRIMARY KEY, name TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'active');
CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, role TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'active');
CREATE TABLE IF NOT EXISTS sessions (token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS inquiries (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, source TEXT NOT NULL DEFAULT 'seed', subject TEXT NOT NULL, redacted_body TEXT NOT NULL, normalized_body_hash TEXT NOT NULL, order_no_hint TEXT, status TEXT NOT NULL DEFAULT 'new', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS customers (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, synthetic_name TEXT NOT NULL, synthetic_contact_ref TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS orders (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, customer_id TEXT NOT NULL, order_no TEXT NOT NULL, item TEXT NOT NULL, ordered_at TEXT NOT NULL, delivery_status TEXT NOT NULL, UNIQUE(tenant_id, order_no));
CREATE TABLE IF NOT EXISTS kb_articles (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, title TEXT NOT NULL, intent_tags TEXT NOT NULL, body TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1, effective_from TEXT, effective_to TEXT, status TEXT NOT NULL DEFAULT 'active', updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS kb_chunks (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, article_id TEXT NOT NULL, chunk_index INTEGER NOT NULL, text TEXT NOT NULL, tokenized_text TEXT NOT NULL, embedding TEXT, embedding_model TEXT, index_version TEXT NOT NULL DEFAULT 'lexical-v1');
CREATE TABLE IF NOT EXISTS runs (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, inquiry_id TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'queued', model_route TEXT, prompt_version TEXT, config_snapshot TEXT, usage_json TEXT, cost_jpy REAL, trace_id TEXT, lease_owner TEXT, lease_expires_at TEXT, heartbeat_at TEXT, attempt_count INTEGER NOT NULL DEFAULT 0, last_error_code TEXT, idempotency_key TEXT UNIQUE, escalation_reason TEXT, handoff_summary TEXT, started_at TEXT, finished_at TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS run_steps (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, run_id TEXT NOT NULL, step_key TEXT NOT NULL, attempt INTEGER NOT NULL, state TEXT NOT NULL, input_redacted TEXT, output_redacted TEXT, prompt_version TEXT, model TEXT, started_at TEXT, finished_at TEXT, duration_ms INTEGER, trace_id TEXT, span_id TEXT, error_code TEXT, UNIQUE(run_id, step_key, attempt));
CREATE TABLE IF NOT EXISTS tool_calls (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, run_id TEXT, server TEXT NOT NULL, tool TEXT NOT NULL, risk_level TEXT NOT NULL, args_redacted TEXT, status TEXT NOT NULL, latency_ms INTEGER, trace_id TEXT, span_id TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS drafts (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, run_id TEXT NOT NULL, revision INTEGER NOT NULL, body TEXT NOT NULL, cited_article_ids TEXT NOT NULL, factual_references TEXT NOT NULL, verification_json TEXT, content_hash TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'pending_review', created_at TEXT NOT NULL, UNIQUE(run_id, revision));
CREATE TABLE IF NOT EXISTS proposed_actions (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, draft_id TEXT NOT NULL, run_id TEXT, action_type TEXT NOT NULL, canonical_payload TEXT NOT NULL, payload_hash TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'proposed', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS approvals (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, draft_id TEXT NOT NULL, draft_revision INTEGER NOT NULL, draft_hash TEXT NOT NULL, operator_id TEXT NOT NULL, selected_action_ids TEXT NOT NULL, policy_version TEXT NOT NULL, approved_at TEXT NOT NULL, rejected_reason TEXT);
CREATE TABLE IF NOT EXISTS execution_jobs (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, proposed_action_id TEXT NOT NULL UNIQUE, idempotency_key TEXT NOT NULL UNIQUE, state TEXT NOT NULL DEFAULT 'queued', lease_owner TEXT, lease_expires_at TEXT, heartbeat_at TEXT, attempts INTEGER NOT NULL DEFAULT 0, result_json TEXT, failure_code TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS tickets (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, inquiry_id TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'open', priority TEXT NOT NULL DEFAULT 'normal', source_action_id TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS ticket_notes (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, ticket_id TEXT NOT NULL, body TEXT NOT NULL, source_action_id TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS outbox_messages (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, inquiry_id TEXT NOT NULL, draft_revision INTEGER NOT NULL, body TEXT NOT NULL, delivery_state TEXT NOT NULL DEFAULT 'sent_demo', source_action_id TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS audit_events (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, actor_type TEXT NOT NULL, actor_id TEXT, action TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, before_hash TEXT, after_hash TEXT, trace_id TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS eval_runs (id TEXT PRIMARY KEY, dataset_version TEXT, prompt_version TEXT, model TEXT, index_version TEXT, report_json TEXT, created_at TEXT NOT NULL);
`

func OpenSQLite(path string, role Role) (*SQLiteStore, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(sqliteDDL); err != nil {
		return nil, err
	}
	return &SQLiteStore{db: db, role: role, q: db, dialect: "sqlite"}, nil
}

func (s *SQLiteStore) Role() Role { return s.role }
func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *SQLiteStore) Transaction(fn func(tx Repository) error) error {
	if s.tx {
		return fn(s)
	}
	t, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	var q querier = t
	if s.dialect == "postgres" {
		q = postgresQuerier{q: t}
	}
	child := &SQLiteStore{db: s.db, role: s.role, q: q, tx: true, dialect: s.dialect}
	if err := fn(child); err != nil {
		_ = t.Rollback()
		return err
	}
	return t.Commit()
}

var ctx = context.Background()

func ns(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}
func ps(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}
func ni(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}
func pi(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}
func nf(p *float64) sql.NullFloat64 {
	if p == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *p, Valid: true}
}
func pf(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}
func strp(s string) *string { return &s }

// ---- tenants / users / sessions ----

func (s *SQLiteStore) InsertTenant(id, name string) error {
	_, err := s.q.ExecContext(ctx, "INSERT OR IGNORE INTO tenants(id,name) VALUES(?,?)", id, name)
	return err
}
func (s *SQLiteStore) InsertUser(u User) error {
	_, err := s.q.ExecContext(ctx, "INSERT OR IGNORE INTO users(id,tenant_id,email,password_hash,role) VALUES(?,?,?,?,?)", u.ID, u.TenantID, u.Email, u.PasswordHash, u.Role)
	return err
}

func scanUser(row *sql.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}
func (s *SQLiteStore) FindUserByEmail(email string) (*User, error) {
	return scanUser(s.q.QueryRowContext(ctx, "SELECT id,tenant_id,email,password_hash,role,status FROM users WHERE email=?", email))
}
func (s *SQLiteStore) FindUserByID(id string) (*User, error) {
	return scanUser(s.q.QueryRowContext(ctx, "SELECT id,tenant_id,email,password_hash,role,status FROM users WHERE id=?", id))
}
func (s *SQLiteStore) CreateSession(tokenHash, userID string) error {
	_, err := s.q.ExecContext(ctx, "INSERT INTO sessions(token_hash,user_id,created_at) VALUES(?,?,?)", tokenHash, userID, NowISO())
	return err
}
func (s *SQLiteStore) FindUserBySession(tokenHash string) (*User, error) {
	return scanUser(s.q.QueryRowContext(ctx, "SELECT u.id,u.tenant_id,u.email,u.password_hash,u.role,u.status FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=?", tokenHash))
}
func (s *SQLiteStore) DeleteSession(tokenHash string) error {
	_, err := s.q.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=?", tokenHash)
	return err
}

// ---- inquiries ----

func (s *SQLiteStore) InsertInquiry(i Inquiry) (*Inquiry, error) {
	if i.Source == "" {
		i.Source = "seed"
	}
	if i.Status == "" {
		i.Status = "new"
	}
	_, err := s.q.ExecContext(ctx, "INSERT OR IGNORE INTO inquiries(id,tenant_id,source,subject,redacted_body,normalized_body_hash,order_no_hint,status,created_at) VALUES(?,?,?,?,?,?,?,?,?)",
		i.ID, i.TenantID, i.Source, i.Subject, i.RedactedBody, i.NormalizedBodyHash, ns(i.OrderNoHint), i.Status, NowISO())
	if err != nil {
		return nil, err
	}
	return s.GetInquiry(i.TenantID, i.ID)
}

func scanInquiry(row *sql.Row) (*Inquiry, error) {
	var i Inquiry
	var hint sql.NullString
	err := row.Scan(&i.ID, &i.TenantID, &i.Source, &i.Subject, &i.RedactedBody, &i.NormalizedBodyHash, &hint, &i.Status, &i.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	i.OrderNoHint = ps(hint)
	return &i, err
}
func scanInquiries(rows *sql.Rows) ([]Inquiry, error) {
	out := []Inquiry{}
	for rows.Next() {
		var i Inquiry
		var hint sql.NullString
		if err := rows.Scan(&i.ID, &i.TenantID, &i.Source, &i.Subject, &i.RedactedBody, &i.NormalizedBodyHash, &hint, &i.Status, &i.CreatedAt); err != nil {
			return nil, err
		}
		i.OrderNoHint = ps(hint)
		out = append(out, i)
	}
	return out, rows.Err()
}

const inquiryCols = "id,tenant_id,source,subject,redacted_body,normalized_body_hash,order_no_hint,status,created_at"

func (s *SQLiteStore) GetInquiry(tenantID, id string) (*Inquiry, error) {
	return scanInquiry(s.q.QueryRowContext(ctx, "SELECT "+inquiryCols+" FROM inquiries WHERE tenant_id=? AND id=?", tenantID, id))
}
func (s *SQLiteStore) ListInquiries(tenantID, status string) ([]Inquiry, error) {
	var rows *sql.Rows
	var err error
	if status != "" {
		rows, err = s.q.QueryContext(ctx, "SELECT "+inquiryCols+" FROM inquiries WHERE tenant_id=? AND status=? ORDER BY created_at", tenantID, status)
	} else {
		rows, err = s.q.QueryContext(ctx, "SELECT "+inquiryCols+" FROM inquiries WHERE tenant_id=? ORDER BY created_at", tenantID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInquiries(rows)
}
func (s *SQLiteStore) UpdateInquiryStatus(tenantID, id, status string) error {
	_, err := s.q.ExecContext(ctx, "UPDATE inquiries SET status=? WHERE tenant_id=? AND id=?", status, tenantID, id)
	return err
}

// ---- CRM ----

func (s *SQLiteStore) InsertCustomer(c Customer) error {
	_, err := s.q.ExecContext(ctx, "INSERT OR IGNORE INTO customers(id,tenant_id,synthetic_name,synthetic_contact_ref) VALUES(?,?,?,?)", c.ID, c.TenantID, c.SyntheticName, c.SyntheticContactRef)
	return err
}
func (s *SQLiteStore) InsertOrder(o Order) error {
	_, err := s.q.ExecContext(ctx, "INSERT OR IGNORE INTO orders(id,tenant_id,customer_id,order_no,item,ordered_at,delivery_status) VALUES(?,?,?,?,?,?,?)", o.ID, o.TenantID, o.CustomerID, o.OrderNo, o.Item, o.OrderedAt, o.DeliveryStatus)
	return err
}

const orderCols = "o.id,o.tenant_id,o.customer_id,o.order_no,o.item,o.ordered_at,o.delivery_status,c.synthetic_name"

func scanOrder(row *sql.Row) (*Order, error) {
	var o Order
	err := row.Scan(&o.ID, &o.TenantID, &o.CustomerID, &o.OrderNo, &o.Item, &o.OrderedAt, &o.DeliveryStatus, &o.CustomerName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &o, err
}
func (s *SQLiteStore) FindOrderByNo(tenantID, orderNo string) (*Order, error) {
	return scanOrder(s.q.QueryRowContext(ctx, "SELECT "+orderCols+" FROM orders o JOIN customers c ON c.id=o.customer_id WHERE o.tenant_id=? AND o.order_no=?", tenantID, orderNo))
}
func (s *SQLiteStore) CountOrdersWithNoAnyTenant(orderNo string) (int, error) {
	var n int
	err := s.q.QueryRowContext(ctx, "SELECT COUNT(*) FROM orders WHERE order_no=?", orderNo).Scan(&n)
	return n, err
}
func (s *SQLiteStore) ListOrders(tenantID string) ([]Order, error) {
	rows, err := s.q.QueryContext(ctx, "SELECT id,tenant_id,customer_id,order_no,item,ordered_at,delivery_status FROM orders WHERE tenant_id=? ORDER BY order_no", tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.TenantID, &o.CustomerID, &o.OrderNo, &o.Item, &o.OrderedAt, &o.DeliveryStatus); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ---- KB ----

func (s *SQLiteStore) InsertArticle(a Article, tokenizedText, indexVersion string) error {
	tags, _ := json.Marshal(a.IntentTags)
	if a.Version == 0 {
		a.Version = 1
	}
	if a.Status == "" {
		a.Status = "active"
	}
	_, err := s.q.ExecContext(ctx, "INSERT OR REPLACE INTO kb_articles(id,tenant_id,title,intent_tags,body,version,status,updated_at) VALUES(?,?,?,?,?,?,?,?)",
		a.ID, a.TenantID, a.Title, string(tags), a.Body, a.Version, a.Status, NowISO())
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx, "INSERT OR REPLACE INTO kb_chunks(id,tenant_id,article_id,chunk_index,text,tokenized_text,index_version) VALUES(?,?,?,?,?,?,?)",
		a.ID+"-c0", a.TenantID, a.ID, 0, a.Body, tokenizedText, indexVersion)
	return err
}

func scanArticleRow(scan func(...any) error) (*Article, error) {
	var a Article
	var tags string
	err := scan(&a.ID, &a.TenantID, &a.Title, &tags, &a.Body, &a.Version, &a.Status, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(tags), &a.IntentTags)
	if a.IntentTags == nil {
		a.IntentTags = []string{}
	}
	return &a, nil
}

const articleCols = "id,tenant_id,title,intent_tags,body,version,status,updated_at"

func (s *SQLiteStore) GetArticle(tenantID, id string) (*Article, error) {
	return scanArticleRow(s.q.QueryRowContext(ctx, "SELECT "+articleCols+" FROM kb_articles WHERE tenant_id=? AND id=?", tenantID, id).Scan)
}
func (s *SQLiteStore) listArticlesQuery(query string, args ...any) ([]Article, error) {
	rows, err := s.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Article{}
	for rows.Next() {
		a, err := scanArticleRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}
func (s *SQLiteStore) ListArticles(tenantID string) ([]Article, error) {
	return s.listArticlesQuery("SELECT "+articleCols+" FROM kb_articles WHERE tenant_id=? ORDER BY id", tenantID)
}
func (s *SQLiteStore) ListActiveArticles(tenantID string) ([]Article, error) {
	return s.listArticlesQuery("SELECT "+articleCols+" FROM kb_articles WHERE tenant_id=? AND status='active' ORDER BY id", tenantID)
}
func (s *SQLiteStore) UpdateArticle(tenantID, id string, patch ArticlePatch) (*Article, error) {
	cur, err := s.GetArticle(tenantID, id)
	if err != nil || cur == nil {
		if err == nil {
			err = &NotFoundError{What: "article " + id}
		}
		return nil, err
	}
	if patch.Title != nil {
		cur.Title = *patch.Title
	}
	if patch.Body != nil {
		cur.Body = *patch.Body
	}
	if patch.Status != nil {
		cur.Status = *patch.Status
	}
	if patch.IntentTags != nil {
		cur.IntentTags = patch.IntentTags
	}
	tags, _ := json.Marshal(cur.IntentTags)
	_, err = s.q.ExecContext(ctx, "UPDATE kb_articles SET title=?, body=?, status=?, intent_tags=?, version=version+1, updated_at=? WHERE tenant_id=? AND id=?",
		cur.Title, cur.Body, cur.Status, string(tags), NowISO(), tenantID, id)
	if err != nil {
		return nil, err
	}
	if patch.Body != nil {
		if _, err := s.q.ExecContext(ctx, "UPDATE kb_chunks SET text=? WHERE tenant_id=? AND article_id=?", cur.Body, tenantID, id); err != nil {
			return nil, err
		}
	}
	if patch.TokenizedText != nil {
		if _, err := s.q.ExecContext(ctx, "UPDATE kb_chunks SET tokenized_text=? WHERE tenant_id=? AND article_id=?", *patch.TokenizedText, tenantID, id); err != nil {
			return nil, err
		}
	}
	if patch.IndexVersion != nil {
		if _, err := s.q.ExecContext(ctx, "UPDATE kb_chunks SET index_version=? WHERE tenant_id=? AND article_id=?", *patch.IndexVersion, tenantID, id); err != nil {
			return nil, err
		}
	}
	return s.GetArticle(tenantID, id)
}
func (s *SQLiteStore) ListChunks(tenantID string) ([]Chunk, error) {
	rows, err := s.q.QueryContext(ctx, "SELECT id,tenant_id,article_id,chunk_index,text,tokenized_text,index_version FROM kb_chunks WHERE tenant_id=? ORDER BY article_id, chunk_index", tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Chunk{}
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.TenantID, &c.ArticleID, &c.ChunkIndex, &c.Text, &c.TokenizedText, &c.IndexVersion); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- runs & checkpoints ----

func (s *SQLiteStore) CreateRun(r Run) (*Run, error) {
	if r.IdempotencyKey != nil {
		var existing Run
		row := s.q.QueryRowContext(ctx, "SELECT id FROM runs WHERE idempotency_key=?", *r.IdempotencyKey)
		var eid string
		if err := row.Scan(&eid); err == nil {
			return s.GetRunByID(eid)
		}
		_ = existing
	}
	_, err := s.q.ExecContext(ctx, "INSERT INTO runs(id,tenant_id,inquiry_id,state,prompt_version,model_route,trace_id,config_snapshot,idempotency_key,created_at) VALUES(?,?,?,'queued',?,?,?,?,?,?)",
		r.ID, r.TenantID, r.InquiryID, ns(r.PromptVersion), ns(r.ModelRoute), ns(r.TraceID), ns(r.ConfigSnapshot), ns(r.IdempotencyKey), NowISO())
	if err != nil {
		return nil, err
	}
	return s.GetRun(r.TenantID, r.ID)
}

const runCols = "id,tenant_id,inquiry_id,state,model_route,prompt_version,config_snapshot,usage_json,cost_jpy,trace_id,lease_owner,lease_expires_at,heartbeat_at,attempt_count,last_error_code,idempotency_key,escalation_reason,handoff_summary,started_at,finished_at,created_at"

func scanRunRow(scan func(...any) error) (*Run, error) {
	var r Run
	var model, prompt, cfg, usage, trace, lease, leaseExp, hb, lerr, idem, esc, handoff, started, finished sql.NullString
	var cost sql.NullFloat64
	err := scan(&r.ID, &r.TenantID, &r.InquiryID, &r.State, &model, &prompt, &cfg, &usage, &cost, &trace, &lease, &leaseExp, &hb, &r.AttemptCount, &lerr, &idem, &esc, &handoff, &started, &finished, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.ModelRoute, r.PromptVersion, r.ConfigSnapshot, r.UsageJSON = ps(model), ps(prompt), ps(cfg), ps(usage)
	r.CostJPY = pf(cost)
	r.TraceID, r.LeaseOwner, r.LeaseExpiresAt, r.HeartbeatAt = ps(trace), ps(lease), ps(leaseExp), ps(hb)
	r.LastErrorCode, r.IdempotencyKey, r.EscalationReason, r.HandoffSummary = ps(lerr), ps(idem), ps(esc), ps(handoff)
	r.StartedAt, r.FinishedAt = ps(started), ps(finished)
	return &r, nil
}
func (s *SQLiteStore) GetRun(tenantID, id string) (*Run, error) {
	return scanRunRow(s.q.QueryRowContext(ctx, "SELECT "+runCols+" FROM runs WHERE tenant_id=? AND id=?", tenantID, id).Scan)
}
func (s *SQLiteStore) GetRunByID(id string) (*Run, error) {
	return scanRunRow(s.q.QueryRowContext(ctx, "SELECT "+runCols+" FROM runs WHERE id=?", id).Scan)
}
func (s *SQLiteStore) ListRunsForInquiry(tenantID, inquiryID string) ([]Run, error) {
	rows, err := s.q.QueryContext(ctx, "SELECT "+runCols+" FROM runs WHERE tenant_id=? AND inquiry_id=? ORDER BY created_at", tenantID, inquiryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, err := scanRunRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}
func (s *SQLiteStore) ClaimNextRun(workerID string, leaseMS int64) (*Run, error) {
	var claimed *Run
	err := s.Transaction(func(tx Repository) error {
		st := tx.(*SQLiteStore)
		row := st.q.QueryRowContext(ctx, "SELECT id FROM runs WHERE state='queued' OR (state='running' AND lease_expires_at IS NOT NULL AND lease_expires_at < ?) ORDER BY created_at LIMIT 1", NowISO())
		var id string
		if err := row.Scan(&id); err != nil {
			return nil // no work
		}
		expires := time.Now().Add(time.Duration(leaseMS) * time.Millisecond).UTC().Format("2006-01-02T15:04:05.000Z07:00")
		now := NowISO()
		if _, err := st.q.ExecContext(ctx, "UPDATE runs SET state='running', lease_owner=?, lease_expires_at=?, heartbeat_at=?, attempt_count=attempt_count+1, started_at=COALESCE(started_at,?) WHERE id=?", workerID, expires, now, now, id); err != nil {
			return err
		}
		r, err := st.GetRunByID(id)
		claimed = r
		return err
	})
	return claimed, err
}
func (s *SQLiteStore) HeartbeatRun(id string, leaseMS int64) error {
	expires := time.Now().Add(time.Duration(leaseMS) * time.Millisecond).UTC().Format("2006-01-02T15:04:05.000Z07:00")
	_, err := s.q.ExecContext(ctx, "UPDATE runs SET heartbeat_at=?, lease_expires_at=? WHERE id=?", NowISO(), expires, id)
	return err
}
func (s *SQLiteStore) FinishRun(id string, p FinishRunPatch) error {
	_, err := s.q.ExecContext(ctx, "UPDATE runs SET state=?, escalation_reason=?, handoff_summary=?, last_error_code=?, usage_json=?, trace_id=COALESCE(?,trace_id), finished_at=?, lease_owner=NULL, lease_expires_at=NULL WHERE id=?",
		p.State, ns(p.EscalationReason), ns(p.HandoffSummary), ns(p.FailureCode), ns(p.UsageJSON), ns(p.TraceID), NowISO(), id)
	return err
}
func (s *SQLiteStore) SaveCheckpoint(c CheckpointInput) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO run_steps(id,tenant_id,run_id,step_key,attempt,state,input_redacted,output_redacted,prompt_version,model,started_at,finished_at,duration_ms,trace_id,span_id,error_code)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(run_id,step_key,attempt) DO UPDATE SET state=excluded.state, output_redacted=excluded.output_redacted, error_code=excluded.error_code, finished_at=excluded.finished_at, duration_ms=excluded.duration_ms`,
		NewID("step"), c.TenantID, c.RunID, c.StepKey, c.Attempt, c.State, ns(c.InputRedacted), ns(c.OutputRedacted), ns(c.PromptVersion), ns(c.Model), NowISO(), NowISO(), ni(c.DurationMS), ns(c.TraceID), ns(c.SpanID), ns(c.ErrorCode))
	return err
}
func (s *SQLiteStore) GetCheckpoints(tenantID, runID string) ([]RunStep, error) {
	rows, err := s.q.QueryContext(ctx, "SELECT id,tenant_id,run_id,step_key,attempt,state,input_redacted,output_redacted,prompt_version,model,started_at,finished_at,duration_ms,trace_id,span_id,error_code FROM run_steps WHERE tenant_id=? AND run_id=? ORDER BY rowid", tenantID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RunStep{}
	for rows.Next() {
		var st RunStep
		var in, outp, prompt, model, started, finished, trace, span, ecode sql.NullString
		var dur sql.NullInt64
		if err := rows.Scan(&st.ID, &st.TenantID, &st.RunID, &st.StepKey, &st.Attempt, &st.State, &in, &outp, &prompt, &model, &started, &finished, &dur, &trace, &span, &ecode); err != nil {
			return nil, err
		}
		st.InputRedacted, st.OutputRedacted, st.PromptVersion, st.Model = ps(in), ps(outp), ps(prompt), ps(model)
		st.StartedAt, st.FinishedAt, st.TraceID, st.SpanID, st.ErrorCode = ps(started), ps(finished), ps(trace), ps(span), ps(ecode)
		st.DurationMS = pi(dur)
		out = append(out, st)
	}
	return out, rows.Err()
}
func (s *SQLiteStore) CompletedStepKeys(runID string) (map[string]bool, error) {
	rows, err := s.q.QueryContext(ctx, "SELECT DISTINCT step_key FROM run_steps WHERE run_id=? AND state='completed'", runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// ---- tool calls ----

func (s *SQLiteStore) InsertToolCall(t ToolCall) (string, error) {
	id := NewID("tool")
	_, err := s.q.ExecContext(ctx, "INSERT INTO tool_calls(id,tenant_id,run_id,server,tool,risk_level,args_redacted,status,latency_ms,trace_id,span_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)",
		id, t.TenantID, ns(t.RunID), t.Server, t.Tool, t.RiskLevel, ns(t.ArgsRedacted), t.Status, ni(t.LatencyMS), ns(t.TraceID), ns(t.SpanID), NowISO())
	return id, err
}
func (s *SQLiteStore) ListToolCalls(tenantID, runID string) ([]ToolCall, error) {
	rows, err := s.q.QueryContext(ctx, "SELECT id,tenant_id,run_id,server,tool,risk_level,args_redacted,status,latency_ms,trace_id,span_id,created_at FROM tool_calls WHERE tenant_id=? AND run_id=? ORDER BY created_at", tenantID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ToolCall{}
	for rows.Next() {
		var t ToolCall
		var runID, args, trace, span sql.NullString
		var lat sql.NullInt64
		if err := rows.Scan(&t.ID, &t.TenantID, &runID, &t.Server, &t.Tool, &t.RiskLevel, &args, &t.Status, &lat, &trace, &span, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.RunID, t.ArgsRedacted, t.TraceID, t.SpanID = ps(runID), ps(args), ps(trace), ps(span)
		t.LatencyMS = pi(lat)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---- drafts & actions ----

const draftCols = "id,tenant_id,run_id,revision,body,cited_article_ids,factual_references,verification_json,content_hash,state,created_at"

func scanDraftRow(scan func(...any) error) (*Draft, error) {
	var d Draft
	var ver sql.NullString
	err := scan(&d.ID, &d.TenantID, &d.RunID, &d.Revision, &d.Body, &d.CitedArticleIDs, &d.FactualReferences, &ver, &d.ContentHash, &d.State, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.VerificationJSON = ps(ver)
	return &d, nil
}
func (s *SQLiteStore) InsertDraft(d Draft) (*Draft, error) {
	if d.State == "" {
		d.State = "pending_review"
	}
	_, err := s.q.ExecContext(ctx, "INSERT INTO drafts(id,tenant_id,run_id,revision,body,cited_article_ids,factual_references,verification_json,content_hash,state,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)",
		d.ID, d.TenantID, d.RunID, d.Revision, d.Body, d.CitedArticleIDs, d.FactualReferences, ns(d.VerificationJSON), d.ContentHash, d.State, NowISO())
	if err != nil {
		return nil, err
	}
	return s.GetDraft(d.TenantID, d.ID)
}
func (s *SQLiteStore) GetDraft(tenantID, id string) (*Draft, error) {
	return scanDraftRow(s.q.QueryRowContext(ctx, "SELECT "+draftCols+" FROM drafts WHERE tenant_id=? AND id=?", tenantID, id).Scan)
}
func (s *SQLiteStore) GetDraftByRun(tenantID, runID string) (*Draft, error) {
	return scanDraftRow(s.q.QueryRowContext(ctx, "SELECT "+draftCols+" FROM drafts WHERE tenant_id=? AND run_id=? ORDER BY revision DESC LIMIT 1", tenantID, runID).Scan)
}
func (s *SQLiteStore) NextRevision(runID string) (int, error) {
	var m sql.NullInt64
	err := s.q.QueryRowContext(ctx, "SELECT MAX(revision) FROM drafts WHERE run_id=?", runID).Scan(&m)
	if err != nil {
		return 0, err
	}
	if !m.Valid {
		return 1, nil
	}
	return int(m.Int64) + 1, nil
}
func (s *SQLiteStore) SetDraftVerification(tenantID, id, verificationJSON string) error {
	_, err := s.q.ExecContext(ctx, "UPDATE drafts SET verification_json=? WHERE tenant_id=? AND id=?", verificationJSON, tenantID, id)
	return err
}
func (s *SQLiteStore) SetDraftState(tenantID, id, state string) error {
	_, err := s.q.ExecContext(ctx, "UPDATE drafts SET state=? WHERE tenant_id=? AND id=?", state, tenantID, id)
	return err
}
func (s *SQLiteStore) SupersedeOtherDrafts(tenantID, runID, keepID string) error {
	_, err := s.q.ExecContext(ctx, "UPDATE drafts SET state='superseded' WHERE tenant_id=? AND run_id=? AND id<>? AND state='pending_review'", tenantID, runID, keepID)
	return err
}

const actionCols = "id,tenant_id,draft_id,run_id,action_type,canonical_payload,payload_hash,state,created_at"

func scanActionRow(scan func(...any) error) (*ProposedAction, error) {
	var a ProposedAction
	var runID sql.NullString
	err := scan(&a.ID, &a.TenantID, &a.DraftID, &runID, &a.ActionType, &a.CanonicalPayload, &a.PayloadHash, &a.State, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.RunID = ps(runID)
	return &a, nil
}
func (s *SQLiteStore) InsertProposedAction(a ProposedAction) (*ProposedAction, error) {
	if a.State == "" {
		a.State = "proposed"
	}
	_, err := s.q.ExecContext(ctx, "INSERT INTO proposed_actions(id,tenant_id,draft_id,run_id,action_type,canonical_payload,payload_hash,state,created_at) VALUES(?,?,?,?,?,?,?,?,?)",
		a.ID, a.TenantID, a.DraftID, ns(a.RunID), a.ActionType, a.CanonicalPayload, a.PayloadHash, a.State, NowISO())
	if err != nil {
		return nil, err
	}
	return s.GetAction(a.TenantID, a.ID)
}
func (s *SQLiteStore) GetAction(tenantID, id string) (*ProposedAction, error) {
	return scanActionRow(s.q.QueryRowContext(ctx, "SELECT "+actionCols+" FROM proposed_actions WHERE tenant_id=? AND id=?", tenantID, id).Scan)
}
func (s *SQLiteStore) GetActionByID(id string) (*ProposedAction, error) {
	return scanActionRow(s.q.QueryRowContext(ctx, "SELECT "+actionCols+" FROM proposed_actions WHERE id=?", id).Scan)
}
func (s *SQLiteStore) ListActionsByDraft(tenantID, draftID string) ([]ProposedAction, error) {
	rows, err := s.q.QueryContext(ctx, "SELECT "+actionCols+" FROM proposed_actions WHERE tenant_id=? AND draft_id=? ORDER BY created_at", tenantID, draftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProposedAction{}
	for rows.Next() {
		a, err := scanActionRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}
func (s *SQLiteStore) SetActionState(id, state string) error {
	_, err := s.q.ExecContext(ctx, "UPDATE proposed_actions SET state=? WHERE id=?", state, id)
	return err
}
func (s *SQLiteStore) TransitionAction(id, from, to string) (bool, error) {
	res, err := s.q.ExecContext(ctx, "UPDATE proposed_actions SET state=? WHERE id=? AND state=?", to, id, from)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ---- approvals & execution jobs ----

func (s *SQLiteStore) InsertApproval(a Approval) error {
	if err := guardWrite(s.role, "approvals"); err != nil {
		return err
	}
	_, err := s.q.ExecContext(ctx, "INSERT INTO approvals(id,tenant_id,draft_id,draft_revision,draft_hash,operator_id,selected_action_ids,policy_version,approved_at,rejected_reason) VALUES(?,?,?,?,?,?,?,?,?,?)",
		a.ID, a.TenantID, a.DraftID, a.DraftRevision, a.DraftHash, a.OperatorID, a.SelectedActionIDs, a.PolicyVersion, NowISO(), ns(a.RejectedReason))
	return err
}
func (s *SQLiteStore) GetApprovalForAction(actionID string) (*Approval, error) {
	var a Approval
	var rej sql.NullString
	err := s.q.QueryRowContext(ctx, "SELECT id,tenant_id,draft_id,draft_revision,draft_hash,operator_id,selected_action_ids,policy_version,approved_at,rejected_reason FROM approvals WHERE selected_action_ids LIKE ?", "%\""+actionID+"\"%").
		Scan(&a.ID, &a.TenantID, &a.DraftID, &a.DraftRevision, &a.DraftHash, &a.OperatorID, &a.SelectedActionIDs, &a.PolicyVersion, &a.ApprovedAt, &rej)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.RejectedReason = ps(rej)
	return &a, nil
}
func (s *SQLiteStore) InsertExecutionJob(j ExecutionJob) (*ExecutionJob, error) {
	if err := guardWrite(s.role, "execution_jobs_insert"); err != nil {
		return nil, err
	}
	existing, err := func() (*ExecutionJob, error) {
		var ej ExecutionJob
		err := s.q.QueryRowContext(ctx, "SELECT id FROM execution_jobs WHERE idempotency_key=?", j.IdempotencyKey).Scan(&ej.ID)
		if err != nil {
			return nil, nil
		}
		return s.GetJob(ej.ID)
	}()
	if err != nil || existing != nil {
		return existing, err
	}
	_, err = s.q.ExecContext(ctx, "INSERT INTO execution_jobs(id,tenant_id,proposed_action_id,idempotency_key,state,created_at) VALUES(?,?,?,?,'queued',?)",
		j.ID, j.TenantID, j.ProposedActionID, j.IdempotencyKey, NowISO())
	if err != nil {
		return nil, err
	}
	return s.GetJob(j.ID)
}

const jobCols = "id,tenant_id,proposed_action_id,idempotency_key,state,lease_owner,lease_expires_at,heartbeat_at,attempts,result_json,failure_code,created_at"

func scanJobRow(scan func(...any) error) (*ExecutionJob, error) {
	var j ExecutionJob
	var lease, leaseExp, hb, result, fail sql.NullString
	err := scan(&j.ID, &j.TenantID, &j.ProposedActionID, &j.IdempotencyKey, &j.State, &lease, &leaseExp, &hb, &j.Attempts, &result, &fail, &j.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j.LeaseOwner, j.LeaseExpiresAt, j.HeartbeatAt, j.ResultJSON, j.FailureCode = ps(lease), ps(leaseExp), ps(hb), ps(result), ps(fail)
	return &j, nil
}
func (s *SQLiteStore) GetJob(id string) (*ExecutionJob, error) {
	return scanJobRow(s.q.QueryRowContext(ctx, "SELECT "+jobCols+" FROM execution_jobs WHERE id=?", id).Scan)
}
func (s *SQLiteStore) GetJobByAction(actionID string) (*ExecutionJob, error) {
	return scanJobRow(s.q.QueryRowContext(ctx, "SELECT "+jobCols+" FROM execution_jobs WHERE proposed_action_id=?", actionID).Scan)
}
func (s *SQLiteStore) ClaimNextJob(workerID string, leaseMS int64) (*ExecutionJob, error) {
	var claimed *ExecutionJob
	err := s.Transaction(func(tx Repository) error {
		st := tx.(*SQLiteStore)
		j, err := scanJobRow(st.q.QueryRowContext(ctx, "SELECT "+jobCols+" FROM execution_jobs WHERE state='queued' OR (state='running' AND lease_expires_at IS NOT NULL AND lease_expires_at < ?) ORDER BY created_at LIMIT 1", NowISO()).Scan)
		if err != nil || j == nil {
			return err
		}
		if j.State == "running" {
			// Expired running job: the outcome is ambiguous — mark unknown,
			// never blindly re-run (SPEC §3.4).
			if _, err := st.q.ExecContext(ctx, "UPDATE execution_jobs SET state='unknown', failure_code='LEASE_EXPIRED_AMBIGUOUS' WHERE id=?", j.ID); err != nil {
				return err
			}
			got, err := st.GetJob(j.ID)
			claimed = got
			return err
		}
		expires := time.Now().Add(time.Duration(leaseMS) * time.Millisecond).UTC().Format("2006-01-02T15:04:05.000Z07:00")
		if _, err := st.q.ExecContext(ctx, "UPDATE execution_jobs SET state='running', lease_owner=?, lease_expires_at=?, heartbeat_at=?, attempts=attempts+1 WHERE id=?", workerID, expires, NowISO(), j.ID); err != nil {
			return err
		}
		got, err := st.GetJob(j.ID)
		claimed = got
		return err
	})
	return claimed, err
}
func (s *SQLiteStore) HeartbeatJob(id string, leaseMS int64) error {
	expires := time.Now().Add(time.Duration(leaseMS) * time.Millisecond).UTC().Format("2006-01-02T15:04:05.000Z07:00")
	_, err := s.q.ExecContext(ctx, "UPDATE execution_jobs SET heartbeat_at=?, lease_expires_at=? WHERE id=?", NowISO(), expires, id)
	return err
}
func (s *SQLiteStore) FinishJob(id, state string, resultJSON, failureCode *string) error {
	// Claiming/finishing is the executor's update path; in a `full`/`api`
	// test handle it is also allowed via the executor guard in guardWrite.
	_, err := s.q.ExecContext(ctx, "UPDATE execution_jobs SET state=?, result_json=?, failure_code=?, lease_owner=NULL, lease_expires_at=NULL WHERE id=?",
		state, ns(resultJSON), ns(failureCode), id)
	return err
}

// ---- fixture writes (capability-guarded) ----

func (s *SQLiteStore) InsertTicket(t Ticket) (*Ticket, error) {
	if err := guardWrite(s.role, "tickets"); err != nil {
		return nil, err
	}
	if t.Status == "" {
		t.Status = "open"
	}
	if t.Priority == "" {
		t.Priority = "normal"
	}
	_, err := s.q.ExecContext(ctx, "INSERT INTO tickets(id,tenant_id,inquiry_id,status,priority,source_action_id,created_at) VALUES(?,?,?,?,?,?,?)",
		t.ID, t.TenantID, t.InquiryID, t.Status, t.Priority, ns(t.SourceActionID), NowISO())
	if err != nil {
		return nil, err
	}
	var got Ticket
	var src sql.NullString
	err = s.q.QueryRowContext(ctx, "SELECT id,tenant_id,inquiry_id,status,priority,source_action_id,created_at FROM tickets WHERE id=?", t.ID).
		Scan(&got.ID, &got.TenantID, &got.InquiryID, &got.Status, &got.Priority, &src, &got.CreatedAt)
	got.SourceActionID = ps(src)
	return &got, err
}
func (s *SQLiteStore) FindTicketByAction(actionID string) (*Ticket, error) {
	var t Ticket
	var src sql.NullString
	err := s.q.QueryRowContext(ctx, "SELECT id,tenant_id,inquiry_id,status,priority,source_action_id,created_at FROM tickets WHERE source_action_id=?", actionID).
		Scan(&t.ID, &t.TenantID, &t.InquiryID, &t.Status, &t.Priority, &src, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.SourceActionID = ps(src)
	return &t, nil
}
func (s *SQLiteStore) ListTickets(tenantID string) ([]Ticket, error) {
	rows, err := s.q.QueryContext(ctx, "SELECT id,tenant_id,inquiry_id,status,priority,source_action_id,created_at FROM tickets WHERE tenant_id=? ORDER BY created_at", tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ticket{}
	for rows.Next() {
		var t Ticket
		var src sql.NullString
		if err := rows.Scan(&t.ID, &t.TenantID, &t.InquiryID, &t.Status, &t.Priority, &src, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.SourceActionID = ps(src)
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *SQLiteStore) InsertTicketNote(n TicketNote) (*TicketNote, error) {
	if err := guardWrite(s.role, "ticket_notes"); err != nil {
		return nil, err
	}
	_, err := s.q.ExecContext(ctx, "INSERT INTO ticket_notes(id,tenant_id,ticket_id,body,source_action_id,created_at) VALUES(?,?,?,?,?,?)",
		n.ID, n.TenantID, n.TicketID, n.Body, ns(n.SourceActionID), NowISO())
	if err != nil {
		return nil, err
	}
	return s.FindNoteByAction(*n.SourceActionID)
}
func (s *SQLiteStore) FindNoteByAction(actionID string) (*TicketNote, error) {
	var n TicketNote
	var src sql.NullString
	err := s.q.QueryRowContext(ctx, "SELECT id,tenant_id,ticket_id,body,source_action_id,created_at FROM ticket_notes WHERE source_action_id=?", actionID).
		Scan(&n.ID, &n.TenantID, &n.TicketID, &n.Body, &src, &n.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	n.SourceActionID = ps(src)
	return &n, nil
}
func (s *SQLiteStore) InsertOutbox(o OutboxMessage) (*OutboxMessage, error) {
	if err := guardWrite(s.role, "outbox_messages"); err != nil {
		return nil, err
	}
	if o.DeliveryState == "" {
		o.DeliveryState = "sent_demo"
	}
	_, err := s.q.ExecContext(ctx, "INSERT INTO outbox_messages(id,tenant_id,inquiry_id,draft_revision,body,delivery_state,source_action_id,created_at) VALUES(?,?,?,?,?,?,?,?)",
		o.ID, o.TenantID, o.InquiryID, o.DraftRevision, o.Body, o.DeliveryState, ns(o.SourceActionID), NowISO())
	if err != nil {
		return nil, err
	}
	if o.SourceActionID != nil {
		return s.FindOutboxByAction(*o.SourceActionID)
	}
	row := s.q.QueryRowContext(ctx, "SELECT id,tenant_id,inquiry_id,draft_revision,body,delivery_state,source_action_id,created_at FROM outbox_messages WHERE id=?", o.ID)
	var got OutboxMessage
	var srcID sql.NullString
	if err := row.Scan(&got.ID, &got.TenantID, &got.InquiryID, &got.DraftRevision, &got.Body, &got.DeliveryState, &srcID, &got.CreatedAt); err != nil {
		return nil, err
	}
	got.SourceActionID = ps(srcID)
	return &got, nil
}
func (s *SQLiteStore) FindOutboxByAction(actionID string) (*OutboxMessage, error) {
	var o OutboxMessage
	var src sql.NullString
	err := s.q.QueryRowContext(ctx, "SELECT id,tenant_id,inquiry_id,draft_revision,body,delivery_state,source_action_id,created_at FROM outbox_messages WHERE source_action_id=?", actionID).
		Scan(&o.ID, &o.TenantID, &o.InquiryID, &o.DraftRevision, &o.Body, &o.DeliveryState, &src, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	o.SourceActionID = ps(src)
	return &o, nil
}
func (s *SQLiteStore) ListOutbox(tenantID string) ([]OutboxMessage, error) {
	rows, err := s.q.QueryContext(ctx, "SELECT id,tenant_id,inquiry_id,draft_revision,body,delivery_state,source_action_id,created_at FROM outbox_messages WHERE tenant_id=? ORDER BY created_at", tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OutboxMessage{}
	for rows.Next() {
		var o OutboxMessage
		var src sql.NullString
		if err := rows.Scan(&o.ID, &o.TenantID, &o.InquiryID, &o.DraftRevision, &o.Body, &o.DeliveryState, &src, &o.CreatedAt); err != nil {
			return nil, err
		}
		o.SourceActionID = ps(src)
		out = append(out, o)
	}
	return out, rows.Err()
}
func (s *SQLiteStore) CountTickets() (int, error) {
	var n int
	err := s.q.QueryRowContext(ctx, "SELECT COUNT(*) FROM tickets").Scan(&n)
	return n, err
}
func (s *SQLiteStore) CountOutbox() (int, error) {
	var n int
	err := s.q.QueryRowContext(ctx, "SELECT COUNT(*) FROM outbox_messages").Scan(&n)
	return n, err
}

// ---- audit & eval ----

func (s *SQLiteStore) InsertAudit(a AuditEvent) error {
	_, err := s.q.ExecContext(ctx, "INSERT INTO audit_events(id,tenant_id,actor_type,actor_id,action,entity_type,entity_id,before_hash,after_hash,trace_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)",
		NewID("audit"), a.TenantID, a.ActorType, ns(a.ActorID), a.Action, a.EntityType, a.EntityID, ns(a.BeforeHash), ns(a.AfterHash), ns(a.TraceID), NowISO())
	return err
}
func (s *SQLiteStore) ListAudit(tenantID, entityType, entityID string) ([]AuditEvent, error) {
	var rows *sql.Rows
	var err error
	if entityType != "" && entityID != "" {
		rows, err = s.q.QueryContext(ctx, "SELECT id,tenant_id,actor_type,actor_id,action,entity_type,entity_id,before_hash,after_hash,trace_id,created_at FROM audit_events WHERE tenant_id=? AND entity_type=? AND entity_id=? ORDER BY created_at", tenantID, entityType, entityID)
	} else {
		rows, err = s.q.QueryContext(ctx, "SELECT id,tenant_id,actor_type,actor_id,action,entity_type,entity_id,before_hash,after_hash,trace_id,created_at FROM audit_events WHERE tenant_id=? ORDER BY created_at DESC LIMIT 200", tenantID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEvent{}
	for rows.Next() {
		var a AuditEvent
		var actor, before, after, trace sql.NullString
		if err := rows.Scan(&a.ID, &a.TenantID, &a.ActorType, &actor, &a.Action, &a.EntityType, &a.EntityID, &before, &after, &trace, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.ActorID, a.BeforeHash, a.AfterHash, a.TraceID = ps(actor), ps(before), ps(after), ps(trace)
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *SQLiteStore) InsertEvalRun(id string, meta EvalRunMeta, reportJSON string) error {
	_, err := s.q.ExecContext(ctx, "INSERT INTO eval_runs(id,dataset_version,prompt_version,model,index_version,report_json,created_at) VALUES(?,?,?,?,?,?,?)",
		id, ns(meta.DatasetVersion), ns(meta.PromptVersion), ns(meta.Model), ns(meta.IndexVersion), reportJSON, NowISO())
	return err
}
func scanEvalRow(scan func(...any) error, withReport bool) (*EvalRunRow, error) {
	var r EvalRunRow
	var ds, pv, model, iv, report sql.NullString
	var err error
	if withReport {
		err = scan(&r.ID, &ds, &pv, &model, &iv, &report, &r.CreatedAt)
	} else {
		err = scan(&r.ID, &ds, &pv, &model, &iv, &r.CreatedAt)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.DatasetVersion, r.PromptVersion, r.Model, r.IndexVersion = ps(ds), ps(pv), ps(model), ps(iv)
	if withReport {
		r.ReportJSON = ps(report)
	}
	return &r, nil
}
func (s *SQLiteStore) ListEvalRuns() ([]EvalRunRow, error) {
	rows, err := s.q.QueryContext(ctx, "SELECT id,dataset_version,prompt_version,model,index_version,created_at FROM eval_runs ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EvalRunRow{}
	for rows.Next() {
		r, err := scanEvalRow(rows.Scan, false)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}
func (s *SQLiteStore) GetEvalRun(id string) (*EvalRunRow, error) {
	return scanEvalRow(s.q.QueryRowContext(ctx, "SELECT id,dataset_version,prompt_version,model,index_version,report_json,created_at FROM eval_runs WHERE id=?", id).Scan, true)
}
