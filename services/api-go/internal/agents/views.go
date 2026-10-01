package agents

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
)

// TotalsView is agent_runs' running totals (RunView.Totals and the live
// agent.run.finished projection).
type TotalsView struct {
	Steps             int32 `json:"steps"`
	LlmCalls          int32 `json:"llm_calls"`
	ResponseCacheHits int32 `json:"response_cache_hits"`
	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	CostMicroUsd      int64 `json:"cost_micro_usd"`
}

// UsageView is one step's model usage; StepView.Usage is nil for
// deterministic/tool/router steps that never called a model.
type UsageView struct {
	Model                    string `json:"model,omitempty"`
	PromptID                 string `json:"prompt_id,omitempty"`
	PromptVersion            int32  `json:"prompt_version,omitempty"`
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheReadInputTokens     int64  `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64  `json:"cache_creation_input_tokens"`
	CostMicroUsd             int64  `json:"cost_micro_usd"`
	ResponseCacheHit         bool   `json:"response_cache_hit"`
	LlmCalls                 int32  `json:"llm_calls"`
}

// RunView is the JSON shape of one agent_runs row. The REST handlers and the
// live tap both produce it (the live tap's projection is partial), so the
// frontend never sees two shapes for one run.
type RunView struct {
	ID              string     `json:"id"`
	Workflow        string     `json:"workflow"`
	SubjectType     string     `json:"subject_type"`
	SubjectID       string     `json:"subject_id"`
	ClientCompanyID string     `json:"client_company_id,omitempty"`
	Status          string     `json:"status"`
	ErrorCode       string     `json:"error_code,omitempty"`
	Plan            []PlanNode `json:"plan"`
	Budget          RunBudget  `json:"budget"`
	Totals          TotalsView `json:"totals"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

// StepView is the JSON shape of one agent_steps row.
type StepView struct {
	ID              string            `json:"id"`
	RunID           string            `json:"run_id"`
	NodeID          string            `json:"node_id"`
	Agent           string            `json:"agent"`
	Action          string            `json:"action"`
	Kind            string            `json:"kind"`
	Status          string            `json:"status"`
	MessageKey      string            `json:"message_key,omitempty"`
	ErrorCode       string            `json:"error_code,omitempty"`
	SubjectType     string            `json:"subject_type"`
	SubjectID       string            `json:"subject_id"`
	ClientCompanyID string            `json:"client_company_id,omitempty"`
	DependsOn       []string          `json:"depends_on"`
	Seq             int64             `json:"seq"`
	Attempt         int32             `json:"attempt"`
	At              time.Time         `json:"at"`
	DurationMs      int64             `json:"duration_ms"`
	Usage           *UsageView        `json:"usage,omitempty"`
	MessageArgs     map[string]string `json:"message_args,omitempty"`
}

func optionalIDString(n uuid.NullUUID) string {
	if !n.Valid {
		return ""
	}
	return n.UUID.String()
}

func tsPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// ViewOfRun maps one agent_runs row (Task 3's GetRun/ListRuns/RecentRuns) to its JSON view.
func ViewOfRun(r sqlc.AgentRun) RunView {
	plan := []PlanNode{}
	_ = json.Unmarshal(r.Plan, &plan)
	if plan == nil {
		plan = []PlanNode{}
	}
	var budget RunBudget
	_ = json.Unmarshal(r.Budget, &budget)
	return RunView{
		ID:              r.ID.String(),
		Workflow:        r.Workflow,
		SubjectType:     r.SubjectType,
		SubjectID:       r.SubjectID,
		ClientCompanyID: optionalIDString(r.ClientCompanyID),
		Status:          r.Status,
		ErrorCode:       r.ErrorCode,
		Plan:            plan,
		Budget:          budget,
		Totals: TotalsView{
			Steps: r.Steps, LlmCalls: r.LlmCalls, ResponseCacheHits: r.ResponseCacheHits,
			InputTokens: r.InputTokens, OutputTokens: r.OutputTokens, CostMicroUsd: r.CostMicroUsd,
		},
		StartedAt:  r.StartedAt.Time,
		FinishedAt: tsPtr(r.FinishedAt),
	}
}

// ViewOfStep maps one agent_steps row (Task 3's ListRunSteps/RecentSteps) to its JSON view.
func ViewOfStep(s sqlc.AgentStep) StepView {
	args := map[string]string{}
	_ = json.Unmarshal(s.MessageArgs, &args)
	deps := s.DependsOn
	if deps == nil {
		deps = []string{}
	}
	v := StepView{
		ID: s.ID.String(), RunID: s.RunID.String(), NodeID: s.NodeID, Agent: s.Agent, Action: s.Action,
		Kind: s.Kind, Status: s.Status, MessageKey: s.MessageKey, ErrorCode: s.ErrorCode,
		SubjectType: s.SubjectType, SubjectID: s.SubjectID, ClientCompanyID: optionalIDString(s.ClientCompanyID),
		DependsOn: deps, Seq: s.Seq, Attempt: s.Attempt, At: s.At.Time, DurationMs: s.DurationMs,
		MessageArgs: args,
	}
	if s.Model != "" || s.LlmCalls > 0 || s.InputTokens > 0 || s.OutputTokens > 0 {
		v.Usage = &UsageView{
			Model: s.Model, PromptID: s.PromptID, PromptVersion: s.PromptVersion,
			InputTokens: s.InputTokens, OutputTokens: s.OutputTokens,
			CacheReadInputTokens: s.CacheReadInputTokens, CacheCreationInputTokens: s.CacheCreationInputTokens,
			CostMicroUsd: s.CostMicroUsd, ResponseCacheHit: s.ResponseCacheHit, LlmCalls: s.LlmCalls,
		}
	}
	return v
}
