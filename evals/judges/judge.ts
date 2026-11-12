// LLM-as-judge interface (SPEC §11.4). CI uses the deterministic MOCK judge;
// a live judge is a separate, non-CI command. Judge scores are a quality
// signal, never a security gate.
export const RUBRIC_VERSION = "rubric-v1.0.0";
export interface JudgeScores {
  politeness: number; clarity: number; correctness: number; completeness: number; escalation_appropriateness: number;
}
export interface JudgeInput {
  draft_body: string | null;
  verification_passed: boolean;
  cited_count: number;
  escalated: boolean;
  expected_escalation: boolean;
}
export interface Judge { name: string; rubric_version: string; score(input: JudgeInput): JudgeScores }

export const mockJudge: Judge = {
  name: "mock-deterministic-judge",
  rubric_version: RUBRIC_VERSION,
  score(input) {
    if (input.escalated || !input.draft_body) {
      return { politeness: 0, clarity: 0, correctness: 0, completeness: 0, escalation_appropriateness: input.escalated === input.expected_escalation ? 5 : 0 };
    }
    const body = input.draft_body;
    return {
      politeness: /です|ます|ございます/.test(body) && body.includes("ひなたストア カスタマーサポート") ? 5 : 2,
      clarity: body.length >= 80 && body.length <= 1200 ? 5 : 3,
      correctness: input.verification_passed ? 5 : 1,
      completeness: input.cited_count >= 1 ? 4 : 1,
      escalation_appropriateness: input.escalated === input.expected_escalation ? 5 : 0,
    };
  },
};
