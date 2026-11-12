package api_test

import (
	"context"
	"testing"

	"github.com/aravinditte/benri-agent/internal/agent"
	"github.com/aravinditte/benri-agent/internal/domain"
	"github.com/aravinditte/benri-agent/internal/modelrail"
	"github.com/aravinditte/benri-agent/internal/store"
	"github.com/aravinditte/benri-agent/internal/testutil"
)

// runPipelineForTest executes the agent pipeline for a queued run, the same
// work the agent-worker binary performs.
func runPipelineForTest(t *testing.T, agentH store.Repository, runID string) {
	t.Helper()
	if _, err := agentH.ClaimNextRun("test", 60000); err != nil {
		t.Fatal(err)
	}
	tk := testutil.Tokenizer()
	llm := modelrail.New(modelrail.Config{}, agent.MockRespond(tk))
	limits := domain.Limits{MaxModelCallsPerRun: 4, MaxToolCallsPerRun: 8, MaxRunDurationMS: 30000, MaxOutputTokens: 1200}
	res, err := agent.RunPipeline(context.Background(), agentH, runID, agent.DirectToolGateway{Store: agentH, TK: tk}, llm, limits, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != "awaiting_review" {
		t.Fatalf("pipeline outcome = %s", res.Outcome)
	}
}
