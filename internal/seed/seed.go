// Package seed loads the committed fixtures into a store (SPEC §6/§13):
// two tenants, demo users, CRM customers/orders, KB articles (tokenized with
// the configured Kagome dictionary), and inquiries redacted with the REAL
// redactor before persistence. Fixtures are synthetic demo data only.
package seed

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/aravinditte/benri-agent/internal/agent"
	"github.com/aravinditte/benri-agent/internal/auth"
	"github.com/aravinditte/benri-agent/internal/domain"
	"github.com/aravinditte/benri-agent/internal/japanese"
	"github.com/aravinditte/benri-agent/internal/retrieval"
	"github.com/aravinditte/benri-agent/internal/store"
)

// Seeded reports whether the demo users already exist.
func Seeded(st store.Repository) bool {
	u, err := st.FindUserByEmail("operator@hinata.example")
	return err == nil && u != nil
}

type inquiryFixture struct {
	InquiryID   string  `json:"inquiry_id"`
	Subject     string  `json:"subject"`
	Body        string  `json:"body"`
	OrderNoHint *string `json:"order_no_hint"`
	TenantID    string  `json:"tenant_id"`
}

type articleFixture struct {
	ArticleID  string   `json:"article_id"`
	TenantID   string   `json:"tenant_id"`
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	IntentTags []string `json:"intent_tags"`
	Status     string   `json:"status"`
	Version    int      `json:"version"`
}

type crmFixture struct {
	Customers []struct {
		CustomerID string `json:"customer_id"`
		TenantID   string `json:"tenant_id"`
		Name       string `json:"synthetic_name"`
		ContactRef string `json:"synthetic_contact_ref"`
	} `json:"customers"`
	Orders []struct {
		OrderNo        string `json:"order_no"`
		CustomerID     string `json:"customer_id"`
		TenantID       string `json:"tenant_id"`
		Item           string `json:"item"`
		OrderedAt      string `json:"ordered_at"`
		DeliveryStatus string `json:"delivery_status"`
	} `json:"orders"`
}

// Seed populates an empty store from fixturesDir. Idempotent: safe to call
// on an already-seeded store (inserts are OR IGNORE / REPLACE).
func Seed(st store.Repository, fixturesDir string, tk japanese.Tokenizer) error {
	_ = st.InsertTenant("tenant-hinata", "Hinata Store")
	_ = st.InsertTenant("tenant-sakura", "Sakura Crafts")
	users := []store.User{
		{ID: "user-operator", TenantID: "tenant-hinata", Email: "operator@hinata.example", PasswordHash: auth.HashPassword("demo-operator-pass"), Role: "operator", Status: "active"},
		{ID: "user-admin", TenantID: "tenant-hinata", Email: "admin@hinata.example", PasswordHash: auth.HashPassword("demo-admin-pass"), Role: "admin", Status: "active"},
	}
	for _, u := range users {
		if err := st.InsertUser(u); err != nil {
			return err
		}
	}
	var crm crmFixture
	if err := readJSON(filepath.Join(fixturesDir, "crm", "customers_orders.json"), &crm); err != nil {
		return err
	}
	for _, c := range crm.Customers {
		if err := st.InsertCustomer(store.Customer{ID: c.CustomerID, TenantID: c.TenantID, SyntheticName: c.Name, SyntheticContactRef: c.ContactRef}); err != nil {
			return err
		}
	}
	for _, o := range crm.Orders {
		if err := st.InsertOrder(store.Order{ID: "ord-" + o.OrderNo, TenantID: o.TenantID, CustomerID: o.CustomerID, OrderNo: o.OrderNo, Item: o.Item, OrderedAt: o.OrderedAt, DeliveryStatus: o.DeliveryStatus}); err != nil {
			return err
		}
	}
	var articles []articleFixture
	if err := readJSON(filepath.Join(fixturesDir, "kb", "articles.json"), &articles); err != nil {
		return err
	}
	idxVersion := retrieval.IndexVersion(tk.Name())
	for _, a := range articles {
		art := store.Article{ID: a.ArticleID, TenantID: a.TenantID, Title: a.Title, Body: a.Body, IntentTags: a.IntentTags, Status: a.Status, Version: a.Version}
		if err := st.InsertArticle(art, retrieval.TokenizedText(a.Body, tk), idxVersion); err != nil {
			return err
		}
	}
	var inquiries []inquiryFixture
	if err := readJSON(filepath.Join(fixturesDir, "inquiries", "inquiries.json"), &inquiries); err != nil {
		return err
	}
	for _, inq := range inquiries {
		tenant := inq.TenantID
		if tenant == "" {
			tenant = "tenant-hinata"
		}
		redacted := agent.RedactPII(inq.Body).Text
		if _, err := st.InsertInquiry(store.Inquiry{
			ID: inq.InquiryID, TenantID: tenant, Source: "seed", Subject: inq.Subject,
			RedactedBody: redacted, NormalizedBodyHash: auth.ContentHash(japanese.Normalize(redacted)),
			OrderNoHint: inq.OrderNoHint, Status: "new",
		}); err != nil {
			return err
		}
	}
	_ = domain.PromptVersion
	return nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
