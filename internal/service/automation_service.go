package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrAutomationForbidden       = errors.New("automation action is forbidden")
	ErrInvalidAutomationPolicy   = errors.New("invalid automation policy")
	ErrInvalidAutomationDecision = errors.New("invalid automation approval decision")
	ErrAutomationLimit           = errors.New("automation policy limit reached")
)

type AutomationEntitlementProvider interface {
	EffectiveLimits(organizationID int64, at time.Time) (model.BillingLimits, error)
}

type AutomationService struct {
	repo         repository.AutomationRepository
	orgs         repository.OrganizationRepository
	operations   repository.OperationsRepository
	billing      repository.BillingRepository
	entitlements AutomationEntitlementProvider
}

func NewAutomationService(
	repo repository.AutomationRepository,
	orgs repository.OrganizationRepository,
	operations repository.OperationsRepository,
	billing repository.BillingRepository,
	entitlements AutomationEntitlementProvider,
) *AutomationService {
	return &AutomationService{
		repo: repo, orgs: orgs, operations: operations,
		billing: billing, entitlements: entitlements,
	}
}

func (s *AutomationService) Policies(actorUserID, organizationID int64) ([]model.AutomationPolicy, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListAutomationPolicies(organizationID)
}

func (s *AutomationService) CreatePolicy(actorUserID, organizationID int64, req model.CreateAutomationPolicyRequest) (model.AutomationPolicy, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.AutomationPolicy{}, err
	}
	items, err := s.repo.ListAutomationPolicies(organizationID)
	if err != nil {
		return model.AutomationPolicy{}, err
	}
	if len(items) >= 50 {
		return model.AutomationPolicy{}, ErrAutomationLimit
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	now := time.Now().UTC()
	item := model.AutomationPolicy{
		OrganizationID:  organizationID,
		Name:            strings.TrimSpace(req.Name),
		Enabled:         enabled,
		TriggerType:     strings.ToLower(strings.TrimSpace(req.TriggerType)),
		TriggerKey:      strings.ToLower(strings.TrimSpace(req.TriggerKey)),
		Comparator:      strings.ToLower(strings.TrimSpace(req.Comparator)),
		Threshold:       req.Threshold,
		ActionType:      strings.ToLower(strings.TrimSpace(req.ActionType)),
		ActionConfig:    req.ActionConfig,
		ApprovalMode:    strings.ToLower(strings.TrimSpace(req.ApprovalMode)),
		CooldownMinutes: req.CooldownMinutes,
		CreatedByUserID: actorUserID,
		UpdatedByUserID: actorUserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := validateAutomationPolicy(item); err != nil {
		return model.AutomationPolicy{}, err
	}
	created, err := s.repo.CreateAutomationPolicy(item)
	if err == nil {
		s.audit(organizationID, &actorUserID, "automation.policy.created", "automation_policy", fmt.Sprint(created.ID), map[string]any{
			"trigger_type": created.TriggerType,
			"action_type": created.ActionType,
			"approval_mode": created.ApprovalMode,
		})
	}
	return created, err
}

func (s *AutomationService) UpdatePolicy(actorUserID, organizationID, policyID int64, req model.UpdateAutomationPolicyRequest) (model.AutomationPolicy, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.AutomationPolicy{}, err
	}
	existing, err := s.repo.GetAutomationPolicy(organizationID, policyID)
	if err != nil {
		return model.AutomationPolicy{}, err
	}
	existing.Name = strings.TrimSpace(req.Name)
	existing.Enabled = req.Enabled
	existing.TriggerType = strings.ToLower(strings.TrimSpace(req.TriggerType))
	existing.TriggerKey = strings.ToLower(strings.TrimSpace(req.TriggerKey))
	existing.Comparator = strings.ToLower(strings.TrimSpace(req.Comparator))
	existing.Threshold = req.Threshold
	existing.ActionType = strings.ToLower(strings.TrimSpace(req.ActionType))
	existing.ActionConfig = req.ActionConfig
	existing.ApprovalMode = strings.ToLower(strings.TrimSpace(req.ApprovalMode))
	existing.CooldownMinutes = req.CooldownMinutes
	existing.UpdatedByUserID = actorUserID
	existing.UpdatedAt = time.Now().UTC()
	if err := validateAutomationPolicy(existing); err != nil {
		return model.AutomationPolicy{}, err
	}
	updated, err := s.repo.UpdateAutomationPolicy(existing)
	if err == nil {
		s.audit(organizationID, &actorUserID, "automation.policy.updated", "automation_policy", fmt.Sprint(policyID), map[string]any{
			"enabled": updated.Enabled,
			"approval_mode": updated.ApprovalMode,
		})
	}
	return updated, err
}

func (s *AutomationService) Executions(actorUserID, organizationID int64) ([]model.AutomationExecution, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListAutomationExecutions(organizationID, 200)
}

func (s *AutomationService) Evaluate(actorUserID, organizationID int64) ([]model.AutomationExecution, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	policies, err := s.repo.ListAutomationPolicies(organizationID)
	if err != nil {
		return nil, err
	}
	return s.evaluatePolicies(policies, &actorUserID)
}

func (s *AutomationService) EvaluateAllSystem() ([]model.AutomationExecution, error) {
	policies, err := s.repo.ListEnabledAutomationPolicies()
	if err != nil {
		return nil, err
	}
	return s.evaluatePolicies(policies, nil)
}

func (s *AutomationService) DecideExecution(actorUserID, organizationID, executionID int64, req model.DecideAutomationExecutionRequest) (model.AutomationExecution, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.AutomationExecution{}, err
	}
	execution, err := s.repo.GetAutomationExecution(organizationID, executionID)
	if err != nil {
		return model.AutomationExecution{}, err
	}
	if execution.Status != model.AutomationExecutionPendingApproval {
		return model.AutomationExecution{}, ErrInvalidAutomationDecision
	}
	now := time.Now().UTC()
	if !req.Approve {
		execution.Status = model.AutomationExecutionRejected
		execution.RejectedAt = &now
		execution.RejectedByUserID = &actorUserID
		execution.UpdatedAt = now
		updated, err := s.repo.UpdateAutomationExecution(execution)
		if err == nil {
			s.audit(organizationID, &actorUserID, "automation.execution.rejected", "automation_execution", fmt.Sprint(executionID), nil)
		}
		return updated, err
	}
	execution.Status = model.AutomationExecutionApproved
	execution.ApprovedAt = &now
	execution.ApprovedByUserID = &actorUserID
	execution.UpdatedAt = now
	execution, err = s.repo.UpdateAutomationExecution(execution)
	if err != nil {
		return model.AutomationExecution{}, err
	}
	s.audit(organizationID, &actorUserID, "automation.execution.approved", "automation_execution", fmt.Sprint(executionID), nil)
	return s.execute(execution, actorUserID)
}

func (s *AutomationService) evaluatePolicies(policies []model.AutomationPolicy, actorUserID *int64) ([]model.AutomationExecution, error) {
	now := time.Now().UTC()
	created := make([]model.AutomationExecution, 0)
	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}
		latest, err := s.repo.LatestAutomationExecution(policy.ID)
		if err == nil && now.Sub(latest.RequestedAt) < time.Duration(policy.CooldownMinutes)*time.Minute {
			continue
		}
		if err != nil && !errors.Is(err, repository.ErrAutomationExecutionNotFound) {
			return nil, err
		}
		snapshot, dedupeKey, matched, err := s.matchPolicy(policy, now)
		if err != nil {
			return nil, err
		}
		if !matched {
			continue
		}
		status := model.AutomationExecutionPendingApproval
		var approvedAt *time.Time
		if policy.ApprovalMode == model.AutomationApprovalAutomatic {
			status = model.AutomationExecutionApproved
			approvedAt = &now
		}
		execution, err := s.repo.CreateAutomationExecution(model.AutomationExecution{
			OrganizationID:  policy.OrganizationID,
			PolicyID:        policy.ID,
			DedupeKey:       dedupeKey,
			Status:          status,
			TriggerSnapshot: snapshot,
			ActionResult:    map[string]any{},
			RequestedAt:     now,
			ApprovedAt:      approvedAt,
			CreatedAt:       now,
			UpdatedAt:       now,
		})
		if errors.Is(err, repository.ErrAutomationExecutionExists) {
			continue
		}
		if err != nil {
			return nil, err
		}
		s.audit(policy.OrganizationID, actorUserID, "automation.execution.requested", "automation_execution", fmt.Sprint(execution.ID), map[string]any{
			"policy_id": policy.ID,
			"approval_mode": policy.ApprovalMode,
		})
		if policy.ApprovalMode == model.AutomationApprovalAutomatic {
			execution, err = s.execute(execution, policy.CreatedByUserID)
			if err != nil {
				return nil, err
			}
		}
		created = append(created, execution)
	}
	return created, nil
}

func (s *AutomationService) matchPolicy(policy model.AutomationPolicy, now time.Time) (map[string]any, string, bool, error) {
	switch policy.TriggerType {
	case model.AutomationTriggerOperationsAlert:
		alerts, err := s.operations.ListOperationalAlerts(policy.OrganizationID, model.OperationalAlertOpen, 200)
		if err != nil {
			return nil, "", false, err
		}
		for _, alert := range alerts {
			if policy.TriggerKey != "*" && alert.Type != policy.TriggerKey {
				continue
			}
			if !automationCompare(alert.CurrentValue, policy.Comparator, policy.Threshold) {
				continue
			}
			return map[string]any{
				"trigger_type": "operations_alert",
				"alert_id": alert.ID,
				"alert_type": alert.Type,
				"metric": alert.Metric,
				"current_value": alert.CurrentValue,
				"threshold_value": alert.ThresholdValue,
			}, fmt.Sprintf("policy:%d:alert:%d", policy.ID, alert.ID), true, nil
		}
		return nil, "", false, nil

	case model.AutomationTriggerBillingUsagePercent:
		start, end := automationMonth(now)
		usage, err := s.billing.ListBillingUsage(policy.OrganizationID, start, end)
		if err != nil {
			return nil, "", false, err
		}
		limits, err := s.entitlements.EffectiveLimits(policy.OrganizationID, now)
		if err != nil {
			return nil, "", false, err
		}
		var quantity int64
		for _, item := range usage {
			if item.Metric == policy.TriggerKey {
				quantity = item.Quantity
				break
			}
		}
		limit := limits.MonthlyAPIOperations
		if limit <= 0 {
			return nil, "", false, nil
		}
		utilization := quantity * 100 / limit
		if !automationCompare(utilization, policy.Comparator, policy.Threshold) {
			return nil, "", false, nil
		}
		bucketSeconds := int64(policy.CooldownMinutes * 60)
		bucket := now.Unix() / bucketSeconds
		return map[string]any{
			"trigger_type": "billing_usage_percent",
			"metric": policy.TriggerKey,
			"quantity": quantity,
			"limit": limit,
			"utilization_percent": utilization,
		}, fmt.Sprintf("policy:%d:billing:%s:%d", policy.ID, policy.TriggerKey, bucket), true, nil
	default:
		return nil, "", false, ErrInvalidAutomationPolicy
	}
}

func (s *AutomationService) execute(execution model.AutomationExecution, actionActorUserID int64) (model.AutomationExecution, error) {
	policy, err := s.repo.GetAutomationPolicy(execution.OrganizationID, execution.PolicyID)
	if err != nil {
		return model.AutomationExecution{}, err
	}
	now := time.Now().UTC()
	execution.Status = model.AutomationExecutionRunning
	execution.ExecutedAt = &now
	execution.UpdatedAt = now
	execution, err = s.repo.UpdateAutomationExecution(execution)
	if err != nil {
		return model.AutomationExecution{}, err
	}

	var actionResult map[string]any
	var actionErr error
	switch policy.ActionType {
	case model.AutomationActionOpenIncident:
		actionResult, actionErr = s.openIncident(policy, actionActorUserID, now)
	default:
		actionErr = ErrInvalidAutomationPolicy
	}

	completedAt := time.Now().UTC()
	execution.CompletedAt = &completedAt
	execution.UpdatedAt = completedAt
	if actionErr != nil {
		execution.Status = model.AutomationExecutionFailed
		execution.ErrorMessage = actionErr.Error()
	} else {
		execution.Status = model.AutomationExecutionSucceeded
		execution.ActionResult = actionResult
	}
	updated, updateErr := s.repo.UpdateAutomationExecution(execution)
	if updateErr != nil {
		return model.AutomationExecution{}, updateErr
	}
	var actor *int64
	if actionActorUserID > 0 {
		actor = &actionActorUserID
	}
	s.audit(execution.OrganizationID, actor, "automation.execution.completed", "automation_execution", fmt.Sprint(execution.ID), map[string]any{
		"status": updated.Status,
		"action_type": policy.ActionType,
	})
	return updated, nil
}

func (s *AutomationService) openIncident(policy model.AutomationPolicy, actorUserID int64, now time.Time) (map[string]any, error) {
	severity := automationConfigString(policy.ActionConfig, "severity")
	title := automationConfigString(policy.ActionConfig, "title")
	summary := automationConfigString(policy.ActionConfig, "summary")
	if title == "" {
		title = "Automation policy triggered: " + policy.Name
	}
	if summary == "" {
		summary = "Created by guarded automation policy " + policy.Name
	}
	incident, err := s.operations.CreateOperationalIncident(model.OperationalIncident{
		OrganizationID:  policy.OrganizationID,
		Title:           title,
		Severity:        severity,
		Status:          model.IncidentStatusOpen,
		Summary:         summary,
		StartedAt:       now,
		CreatedByUserID: actorUserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"incident_id": incident.ID,
		"severity": incident.Severity,
		"status": incident.Status,
	}, nil
}

func (s *AutomationService) requireAdmin(userID, organizationID int64) (model.OrganizationMember, error) {
	member, err := s.orgs.GetMember(organizationID, userID)
	if err != nil {
		return model.OrganizationMember{}, ErrAutomationForbidden
	}
	switch member.Role {
	case model.OrganizationRoleOwner, model.OrganizationRoleAdmin, model.OrganizationRoleDelegatedAdmin:
		return member, nil
	default:
		return model.OrganizationMember{}, ErrAutomationForbidden
	}
}

func (s *AutomationService) audit(organizationID int64, actorUserID *int64, action, resourceType, resourceID string, metadata map[string]any) {
	_ = s.orgs.RecordAudit(model.OrganizationAuditEvent{
		OrganizationID: organizationID,
		ActorUserID:    actorUserID,
		Action:         action,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Metadata:       metadata,
		CreatedAt:      time.Now().UTC(),
	})
}

func validateAutomationPolicy(policy model.AutomationPolicy) error {
	if policy.Name == "" || len(policy.Name) > 200 ||
		policy.CooldownMinutes < 1 || policy.CooldownMinutes > 10080 ||
		!validAutomationComparator(policy.Comparator) ||
		(policy.ApprovalMode != model.AutomationApprovalAutomatic && policy.ApprovalMode != model.AutomationApprovalRequired) ||
		policy.ActionType != model.AutomationActionOpenIncident {
		return ErrInvalidAutomationPolicy
	}
	switch policy.TriggerType {
	case model.AutomationTriggerOperationsAlert:
		if policy.TriggerKey == "" || len(policy.TriggerKey) > 100 {
			return ErrInvalidAutomationPolicy
		}
	case model.AutomationTriggerBillingUsagePercent:
		if policy.TriggerKey != "api_operations" || policy.Threshold < 1 || policy.Threshold > 10000 {
			return ErrInvalidAutomationPolicy
		}
	default:
		return ErrInvalidAutomationPolicy
	}
	severity := automationConfigString(policy.ActionConfig, "severity")
	if !validIncidentSeverity(severity) {
		return ErrInvalidAutomationPolicy
	}
	title := automationConfigString(policy.ActionConfig, "title")
	summary := automationConfigString(policy.ActionConfig, "summary")
	if len(title) > 200 || len(summary) > 8000 {
		return ErrInvalidAutomationPolicy
	}
	if policy.ApprovalMode == model.AutomationApprovalAutomatic &&
		(severity == model.IncidentSeverity1 || severity == model.IncidentSeverity2) {
		return ErrInvalidAutomationPolicy
	}
	return nil
}

func automationConfigString(config map[string]any, key string) string {
	value, ok := config[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func validAutomationComparator(value string) bool {
	switch value {
	case model.AutomationComparatorEqual, model.AutomationComparatorGreaterOrEqual, model.AutomationComparatorLessOrEqual:
		return true
	default:
		return false
	}
}

func automationCompare(value int64, comparator string, threshold int64) bool {
	switch comparator {
	case model.AutomationComparatorEqual:
		return value == threshold
	case model.AutomationComparatorGreaterOrEqual:
		return value >= threshold
	case model.AutomationComparatorLessOrEqual:
		return value <= threshold
	default:
		return false
	}
}

func automationMonth(at time.Time) (time.Time, time.Time) {
	at = at.UTC()
	start := time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}
