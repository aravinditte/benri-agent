package agent_test

import (
	"context"
	"testing"

	"github.com/aravinditte/benri-agent/internal/agent"
	"github.com/aravinditte/benri-agent/internal/domain"
	"github.com/aravinditte/benri-agent/internal/modelrail"
	"github.com/aravinditte/benri-agent/internal/observability"
	"github.com/aravinditte/benri-agent/internal/store"
	"github.com/aravinditte/benri-agent/internal/testutil"
)

// TestPipelineEmitsTraceTree: a run produces a benri.run root span with
// child step spans in the in-memory recorder (the same spans exported to
// Kairo via OTLP when configured). Export is async and its failure never
// fails a run — here the OTLP endpoint is deliberately unreachable.
func TestPipelineEmitsTraceTree(t *testing.T) {
	shutdown, err := observability.Setup(context.Background(), "benri-test", "http://127.0.0.1:1/unreachable")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = shutdown(context.Background()) }()
	observability.GlobalRecorder.Clear()
	_, full, agentH := setupPipeline(t)
	run, err := full.CreateRun(store.Run{ID: store.NewID("run"), TenantID: "tenant-hinata", InquiryID: "INQ-001"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = full.ClaimNextRun("test", 60000)
	tk := testutil.Tokenizer()
	llm := modelrail.New(modelrail.Config{}, agent.MockRespond(tk))
	limits := domain.Limits{MaxModelCallsPerRun: 4, MaxToolCallsPerRun: 8, MaxRunDurationMS: 30000, MaxOutputTokens: 1200}
	res, err := agent.RunPipeline(context.Background(), agentH, run.ID, agent.DirectToolGateway{Store: agentH, TK: tk}, llm, limits, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != "awaiting_review" {
		t.Fatalf("outcome = %s (unreachable Kairo must not fail the run)", res.Outcome)
	}
	spans := observability.GlobalRecorder.ForTrace(res.TraceID)
	if len(spans) == 0 {
		t.Fatal("no spans recorded for the run trace")
	}
	names := map[string]bool{}
	for _, s := range spans {
		names[s.Name] = true
	}
	for _, want := range []string{"benri.run", "classify", "retrieve", "verify"} {
		if !names[want] {
			t.Errorf("missing span %q in trace tree: %v", want, names)
		}
	}
}
