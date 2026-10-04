package model

import "time"

const (
	AIFeatureTaskSummary            = "task_summary"
	AIFeatureDescriptionImprovement = "description_improvement"
	AIFeatureSubtasks               = "subtasks"
	AIFeaturePrioritySuggestion     = "priority_suggestion"
	AIFeatureDuplicateSuggestion    = "duplicate_suggestion"
	AIFeatureSemanticSearch         = "semantic_search"
	AIFeatureProjectSummary         = "project_summary"
	AIFeatureIncidentSummary        = "incident_summary"
	AIFeatureRiskWorkflowSuggestion = "risk_workflow_suggestion"
	AIFeatureNaturalLanguageReport  = "natural_language_report"

	AIRequestCompleted       = "completed"
	AIRequestPendingApproval = "pending_approval"
	AIRequestApproved        = "approved"
	AIRequestRejected        = "rejected"
	AIRequestBlocked         = "blocked"
	AIRequestFailed          = "failed"

	AIProviderLocalRules = "local_rules"

	AIApprovalApprove = "approve"
	AIApprovalReject  = "reject"
)

type AIPolicy struct {
	OrganizationID                 int64     `json:"organization_id"`
	Enabled                        bool      `json:"enabled"`
	Provider                       string    `json:"provider"`
	MonthlyBudgetCents             int64     `json:"monthly_budget_cents"`
	RedactionEnabled               bool      `json:"redaction_enabled"`
	MaxInputChars                  int       `json:"max_input_chars"`
	AllowedClassifications         []string  `json:"allowed_classifications"`
	ExternalMaxClassification      string    `json:"external_max_classification"`
	RequireHumanApprovalForActions bool      `json:"require_human_approval_for_actions"`
	UpdatedByUserID                int64     `json:"updated_by_user_id"`
	CreatedAt                      time.Time `json:"created_at"`
	UpdatedAt                      time.Time `json:"updated_at"`
}

type AIProviderCapability struct {
	Key                      string   `json:"key"`
	DisplayName              string   `json:"display_name"`
	Configured               bool     `json:"configured"`
	External                 bool     `json:"external"`
	StructuredOutput         bool     `json:"structured_output"`
	SupportedClassifications []string `json:"supported_classifications"`
}

type AIRequest struct {
	ID                 int64          `json:"id"`
	OrganizationID     int64          `json:"organization_id"`
	WorkspaceID        *int64         `json:"workspace_id,omitempty"`
	Feature            string         `json:"feature"`
	Status             string         `json:"status"`
	Provider           string         `json:"provider"`
	Model              string         `json:"model"`
	Classification     string         `json:"classification"`
	InputHash          string         `json:"input_hash"`
	RedactionCount     int            `json:"redaction_count"`
	PromptMetadata     map[string]any `json:"prompt_metadata"`
	StructuredResult   map[string]any `json:"structured_result"`
	ProposedAction     map[string]any `json:"proposed_action,omitempty"`
	RequiresApproval   bool           `json:"requires_approval"`
	DestructiveAction  bool           `json:"destructive_action"`
	InputUnits         int64          `json:"input_units"`
	OutputUnits        int64          `json:"output_units"`
	EstimatedCostCents int64          `json:"estimated_cost_cents"`
	ActualCostCents    int64          `json:"actual_cost_cents"`
	CreatedByUserID    int64          `json:"created_by_user_id"`
	CreatedAt          time.Time      `json:"created_at"`
	DecidedByUserID    *int64         `json:"decided_by_user_id,omitempty"`
	DecisionComment    string         `json:"decision_comment,omitempty"`
	DecidedAt          *time.Time     `json:"decided_at,omitempty"`
}

type AIUsageSummary struct {
	OrganizationID    int64  `json:"organization_id"`
	Month             string `json:"month"`
	BudgetCents       int64  `json:"budget_cents"`
	SpentCents        int64  `json:"spent_cents"`
	RemainingCents    int64  `json:"remaining_cents"`
	RequestCount      int64  `json:"request_count"`
	InputUnits        int64  `json:"input_units"`
	OutputUnits       int64  `json:"output_units"`
	BudgetUtilization int    `json:"budget_utilization_percent"`
}

type AIEvaluationCase struct {
	ID               int64          `json:"id"`
	OrganizationID   int64          `json:"organization_id"`
	Name             string         `json:"name"`
	Feature          string         `json:"feature"`
	Input            string         `json:"input"`
	Classification   string         `json:"classification"`
	ExpectedKeywords []string       `json:"expected_keywords"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	CreatedByUserID  int64          `json:"created_by_user_id"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type AIEvaluationRun struct {
	ID               int64          `json:"id"`
	OrganizationID   int64          `json:"organization_id"`
	CaseID           int64          `json:"case_id"`
	Provider         string         `json:"provider"`
	Model            string         `json:"model"`
	ScoreBasisPoints int            `json:"score_basis_points"`
	InputUnits       int64          `json:"input_units"`
	OutputUnits      int64          `json:"output_units"`
	ActualCostCents  int64          `json:"actual_cost_cents"`
	Passed           bool           `json:"passed"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
}

type AIQualitySummary struct {
	OrganizationID int64             `json:"organization_id"`
	TotalRuns      int64             `json:"total_runs"`
	PassedRuns     int64             `json:"passed_runs"`
	PassRate       float64           `json:"pass_rate"`
	AverageScore   float64           `json:"average_score_basis_points"`
	RecentRuns     []AIEvaluationRun `json:"recent_runs"`
}

type UpdateAIPolicyRequest struct {
	Enabled                        bool     `json:"enabled"`
	Provider                       string   `json:"provider"`
	MonthlyBudgetCents             int64    `json:"monthly_budget_cents"`
	RedactionEnabled               bool     `json:"redaction_enabled"`
	MaxInputChars                  int      `json:"max_input_chars"`
	AllowedClassifications         []string `json:"allowed_classifications"`
	ExternalMaxClassification      string   `json:"external_max_classification"`
	RequireHumanApprovalForActions bool     `json:"require_human_approval_for_actions"`
}

type AIAssistRequest struct {
	Feature        string         `json:"feature"`
	WorkspaceID    *int64         `json:"workspace_id,omitempty"`
	TaskID         *int64         `json:"task_id,omitempty"`
	ProjectID      *int64         `json:"project_id,omitempty"`
	IncidentID     *int64         `json:"incident_id,omitempty"`
	Input          string         `json:"input,omitempty"`
	Classification string         `json:"classification,omitempty"`
	ProposedAction map[string]any `json:"proposed_action,omitempty"`
}

type AIDecisionRequest struct {
	Decision string `json:"decision"`
	Comment  string `json:"comment,omitempty"`
}

type AISemanticSearchRequest struct {
	WorkspaceID    int64  `json:"workspace_id"`
	Query          string `json:"query"`
	Classification string `json:"classification,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

type AISemanticSearchHit struct {
	Task  Task    `json:"task"`
	Score float64 `json:"score"`
	Why   string  `json:"why"`
}

type CreateAIEvaluationCaseRequest struct {
	Name             string         `json:"name"`
	Feature          string         `json:"feature"`
	Input            string         `json:"input"`
	Classification   string         `json:"classification,omitempty"`
	ExpectedKeywords []string       `json:"expected_keywords"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}
