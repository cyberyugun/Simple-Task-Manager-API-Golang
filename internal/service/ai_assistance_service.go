package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrAIAssistanceForbidden      = errors.New("AI assistance action is forbidden")
	ErrAIAssistanceDisabled       = errors.New("AI assistance is disabled by organization policy")
	ErrInvalidAIPolicy            = errors.New("invalid AI assistance policy")
	ErrInvalidAIAssistanceRequest = errors.New("invalid AI assistance request")
	ErrAIProviderUnavailable      = errors.New("AI provider is not configured")
	ErrAIClassificationBlocked    = errors.New("AI provider routing blocked by data classification policy")
	ErrAIBudgetExceeded           = errors.New("AI monthly cost budget exceeded")
	ErrAIApprovalRequired         = errors.New("AI proposed action requires human approval")
	ErrAIActionNotAllowed         = errors.New("AI proposed action is not allowed")
	ErrInvalidAIEvaluation        = errors.New("invalid AI evaluation case")
)

type AIProviderRequest struct {
	Feature        string
	Text           string
	Classification string
	Context        map[string]any
}

type AIProviderResponse struct {
	Model           string
	Result          map[string]any
	InputUnits      int64
	OutputUnits     int64
	ActualCostCents int64
}

type AIProvider interface {
	Key() string
	DisplayName() string
	External() bool
	SupportedClassifications() []string
	EstimateCostCents(AIProviderRequest) int64
	Generate(context.Context, AIProviderRequest) (AIProviderResponse, error)
}

type AIProviderRegistry struct {
	providers map[string]AIProvider
}

func NewAIProviderRegistry() *AIProviderRegistry {
	registry := &AIProviderRegistry{providers: make(map[string]AIProvider)}
	registry.Register(localRulesAIProvider{})
	return registry
}

func (r *AIProviderRegistry) Register(provider AIProvider) {
	if provider == nil || strings.TrimSpace(provider.Key()) == "" {
		return
	}
	r.providers[provider.Key()] = provider
}

func (r *AIProviderRegistry) Get(key string) (AIProvider, bool) {
	provider, ok := r.providers[key]
	return provider, ok
}

func (r *AIProviderRegistry) Capabilities() []model.AIProviderCapability {
	keys := make([]string, 0, len(r.providers))
	for key := range r.providers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]model.AIProviderCapability, 0, len(keys))
	for _, key := range keys {
		provider := r.providers[key]
		items = append(items, model.AIProviderCapability{
			Key: key, DisplayName: provider.DisplayName(), Configured: true, External: provider.External(),
			StructuredOutput: true, SupportedClassifications: append([]string(nil), provider.SupportedClassifications()...),
		})
	}
	return items
}

type localRulesAIProvider struct{}

func (localRulesAIProvider) Key() string         { return model.AIProviderLocalRules }
func (localRulesAIProvider) DisplayName() string { return "Local Rules Intelligence" }
func (localRulesAIProvider) External() bool      { return false }
func (localRulesAIProvider) SupportedClassifications() []string {
	return []string{
		model.DataClassificationPublic,
		model.DataClassificationInternal,
		model.DataClassificationConfidential,
		model.DataClassificationRestricted,
	}
}
func (localRulesAIProvider) EstimateCostCents(req AIProviderRequest) int64 {
	units := int64((len([]rune(req.Text)) + 999) / 1000)
	if units < 1 {
		units = 1
	}
	return units
}
func (localRulesAIProvider) Generate(_ context.Context, req AIProviderRequest) (AIProviderResponse, error) {
	text := strings.TrimSpace(req.Text)
	result := map[string]any{}
	switch req.Feature {
	case model.AIFeatureTaskSummary:
		result["summary"] = summarizeText(text, 240)
	case model.AIFeatureDescriptionImprovement:
		result["description"] = improveDescription(text)
	case model.AIFeatureSubtasks:
		result["subtasks"] = suggestSubtasks(text)
	case model.AIFeaturePrioritySuggestion:
		priority, reasons := suggestPriority(text, req.Context)
		result["priority"] = priority
		result["reasons"] = reasons
	case model.AIFeatureDuplicateSuggestion:
		result["candidates"] = req.Context["duplicate_candidates"]
	case model.AIFeatureProjectSummary:
		result["summary"] = summarizeProject(req.Context)
	case model.AIFeatureIncidentSummary:
		result["summary"] = summarizeIncident(req.Context)
	case model.AIFeatureRiskWorkflowSuggestion:
		result["risks"] = req.Context["risks"]
		result["workflow_suggestions"] = req.Context["workflow_suggestions"]
	case model.AIFeatureNaturalLanguageReport:
		result["report"] = naturalLanguageReport(req.Context, text)
	default:
		return AIProviderResponse{}, ErrInvalidAIAssistanceRequest
	}
	raw, _ := json.Marshal(result)
	inputUnits := int64((len([]rune(text)) + 3) / 4)
	outputUnits := int64((len(raw) + 3) / 4)
	cost := int64((len([]rune(text)) + 999) / 1000)
	if cost < 1 {
		cost = 1
	}
	return AIProviderResponse{
		Model: "local-rules-v1", Result: result, InputUnits: inputUnits,
		OutputUnits: outputUnits, ActualCostCents: cost,
	}, nil
}

type AIAssistanceService struct {
	repo       repository.AIAssistanceRepository
	orgs       repository.OrganizationRepository
	operations repository.OperationsRepository
	tasks      repository.TaskRepository
	taskSvc    *TaskService
	providers  *AIProviderRegistry
}

func NewAIAssistanceService(
	repo repository.AIAssistanceRepository,
	orgs repository.OrganizationRepository,
	operations repository.OperationsRepository,
	tasks repository.TaskRepository,
	taskSvc *TaskService,
	providers *AIProviderRegistry,
) *AIAssistanceService {
	if providers == nil {
		providers = NewAIProviderRegistry()
	}
	return &AIAssistanceService{
		repo: repo, orgs: orgs, operations: operations, tasks: tasks, taskSvc: taskSvc, providers: providers,
	}
}

func (s *AIAssistanceService) Providers(actorUserID, organizationID int64) ([]model.AIProviderCapability, error) {
	if _, err := s.requireMember(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.providers.Capabilities(), nil
}

func (s *AIAssistanceService) Policy(actorUserID, organizationID int64) (model.AIPolicy, error) {
	if _, err := s.requireMember(actorUserID, organizationID); err != nil {
		return model.AIPolicy{}, err
	}
	return s.policyOrDefault(organizationID)
}

func (s *AIAssistanceService) UpdatePolicy(actorUserID, organizationID int64, req model.UpdateAIPolicyRequest) (model.AIPolicy, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.AIPolicy{}, err
	}
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		provider = model.AIProviderLocalRules
	}
	if _, ok := s.providers.Get(provider); !ok {
		return model.AIPolicy{}, ErrAIProviderUnavailable
	}
	allowed := normalizeAIClassifications(req.AllowedClassifications)
	if len(allowed) == 0 || req.MonthlyBudgetCents < 0 || req.MonthlyBudgetCents > 1000000000 {
		return model.AIPolicy{}, ErrInvalidAIPolicy
	}
	maxInput := req.MaxInputChars
	if maxInput == 0 {
		maxInput = 12000
	}
	if maxInput < 256 || maxInput > 200000 {
		return model.AIPolicy{}, ErrInvalidAIPolicy
	}
	externalMax := normalizeAIClassification(req.ExternalMaxClassification)
	if externalMax == "" {
		externalMax = model.DataClassificationInternal
	}
	now := time.Now().UTC()
	current, err := s.repo.GetPolicy(organizationID)
	if err != nil && !errors.Is(err, repository.ErrAIPolicyNotFound) {
		return model.AIPolicy{}, err
	}
	createdAt := now
	if err == nil {
		createdAt = current.CreatedAt
	}
	item, err := s.repo.UpsertPolicy(model.AIPolicy{
		OrganizationID: organizationID, Enabled: req.Enabled, Provider: provider,
		MonthlyBudgetCents: req.MonthlyBudgetCents, RedactionEnabled: req.RedactionEnabled,
		MaxInputChars: maxInput, AllowedClassifications: allowed, ExternalMaxClassification: externalMax,
		RequireHumanApprovalForActions: req.RequireHumanApprovalForActions,
		UpdatedByUserID:                actorUserID, CreatedAt: createdAt, UpdatedAt: now,
	})
	if err == nil {
		_ = s.audit(organizationID, &actorUserID, "ai.policy.updated", "ai_policy", fmt.Sprint(organizationID), map[string]any{
			"enabled": item.Enabled, "provider": item.Provider, "monthly_budget_cents": item.MonthlyBudgetCents,
			"allowed_classifications": item.AllowedClassifications, "redaction_enabled": item.RedactionEnabled,
			"requires_human_approval": item.RequireHumanApprovalForActions,
		}, now)
	}
	return item, err
}

func (s *AIAssistanceService) Requests(actorUserID, organizationID int64) ([]model.AIRequest, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListRequests(organizationID, 200)
}

func (s *AIAssistanceService) Assist(ctx context.Context, actorUserID, organizationID int64, req model.AIAssistRequest) (model.AIRequest, error) {
	if _, err := s.requireMember(actorUserID, organizationID); err != nil {
		return model.AIRequest{}, err
	}
	policy, err := s.policyOrDefault(organizationID)
	if err != nil {
		return model.AIRequest{}, err
	}
	if !policy.Enabled {
		return model.AIRequest{}, ErrAIAssistanceDisabled
	}
	feature := strings.ToLower(strings.TrimSpace(req.Feature))
	if !validAIFeature(feature) || feature == model.AIFeatureSemanticSearch {
		return model.AIRequest{}, ErrInvalidAIAssistanceRequest
	}
	classification := normalizeAIClassification(req.Classification)
	if classification == "" {
		classification = model.DataClassificationInternal
	}
	if !containsAIString(policy.AllowedClassifications, classification) {
		return model.AIRequest{}, ErrAIClassificationBlocked
	}
	provider, ok := s.providers.Get(policy.Provider)
	if !ok {
		return model.AIRequest{}, ErrAIProviderUnavailable
	}
	if !providerSupportsClassification(provider, classification) ||
		(provider.External() && aiClassificationRank(classification) > aiClassificationRank(policy.ExternalMaxClassification)) {
		return model.AIRequest{}, ErrAIClassificationBlocked
	}
	if req.WorkspaceID != nil {
		if *req.WorkspaceID <= 0 || !s.workspaceBelongsToOrganization(organizationID, *req.WorkspaceID) {
			return model.AIRequest{}, ErrAIAssistanceForbidden
		}
	}
	text, contextData, proposedAction, destructive, err := s.prepareRequestContext(organizationID, feature, req)
	if err != nil {
		return model.AIRequest{}, err
	}
	if len([]rune(text)) > policy.MaxInputChars {
		return model.AIRequest{}, ErrInvalidAIAssistanceRequest
	}
	redacted := text
	redactionCount := 0
	if policy.RedactionEnabled {
		redacted, redactionCount = redactAIText(text)
	}
	providerRequest := AIProviderRequest{
		Feature: feature, Text: redacted, Classification: classification, Context: contextData,
	}
	estimated := provider.EstimateCostCents(providerRequest)
	if err := s.checkBudget(organizationID, policy, estimated, time.Now().UTC()); err != nil {
		return model.AIRequest{}, err
	}
	output, err := provider.Generate(ctx, providerRequest)
	if err != nil {
		return model.AIRequest{}, err
	}
	if output.Result == nil {
		return model.AIRequest{}, ErrInvalidAIAssistanceRequest
	}
	if len(proposedAction) == 0 {
		proposedAction = actionFromAIResult(feature, req, output.Result)
		destructive = isDestructiveAIAction(proposedAction)
	}
	requiresApproval := len(proposedAction) > 0
	if requiresApproval && !policy.RequireHumanApprovalForActions {
		// AI-originated mutations still stay human-gated. The policy flag can only strengthen,
		// never remove, the platform safety invariant.
		requiresApproval = true
	}
	status := model.AIRequestCompleted
	if requiresApproval {
		status = model.AIRequestPendingApproval
	}
	now := time.Now().UTC()
	hash := sha256.Sum256([]byte(text))
	workspaceID := req.WorkspaceID
	item, err := s.repo.CreateRequest(model.AIRequest{
		OrganizationID: organizationID, WorkspaceID: workspaceID, Feature: feature, Status: status,
		Provider: provider.Key(), Model: output.Model, Classification: classification,
		InputHash: hex.EncodeToString(hash[:]), RedactionCount: redactionCount,
		PromptMetadata: map[string]any{
			"input_chars": len([]rune(text)), "redacted_chars": len([]rune(redacted)),
			"context_keys": sortedAIMapKeys(contextData), "provider_external": provider.External(),
		},
		StructuredResult: output.Result, ProposedAction: proposedAction, RequiresApproval: requiresApproval,
		DestructiveAction: destructive, InputUnits: output.InputUnits, OutputUnits: output.OutputUnits,
		EstimatedCostCents: estimated, ActualCostCents: output.ActualCostCents,
		CreatedByUserID: actorUserID, CreatedAt: now,
	})
	if err == nil {
		_ = s.audit(organizationID, &actorUserID, "ai.request.completed", "ai_request", fmt.Sprint(item.ID), map[string]any{
			"feature": feature, "provider": provider.Key(), "model": output.Model,
			"classification": classification, "redaction_count": redactionCount,
			"requires_approval": requiresApproval, "destructive_action": destructive,
			"actual_cost_cents": output.ActualCostCents,
		}, now)
	}
	return item, err
}

func (s *AIAssistanceService) Decide(actorUserID, organizationID, requestID int64, req model.AIDecisionRequest) (model.AIRequest, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.AIRequest{}, err
	}
	item, err := s.repo.GetRequest(organizationID, requestID)
	if err != nil {
		return model.AIRequest{}, err
	}
	if item.Status != model.AIRequestPendingApproval || !item.RequiresApproval {
		return model.AIRequest{}, ErrAIApprovalRequired
	}
	decision := strings.ToLower(strings.TrimSpace(req.Decision))
	now := time.Now().UTC()
	actor := actorUserID
	item.DecidedByUserID = &actor
	item.DecisionComment = strings.TrimSpace(req.Comment)
	item.DecidedAt = &now
	switch decision {
	case model.AIApprovalReject:
		item.Status = model.AIRequestRejected
	case model.AIApprovalApprove:
		if err := s.executeApprovedAction(actorUserID, item); err != nil {
			return model.AIRequest{}, err
		}
		item.Status = model.AIRequestApproved
	default:
		return model.AIRequest{}, ErrInvalidAIAssistanceRequest
	}
	updated, err := s.repo.UpdateRequest(item)
	if err == nil {
		_ = s.audit(organizationID, &actorUserID, "ai.request.decided", "ai_request", fmt.Sprint(requestID), map[string]any{
			"decision": decision, "destructive_action": item.DestructiveAction,
		}, now)
	}
	return updated, err
}

func (s *AIAssistanceService) Usage(actorUserID, organizationID int64, at time.Time) (model.AIUsageSummary, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.AIUsageSummary{}, err
	}
	policy, err := s.policyOrDefault(organizationID)
	if err != nil {
		return model.AIUsageSummary{}, err
	}
	from := time.Date(at.UTC().Year(), at.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	summary, err := s.repo.MonthlyUsage(organizationID, from, to)
	if err != nil {
		return model.AIUsageSummary{}, err
	}
	summary.BudgetCents = policy.MonthlyBudgetCents
	summary.RemainingCents = policy.MonthlyBudgetCents - summary.SpentCents
	if summary.RemainingCents < 0 {
		summary.RemainingCents = 0
	}
	if policy.MonthlyBudgetCents > 0 {
		summary.BudgetUtilization = int(math.Round(float64(summary.SpentCents) * 100 / float64(policy.MonthlyBudgetCents)))
	}
	return summary, nil
}

func (s *AIAssistanceService) SemanticSearch(actorUserID, organizationID int64, req model.AISemanticSearchRequest) ([]model.AISemanticSearchHit, error) {
	if _, err := s.requireMember(actorUserID, organizationID); err != nil {
		return nil, err
	}
	policy, err := s.policyOrDefault(organizationID)
	if err != nil {
		return nil, err
	}
	if !policy.Enabled {
		return nil, ErrAIAssistanceDisabled
	}
	if req.WorkspaceID <= 0 || !s.workspaceBelongsToOrganization(organizationID, req.WorkspaceID) {
		return nil, ErrAIAssistanceForbidden
	}
	classification := normalizeAIClassification(req.Classification)
	if classification == "" {
		classification = model.DataClassificationInternal
	}
	if !containsAIString(policy.AllowedClassifications, classification) {
		return nil, ErrAIClassificationBlocked
	}
	query := strings.TrimSpace(req.Query)
	if query == "" || len([]rune(query)) > policy.MaxInputChars {
		return nil, ErrInvalidAIAssistanceRequest
	}
	limit := req.Limit
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 50 {
		return nil, ErrInvalidAIAssistanceRequest
	}
	page, err := s.tasks.FindAll(req.WorkspaceID, model.TaskQuery{Page: 1, Limit: 100, Sort: "updated_at", Order: "desc"})
	if err != nil {
		return nil, err
	}
	queryTokens := semanticAITokens(query)
	items := make([]model.AISemanticSearchHit, 0)
	for _, task := range page.Items {
		score, why := semanticAIScore(queryTokens, semanticAITokens(task.Title+" "+task.Description))
		if score <= 0 {
			continue
		}
		items = append(items, model.AISemanticSearchHit{Task: task, Score: score, Why: why})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			return items[i].Task.UpdatedAt.After(items[j].Task.UpdatedAt)
		}
		return items[i].Score > items[j].Score
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *AIAssistanceService) EvaluationCases(actorUserID, organizationID int64) ([]model.AIEvaluationCase, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListEvaluationCases(organizationID)
}

func (s *AIAssistanceService) CreateEvaluationCase(actorUserID, organizationID int64, req model.CreateAIEvaluationCaseRequest) (model.AIEvaluationCase, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.AIEvaluationCase{}, err
	}
	name := strings.TrimSpace(req.Name)
	feature := strings.ToLower(strings.TrimSpace(req.Feature))
	input := strings.TrimSpace(req.Input)
	classification := normalizeAIClassification(req.Classification)
	if classification == "" {
		classification = model.DataClassificationInternal
	}
	keywords := normalizeAIKeywords(req.ExpectedKeywords)
	if name == "" || !validAIFeature(feature) || feature == model.AIFeatureSemanticSearch || input == "" || len(keywords) == 0 {
		return model.AIEvaluationCase{}, ErrInvalidAIEvaluation
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateEvaluationCase(model.AIEvaluationCase{
		OrganizationID: organizationID, Name: name, Feature: feature, Input: input,
		Classification: classification, ExpectedKeywords: keywords, Metadata: cloneServiceMap(req.Metadata),
		CreatedByUserID: actorUserID, CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		_ = s.audit(organizationID, &actorUserID, "ai.evaluation_case.created", "ai_evaluation_case", fmt.Sprint(item.ID),
			map[string]any{"feature": feature}, now)
	}
	return item, err
}

func (s *AIAssistanceService) RunEvaluation(ctx context.Context, actorUserID, organizationID, caseID int64) (model.AIEvaluationRun, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.AIEvaluationRun{}, err
	}
	policy, err := s.policyOrDefault(organizationID)
	if err != nil {
		return model.AIEvaluationRun{}, err
	}
	if !policy.Enabled {
		return model.AIEvaluationRun{}, ErrAIAssistanceDisabled
	}
	testCase, err := s.repo.GetEvaluationCase(organizationID, caseID)
	if err != nil {
		return model.AIEvaluationRun{}, err
	}
	provider, ok := s.providers.Get(policy.Provider)
	if !ok {
		return model.AIEvaluationRun{}, ErrAIProviderUnavailable
	}
	if !containsAIString(policy.AllowedClassifications, testCase.Classification) ||
		!providerSupportsClassification(provider, testCase.Classification) ||
		(provider.External() && aiClassificationRank(testCase.Classification) > aiClassificationRank(policy.ExternalMaxClassification)) {
		return model.AIEvaluationRun{}, ErrAIClassificationBlocked
	}
	text := testCase.Input
	redactionCount := 0
	if policy.RedactionEnabled {
		text, redactionCount = redactAIText(text)
	}
	providerRequest := AIProviderRequest{Feature: testCase.Feature, Text: text, Classification: testCase.Classification, Context: map[string]any{}}
	if err := s.checkBudget(organizationID, policy, provider.EstimateCostCents(providerRequest), time.Now().UTC()); err != nil {
		return model.AIEvaluationRun{}, err
	}
	result, err := provider.Generate(ctx, providerRequest)
	if err != nil {
		return model.AIEvaluationRun{}, err
	}
	raw, _ := json.Marshal(result.Result)
	lower := strings.ToLower(string(raw))
	matched := 0
	for _, keyword := range testCase.ExpectedKeywords {
		if strings.Contains(lower, strings.ToLower(keyword)) {
			matched++
		}
	}
	score := matched * 10000 / len(testCase.ExpectedKeywords)
	now := time.Now().UTC()
	run, err := s.repo.CreateEvaluationRun(model.AIEvaluationRun{
		OrganizationID: organizationID, CaseID: caseID, Provider: provider.Key(), Model: result.Model,
		ScoreBasisPoints: score, InputUnits: result.InputUnits, OutputUnits: result.OutputUnits,
		ActualCostCents: result.ActualCostCents, Passed: score >= 7000,
		Metadata:  map[string]any{"expected_keywords": len(testCase.ExpectedKeywords), "matched_keywords": matched, "redaction_count": redactionCount},
		CreatedAt: now,
	})
	if err == nil {
		_ = s.audit(organizationID, &actorUserID, "ai.evaluation.completed", "ai_evaluation_run", fmt.Sprint(run.ID),
			map[string]any{"case_id": caseID, "score_basis_points": score, "passed": run.Passed}, now)
	}
	return run, err
}

func (s *AIAssistanceService) Quality(actorUserID, organizationID int64) (model.AIQualitySummary, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.AIQualitySummary{}, err
	}
	runs, err := s.repo.ListEvaluationRuns(organizationID, 100)
	if err != nil {
		return model.AIQualitySummary{}, err
	}
	summary := model.AIQualitySummary{OrganizationID: organizationID, RecentRuns: runs}
	var score int64
	for _, run := range runs {
		summary.TotalRuns++
		score += int64(run.ScoreBasisPoints)
		if run.Passed {
			summary.PassedRuns++
		}
	}
	if summary.TotalRuns > 0 {
		summary.PassRate = float64(summary.PassedRuns) / float64(summary.TotalRuns)
		summary.AverageScore = float64(score) / float64(summary.TotalRuns)
	}
	return summary, nil
}

func (s *AIAssistanceService) prepareRequestContext(organizationID int64, feature string, req model.AIAssistRequest) (string, map[string]any, map[string]any, bool, error) {
	contextData := map[string]any{}
	text := strings.TrimSpace(req.Input)
	proposedAction := cloneServiceMap(req.ProposedAction)
	destructive := isDestructiveAIAction(proposedAction)

	if req.TaskID != nil {
		if req.WorkspaceID == nil || *req.TaskID <= 0 {
			return "", nil, nil, false, ErrInvalidAIAssistanceRequest
		}
		task, err := s.tasks.FindByID(*req.WorkspaceID, *req.TaskID)
		if err != nil {
			return "", nil, nil, false, err
		}
		contextData["task_id"] = task.ID
		contextData["title"] = task.Title
		contextData["priority"] = task.Priority
		contextData["status"] = task.Status
		if task.DueAt != nil {
			contextData["due_at"] = task.DueAt.UTC().Format(time.RFC3339)
		}
		if text == "" {
			text = task.Title + "\n" + task.Description
		}
	}
	switch feature {
	case model.AIFeatureDuplicateSuggestion:
		if req.WorkspaceID == nil {
			return "", nil, nil, false, ErrInvalidAIAssistanceRequest
		}
		page, err := s.tasks.FindAll(*req.WorkspaceID, model.TaskQuery{Page: 1, Limit: 100, Sort: "updated_at", Order: "desc"})
		if err != nil {
			return "", nil, nil, false, err
		}
		contextData["duplicate_candidates"] = duplicateAICandidates(text, page.Items, req.TaskID)
	case model.AIFeatureProjectSummary:
		if req.WorkspaceID == nil || req.ProjectID == nil || *req.ProjectID <= 0 {
			return "", nil, nil, false, ErrInvalidAIAssistanceRequest
		}
		page, err := s.tasks.FindAll(*req.WorkspaceID, model.TaskQuery{Page: 1, Limit: 100, ProjectID: req.ProjectID, Sort: "updated_at", Order: "desc"})
		if err != nil {
			return "", nil, nil, false, err
		}
		contextData["project_id"] = *req.ProjectID
		contextData["tasks"] = compactAITasks(page.Items)
		if text == "" {
			text = fmt.Sprintf("Summarize project %d with %d tasks", *req.ProjectID, len(page.Items))
		}
	case model.AIFeatureIncidentSummary:
		if req.IncidentID == nil || *req.IncidentID <= 0 {
			return "", nil, nil, false, ErrInvalidAIAssistanceRequest
		}
		items, err := s.operations.ListOperationalIncidents(organizationID, "", time.Time{}, time.Now().UTC().AddDate(20, 0, 0))
		if err != nil {
			return "", nil, nil, false, err
		}
		found := false
		for _, incident := range items {
			if incident.ID == *req.IncidentID {
				contextData["incident_id"] = incident.ID
				contextData["title"] = incident.Title
				contextData["severity"] = incident.Severity
				contextData["status"] = incident.Status
				contextData["summary"] = incident.Summary
				if text == "" {
					text = incident.Title + "\n" + incident.Summary
				}
				found = true
				break
			}
		}
		if !found {
			return "", nil, nil, false, repository.ErrOperationalIncidentNotFound
		}
	case model.AIFeatureRiskWorkflowSuggestion, model.AIFeatureNaturalLanguageReport:
		if req.WorkspaceID == nil {
			return "", nil, nil, false, ErrInvalidAIAssistanceRequest
		}
		page, err := s.tasks.FindAll(*req.WorkspaceID, model.TaskQuery{Page: 1, Limit: 100, Sort: "updated_at", Order: "desc"})
		if err != nil {
			return "", nil, nil, false, err
		}
		contextData["tasks"] = compactAITasks(page.Items)
		contextData["risks"], contextData["workflow_suggestions"] = riskAndWorkflowSuggestions(page.Items)
		if text == "" {
			text = "Summarize task portfolio health and recommended workflow actions"
		}
	}
	if text == "" {
		return "", nil, nil, false, ErrInvalidAIAssistanceRequest
	}
	return text, contextData, proposedAction, destructive, nil
}

func (s *AIAssistanceService) executeApprovedAction(actorUserID int64, item model.AIRequest) error {
	if len(item.ProposedAction) == 0 || item.WorkspaceID == nil {
		return ErrAIActionNotAllowed
	}
	action := strings.ToLower(strings.TrimSpace(fmt.Sprint(item.ProposedAction["type"])))
	taskID, ok := aiInt64(item.ProposedAction["task_id"])
	if !ok || taskID <= 0 {
		return ErrAIActionNotAllowed
	}
	task, err := s.tasks.FindByID(*item.WorkspaceID, taskID)
	if err != nil {
		return err
	}
	switch action {
	case "task.update_description":
		value := strings.TrimSpace(fmt.Sprint(item.ProposedAction["description"]))
		if value == "" || len([]rune(value)) > 20000 {
			return ErrAIActionNotAllowed
		}
		_, err = s.taskSvc.Update(*item.WorkspaceID, taskID, actorUserID, taskUpdateRequestFromExisting(task, value, task.Priority))
		return err
	case "task.update_priority":
		priority := strings.ToUpper(strings.TrimSpace(fmt.Sprint(item.ProposedAction["priority"])))
		if priority != model.TaskPriorityLow && priority != model.TaskPriorityMedium && priority != model.TaskPriorityHigh && priority != model.TaskPriorityUrgent {
			return ErrAIActionNotAllowed
		}
		_, err = s.taskSvc.Update(*item.WorkspaceID, taskID, actorUserID, taskUpdateRequestFromExisting(task, task.Description, priority))
		return err
	case "task.archive":
		_, err = s.taskSvc.Archive(*item.WorkspaceID, taskID, actorUserID)
		return err
	default:
		return ErrAIActionNotAllowed
	}
}

func taskUpdateRequestFromExisting(task model.Task, description, priority string) model.UpdateTaskRequest {
	return model.UpdateTaskRequest{
		Title: task.Title, Description: description, Status: task.Status, Priority: priority,
		StartAt: task.StartAt, DueAt: task.DueAt, ProjectID: task.ProjectID, ListID: task.ListID,
		ParentTaskID: task.ParentTaskID, Position: task.Position, EstimatedMinutes: task.EstimatedMinutes,
		ActualMinutes: task.ActualMinutes, Version: task.Version,
	}
}

func (s *AIAssistanceService) checkBudget(organizationID int64, policy model.AIPolicy, estimated int64, at time.Time) error {
	if policy.MonthlyBudgetCents == 0 {
		return nil
	}
	from := time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	usage, err := s.repo.MonthlyUsage(organizationID, from, to)
	if err != nil {
		return err
	}
	if usage.SpentCents+estimated > policy.MonthlyBudgetCents {
		return ErrAIBudgetExceeded
	}
	return nil
}

func (s *AIAssistanceService) policyOrDefault(organizationID int64) (model.AIPolicy, error) {
	policy, err := s.repo.GetPolicy(organizationID)
	if err == nil {
		return policy, nil
	}
	if !errors.Is(err, repository.ErrAIPolicyNotFound) {
		return model.AIPolicy{}, err
	}
	return model.AIPolicy{
		OrganizationID: organizationID, Enabled: false, Provider: model.AIProviderLocalRules,
		MonthlyBudgetCents: 0, RedactionEnabled: true, MaxInputChars: 12000,
		AllowedClassifications:         []string{model.DataClassificationPublic, model.DataClassificationInternal},
		ExternalMaxClassification:      model.DataClassificationInternal,
		RequireHumanApprovalForActions: true,
	}, nil
}

func (s *AIAssistanceService) requireMember(userID, organizationID int64) (model.OrganizationMember, error) {
	org, err := s.orgs.GetOrganization(organizationID)
	if err != nil {
		return model.OrganizationMember{}, err
	}
	if org.Status != model.OrganizationStatusActive {
		return model.OrganizationMember{}, ErrOrganizationInactive
	}
	member, err := s.orgs.GetMember(organizationID, userID)
	if err != nil {
		return model.OrganizationMember{}, ErrAIAssistanceForbidden
	}
	return member, nil
}

func (s *AIAssistanceService) requireAdmin(userID, organizationID int64) (model.OrganizationMember, error) {
	member, err := s.requireMember(userID, organizationID)
	if err != nil {
		return model.OrganizationMember{}, err
	}
	if member.Role != model.OrganizationRoleOwner && member.Role != model.OrganizationRoleAdmin && member.Role != model.OrganizationRoleDelegatedAdmin {
		return model.OrganizationMember{}, ErrAIAssistanceForbidden
	}
	return member, nil
}

func (s *AIAssistanceService) workspaceBelongsToOrganization(organizationID, workspaceID int64) bool {
	items, err := s.orgs.ListWorkspaces(organizationID)
	if err != nil {
		return false
	}
	for _, item := range items {
		if item.WorkspaceID == workspaceID {
			return true
		}
	}
	return false
}

func (s *AIAssistanceService) audit(organizationID int64, actorID *int64, action, resourceType, resourceID string, metadata map[string]any, now time.Time) error {
	return s.orgs.RecordAudit(model.OrganizationAuditEvent{
		OrganizationID: organizationID, ActorUserID: actorID, Action: action,
		ResourceType: resourceType, ResourceID: resourceID, Metadata: metadata, CreatedAt: now,
	})
}

func validAIFeature(value string) bool {
	switch value {
	case model.AIFeatureTaskSummary, model.AIFeatureDescriptionImprovement, model.AIFeatureSubtasks,
		model.AIFeaturePrioritySuggestion, model.AIFeatureDuplicateSuggestion, model.AIFeatureSemanticSearch,
		model.AIFeatureProjectSummary, model.AIFeatureIncidentSummary, model.AIFeatureRiskWorkflowSuggestion,
		model.AIFeatureNaturalLanguageReport:
		return true
	default:
		return false
	}
}

func normalizeAIClassification(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case model.DataClassificationPublic, model.DataClassificationInternal,
		model.DataClassificationConfidential, model.DataClassificationRestricted:
		return value
	default:
		return ""
	}
}

func normalizeAIClassifications(values []string) []string {
	seen := map[string]struct{}{}
	items := make([]string, 0, len(values))
	for _, raw := range values {
		value := normalizeAIClassification(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	sort.Slice(items, func(i, j int) bool { return aiClassificationRank(items[i]) < aiClassificationRank(items[j]) })
	return items
}

func aiClassificationRank(value string) int {
	switch value {
	case model.DataClassificationPublic:
		return 1
	case model.DataClassificationInternal:
		return 2
	case model.DataClassificationConfidential:
		return 3
	case model.DataClassificationRestricted:
		return 4
	default:
		return 99
	}
}

func providerSupportsClassification(provider AIProvider, classification string) bool {
	return containsAIString(provider.SupportedClassifications(), classification)
}

func containsAIString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

var (
	aiEmailPattern  = regexp.MustCompile(`(?i)\b[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}\b`)
	aiPhonePattern  = regexp.MustCompile(`\b(?:\+?\d[\d .()-]{7,}\d)\b`)
	aiBearerPattern = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{8,}\b`)
	aiSecretPattern = regexp.MustCompile(`(?i)\b(api[_-]?key|secret|token|password)\s*[:=]\s*[^\s,;]{4,}`)
)

func redactAIText(input string) (string, int) {
	output := input
	count := 0
	for _, rule := range []struct {
		replacement string
		pattern     *regexp.Regexp
	}{
		{"[REDACTED_EMAIL]", aiEmailPattern},
		{"[REDACTED_PHONE]", aiPhonePattern},
		{"[REDACTED_BEARER]", aiBearerPattern},
		{"$1=[REDACTED_SECRET]", aiSecretPattern},
	} {
		matches := rule.pattern.FindAllStringIndex(output, -1)
		count += len(matches)
		output = rule.pattern.ReplaceAllString(output, rule.replacement)
	}
	return output, count
}

func actionFromAIResult(feature string, req model.AIAssistRequest, result map[string]any) map[string]any {
	if req.TaskID == nil {
		return map[string]any{}
	}
	switch feature {
	case model.AIFeatureDescriptionImprovement:
		if value, ok := result["description"].(string); ok && strings.TrimSpace(value) != "" {
			return map[string]any{"type": "task.update_description", "task_id": *req.TaskID, "description": value}
		}
	case model.AIFeaturePrioritySuggestion:
		if value, ok := result["priority"].(string); ok && strings.TrimSpace(value) != "" {
			return map[string]any{"type": "task.update_priority", "task_id": *req.TaskID, "priority": value}
		}
	}
	return map[string]any{}
}

func isDestructiveAIAction(action map[string]any) bool {
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(action["type"]))) {
	case "task.archive", "task.delete", "workflow.execute", "workflow.cancel":
		return true
	default:
		return false
	}
}

func aiInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case float64:
		if typed == math.Trunc(typed) {
			return int64(typed), true
		}
	}
	return 0, false
}

func cloneServiceMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func sortedAIMapKeys(input map[string]any) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func summarizeText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if len([]rune(text)) <= limit {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

func improveDescription(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return ""
	}
	runes := []rune(text)
	runes[0] = unicode.ToUpper(runes[0])
	result := string(runes)
	if !strings.HasSuffix(result, ".") && !strings.HasSuffix(result, "!") && !strings.HasSuffix(result, "?") {
		result += "."
	}
	return result
}

func suggestSubtasks(text string) []map[string]any {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || r == ';' || r == '.' || r == ','
	})
	items := make([]map[string]any, 0, 5)
	seen := map[string]struct{}{}
	for _, part := range parts {
		title := strings.TrimSpace(part)
		if len([]rune(title)) < 4 {
			continue
		}
		key := strings.ToLower(title)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, map[string]any{"title": title, "priority": model.TaskPriorityMedium})
		if len(items) == 5 {
			break
		}
	}
	if len(items) == 0 {
		items = append(items,
			map[string]any{"title": "Clarify acceptance criteria", "priority": model.TaskPriorityMedium},
			map[string]any{"title": "Implement and verify the change", "priority": model.TaskPriorityMedium},
		)
	}
	return items
}

func suggestPriority(text string, context map[string]any) (string, []string) {
	lower := strings.ToLower(text)
	reasons := make([]string, 0)
	priority := model.TaskPriorityMedium
	if strings.Contains(lower, "critical") || strings.Contains(lower, "outage") || strings.Contains(lower, "urgent") || strings.Contains(lower, "security") {
		priority = model.TaskPriorityUrgent
		reasons = append(reasons, "urgent or high-impact language detected")
	} else if strings.Contains(lower, "block") || strings.Contains(lower, "deadline") || strings.Contains(lower, "customer") {
		priority = model.TaskPriorityHigh
		reasons = append(reasons, "dependency, deadline, or customer impact detected")
	}
	if raw, ok := context["due_at"].(string); ok {
		if due, err := time.Parse(time.RFC3339, raw); err == nil && due.Before(time.Now().UTC().Add(24*time.Hour)) {
			if priority == model.TaskPriorityMedium {
				priority = model.TaskPriorityHigh
			}
			reasons = append(reasons, "due date is within 24 hours")
		}
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "no urgent risk signals detected")
	}
	return priority, reasons
}

func duplicateAICandidates(text string, tasks []model.Task, excludeID *int64) []map[string]any {
	queryTokens := semanticAITokens(text)
	items := make([]map[string]any, 0)
	for _, task := range tasks {
		if excludeID != nil && task.ID == *excludeID {
			continue
		}
		score, _ := semanticAIScore(queryTokens, semanticAITokens(task.Title+" "+task.Description))
		if score < 0.20 {
			continue
		}
		items = append(items, map[string]any{"task_id": task.ID, "title": task.Title, "score": score})
	}
	sort.Slice(items, func(i, j int) bool {
		left, _ := items[i]["score"].(float64)
		right, _ := items[j]["score"].(float64)
		return left > right
	})
	if len(items) > 5 {
		items = items[:5]
	}
	return items
}

func compactAITasks(tasks []model.Task) []map[string]any {
	items := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		item := map[string]any{
			"id": task.ID, "title": task.Title, "status": task.Status, "priority": task.Priority,
			"description": summarizeText(task.Description, 240),
		}
		if task.DueAt != nil {
			item["due_at"] = task.DueAt.UTC().Format(time.RFC3339)
		}
		items = append(items, item)
	}
	return items
}

func summarizeProject(context map[string]any) string {
	raw, _ := context["tasks"].([]map[string]any)
	if len(raw) == 0 {
		if values, ok := context["tasks"].([]any); ok {
			return fmt.Sprintf("Project contains %d tracked tasks.", len(values))
		}
		return "Project has no tracked tasks."
	}
	statuses := map[string]int{}
	priorities := map[string]int{}
	for _, task := range raw {
		statuses[fmt.Sprint(task["status"])]++
		priorities[fmt.Sprint(task["priority"])]++
	}
	return fmt.Sprintf("Project contains %d tasks. Status mix: %v. Priority mix: %v.", len(raw), statuses, priorities)
}

func summarizeIncident(context map[string]any) string {
	return fmt.Sprintf("%s is a %s incident currently %s. %s",
		strings.TrimSpace(fmt.Sprint(context["title"])),
		strings.TrimSpace(fmt.Sprint(context["severity"])),
		strings.TrimSpace(fmt.Sprint(context["status"])),
		strings.TrimSpace(fmt.Sprint(context["summary"])),
	)
}

func riskAndWorkflowSuggestions(tasks []model.Task) ([]map[string]any, []map[string]any) {
	now := time.Now().UTC()
	risks := make([]map[string]any, 0)
	suggestions := make([]map[string]any, 0)
	var blocked, overdue, urgent int
	for _, task := range tasks {
		if task.Status == model.TaskStatusBlocked {
			blocked++
		}
		if task.Priority == model.TaskPriorityUrgent && task.Status != model.TaskStatusDone {
			urgent++
		}
		if task.DueAt != nil && task.DueAt.Before(now) && task.Status != model.TaskStatusDone {
			overdue++
		}
	}
	if blocked > 0 {
		risks = append(risks, map[string]any{"type": "blocked_tasks", "count": blocked, "severity": "high"})
		suggestions = append(suggestions, map[string]any{"type": "workflow", "suggestion": "Add blocker escalation and owner review"})
	}
	if overdue > 0 {
		risks = append(risks, map[string]any{"type": "overdue_tasks", "count": overdue, "severity": "high"})
		suggestions = append(suggestions, map[string]any{"type": "workflow", "suggestion": "Add due-date reminder and escalation"})
	}
	if urgent > 0 {
		risks = append(risks, map[string]any{"type": "urgent_open_tasks", "count": urgent, "severity": "medium"})
	}
	if len(risks) == 0 {
		risks = append(risks, map[string]any{"type": "portfolio_health", "count": 0, "severity": "low"})
	}
	return risks, suggestions
}

func naturalLanguageReport(context map[string]any, prompt string) string {
	tasks, _ := context["tasks"].([]map[string]any)
	if len(tasks) == 0 {
		return "No task data is available for this report."
	}
	statuses := map[string]int{}
	priorities := map[string]int{}
	for _, task := range tasks {
		statuses[fmt.Sprint(task["status"])]++
		priorities[fmt.Sprint(task["priority"])]++
	}
	return fmt.Sprintf("%s Total tasks: %d. Status distribution: %v. Priority distribution: %v.",
		summarizeText(prompt, 160), len(tasks), statuses, priorities)
}

func normalizeAIKeywords(values []string) []string {
	seen := map[string]struct{}{}
	items := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}

var semanticAISynonyms = map[string]string{
	"bug": "issue", "defect": "issue", "error": "issue", "failure": "issue",
	"urgent": "priority", "critical": "priority", "important": "priority",
	"deadline": "due", "overdue": "due", "late": "due",
	"client": "customer", "buyer": "customer",
	"login": "auth", "authentication": "auth", "authorization": "auth",
}

func semanticAITokens(value string) map[string]struct{} {
	fields := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := map[string]struct{}{}
	for _, field := range fields {
		if len(field) < 2 {
			continue
		}
		if canonical, ok := semanticAISynonyms[field]; ok {
			field = canonical
		}
		out[field] = struct{}{}
	}
	return out
}

func semanticAIScore(query, document map[string]struct{}) (float64, string) {
	if len(query) == 0 || len(document) == 0 {
		return 0, ""
	}
	intersection := 0
	for token := range query {
		if _, ok := document[token]; ok {
			intersection++
		}
	}
	if intersection == 0 {
		return 0, ""
	}
	union := len(query) + len(document) - intersection
	score := float64(intersection) / float64(union)
	return score, fmt.Sprintf("%d normalized concepts matched", intersection)
}
