package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrOperationsForbidden        = errors.New("operations action is forbidden")
	ErrInvalidOperationsPolicy    = errors.New("invalid operations policy")
	ErrInvalidCostAllocation      = errors.New("invalid cost allocation")
	ErrInvalidMaintenanceWindow   = errors.New("invalid maintenance window")
	ErrInvalidOperationalIncident = errors.New("invalid operational incident")
)

type OperationsEntitlementProvider interface {
	EffectiveLimits(organizationID int64, at time.Time) (model.BillingLimits, error)
}

type OperationsService struct {
	repo         repository.OperationsRepository
	orgs         repository.OrganizationRepository
	billing      repository.BillingRepository
	entitlements OperationsEntitlementProvider
}

func NewOperationsService(
	repo repository.OperationsRepository,
	orgs repository.OrganizationRepository,
	billing repository.BillingRepository,
	entitlements OperationsEntitlementProvider,
) *OperationsService {
	return &OperationsService{repo: repo, orgs: orgs, billing: billing, entitlements: entitlements}
}

func (s *OperationsService) Policy(actorUserID, organizationID int64) (model.OperationsPolicy, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.OperationsPolicy{}, err
	}
	return s.policyOrDefault(organizationID, actorUserID, time.Now().UTC())
}

func (s *OperationsService) UpdatePolicy(actorUserID, organizationID int64, req model.UpdateOperationsPolicyRequest) (model.OperationsPolicy, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.OperationsPolicy{}, err
	}
	tier := strings.ToLower(strings.TrimSpace(req.SupportTier))
	if req.MonthlyBudgetCents < 1 || req.MonthlyBudgetCents > 1000000000000000 ||
		req.BudgetAlertThresholdPercent < 1 || req.BudgetAlertThresholdPercent > 100 ||
		req.SLOTargetBasisPoints < 9000 || req.SLOTargetBasisPoints > 10000 ||
		req.IncidentEscalationMinutes < 1 || req.IncidentEscalationMinutes > 10080 ||
		!validSupportTier(tier) || len(strings.TrimSpace(req.SupportContact)) > 255 {
		return model.OperationsPolicy{}, ErrInvalidOperationsPolicy
	}
	now := time.Now().UTC()
	existing, err := s.repo.GetOperationsPolicy(organizationID)
	createdAt := now
	if err == nil {
		createdAt = existing.CreatedAt
	} else if !errors.Is(err, repository.ErrOperationsPolicyNotFound) {
		return model.OperationsPolicy{}, err
	}
	item, err := s.repo.UpsertOperationsPolicy(model.OperationsPolicy{
		OrganizationID:              organizationID,
		MonthlyBudgetCents:          req.MonthlyBudgetCents,
		BudgetAlertThresholdPercent: req.BudgetAlertThresholdPercent,
		SLOTargetBasisPoints:        req.SLOTargetBasisPoints,
		IncidentEscalationMinutes:   req.IncidentEscalationMinutes,
		SupportTier:                 tier,
		SupportContact:              strings.TrimSpace(req.SupportContact),
		UpdatedByUserID:             actorUserID,
		CreatedAt:                   createdAt,
		UpdatedAt:                   now,
	})
	if err == nil {
		s.audit(organizationID, actorUserID, "operations.policy.updated", "operations_policy", fmt.Sprint(organizationID), map[string]any{
			"monthly_budget_cents":    req.MonthlyBudgetCents,
			"slo_target_basis_points": req.SLOTargetBasisPoints,
			"support_tier":            tier,
		})
	}
	return item, err
}

func (s *OperationsService) CreateCostAllocation(actorUserID, organizationID int64, req model.CreateCostAllocationRequest) (model.CostAllocation, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.CostAllocation{}, err
	}
	category := strings.ToLower(strings.TrimSpace(req.Category))
	source := strings.ToLower(strings.TrimSpace(req.Source))
	if category == "" || source == "" || len(category) > 80 || len(source) > 120 ||
		req.AmountCents < 0 || req.AmountCents > 1000000000000000 ||
		req.PeriodStart.IsZero() || req.PeriodEnd.IsZero() || !req.PeriodEnd.After(req.PeriodStart) ||
		req.PeriodEnd.Sub(req.PeriodStart) > 366*24*time.Hour {
		return model.CostAllocation{}, ErrInvalidCostAllocation
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateCostAllocation(model.CostAllocation{
		OrganizationID:  organizationID,
		Category:        category,
		Source:          source,
		AmountCents:     req.AmountCents,
		Currency:        "USD",
		PeriodStart:     req.PeriodStart.UTC(),
		PeriodEnd:       req.PeriodEnd.UTC(),
		Metadata:        req.Metadata,
		CreatedByUserID: actorUserID,
		CreatedAt:       now,
	})
	if err == nil {
		s.audit(organizationID, actorUserID, "operations.cost.allocated", "cost_allocation", fmt.Sprint(item.ID), map[string]any{
			"category": category, "source": source, "amount_cents": req.AmountCents,
		})
	}
	return item, err
}

func (s *OperationsService) Costs(actorUserID, organizationID int64) ([]model.CostAllocation, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	start, end := operationsMonth(time.Now().UTC())
	return s.repo.ListCostAllocations(organizationID, start, end)
}

func (s *OperationsService) Alerts(actorUserID, organizationID int64) ([]model.OperationalAlert, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListOperationalAlerts(organizationID, "", 200)
}

func (s *OperationsService) AcknowledgeAlert(actorUserID, organizationID, alertID int64) (model.OperationalAlert, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.OperationalAlert{}, err
	}
	item, err := s.repo.AcknowledgeOperationalAlert(organizationID, alertID, actorUserID, time.Now().UTC())
	if err == nil {
		s.audit(organizationID, actorUserID, "operations.alert.acknowledged", "operational_alert", fmt.Sprint(alertID), map[string]any{
			"type": item.Type, "metric": item.Metric,
		})
	}
	return item, err
}

func (s *OperationsService) CreateMaintenance(actorUserID, organizationID int64, req model.CreateMaintenanceWindowRequest) (model.MaintenanceWindow, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.MaintenanceWindow{}, err
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || len(title) > 200 || len(strings.TrimSpace(req.Description)) > 4000 ||
		req.StartsAt.IsZero() || req.EndsAt.IsZero() || !req.EndsAt.After(req.StartsAt) ||
		req.EndsAt.Sub(req.StartsAt) > 30*24*time.Hour {
		return model.MaintenanceWindow{}, ErrInvalidMaintenanceWindow
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateMaintenanceWindow(model.MaintenanceWindow{
		OrganizationID:  organizationID,
		Title:           title,
		Description:     strings.TrimSpace(req.Description),
		Status:          model.MaintenanceStatusScheduled,
		StartsAt:        req.StartsAt.UTC(),
		EndsAt:          req.EndsAt.UTC(),
		CreatedByUserID: actorUserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err == nil {
		s.audit(organizationID, actorUserID, "operations.maintenance.created", "maintenance_window", fmt.Sprint(item.ID), map[string]any{
			"starts_at": item.StartsAt, "ends_at": item.EndsAt,
		})
	}
	return item, err
}

func (s *OperationsService) Maintenance(actorUserID, organizationID int64) ([]model.MaintenanceWindow, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return s.repo.ListMaintenanceWindows(organizationID, now.Add(-30*24*time.Hour), now.Add(90*24*time.Hour))
}

func (s *OperationsService) UpdateMaintenance(actorUserID, organizationID, windowID int64, req model.UpdateMaintenanceWindowRequest) (model.MaintenanceWindow, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.MaintenanceWindow{}, err
	}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status != model.MaintenanceStatusScheduled && status != model.MaintenanceStatusCanceled && status != model.MaintenanceStatusCompleted {
		return model.MaintenanceWindow{}, ErrInvalidMaintenanceWindow
	}
	item, err := s.repo.UpdateMaintenanceWindowStatus(organizationID, windowID, status, time.Now().UTC())
	if err == nil {
		s.audit(organizationID, actorUserID, "operations.maintenance.status_updated", "maintenance_window", fmt.Sprint(windowID), map[string]any{
			"status": status,
		})
	}
	return item, err
}

func (s *OperationsService) CreateIncident(actorUserID, organizationID int64, req model.CreateOperationalIncidentRequest) (model.OperationalIncident, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.OperationalIncident{}, err
	}
	title := strings.TrimSpace(req.Title)
	severity := strings.ToLower(strings.TrimSpace(req.Severity))
	startedAt := req.StartedAt.UTC()
	if req.StartedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	if title == "" || len(title) > 200 || !validIncidentSeverity(severity) || len(strings.TrimSpace(req.Summary)) > 8000 ||
		startedAt.After(time.Now().UTC().Add(5*time.Minute)) {
		return model.OperationalIncident{}, ErrInvalidOperationalIncident
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateOperationalIncident(model.OperationalIncident{
		OrganizationID:  organizationID,
		Title:           title,
		Severity:        severity,
		Status:          model.IncidentStatusOpen,
		Summary:         strings.TrimSpace(req.Summary),
		StartedAt:       startedAt,
		CreatedByUserID: actorUserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err == nil {
		s.audit(organizationID, actorUserID, "operations.incident.created", "operational_incident", fmt.Sprint(item.ID), map[string]any{
			"severity": severity, "started_at": startedAt,
		})
	}
	return item, err
}

func (s *OperationsService) Incidents(actorUserID, organizationID int64) ([]model.OperationalIncident, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return s.repo.ListOperationalIncidents(organizationID, "", now.Add(-365*24*time.Hour), now.Add(time.Minute))
}

func (s *OperationsService) UpdateIncident(actorUserID, organizationID, incidentID int64, req model.UpdateOperationalIncidentRequest) (model.OperationalIncident, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.OperationalIncident{}, err
	}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status != model.IncidentStatusOpen && status != model.IncidentStatusMonitoring && status != model.IncidentStatusResolved ||
		len(strings.TrimSpace(req.Summary)) > 8000 {
		return model.OperationalIncident{}, ErrInvalidOperationalIncident
	}
	now := time.Now().UTC()
	var resolvedAt *time.Time
	if status == model.IncidentStatusResolved {
		resolvedAt = &now
	}
	item, err := s.repo.UpdateOperationalIncident(
		organizationID, incidentID, status, strings.TrimSpace(req.Summary), resolvedAt, now,
	)
	if err == nil {
		s.audit(organizationID, actorUserID, "operations.incident.updated", "operational_incident", fmt.Sprint(incidentID), map[string]any{
			"status": status,
		})
	}
	return item, err
}

func (s *OperationsService) Evaluate(actorUserID, organizationID int64) ([]model.OperationalAlert, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	policy, err := s.policyOrDefault(organizationID, actorUserID, now)
	if err != nil {
		return nil, err
	}
	start, end := operationsMonth(now)
	costs, err := s.repo.ListCostAllocations(organizationID, start, end)
	if err != nil {
		return nil, err
	}
	currentSpend := allocatedSpend(costs, start, now)
	projectedSpend := projectPeriodValue(currentSpend, start, end, now)
	periodKey := start.Format("2006-01")

	if currentSpend*100 >= policy.MonthlyBudgetCents*int64(policy.BudgetAlertThresholdPercent) {
		if _, err := s.upsertAlert(model.OperationalAlert{
			OrganizationID: organizationID,
			Fingerprint:    "budget-threshold:" + periodKey,
			Type:           "budget_threshold",
			Metric:         "spend_cents",
			Message:        "current monthly spend reached the configured budget alert threshold",
			CurrentValue:   currentSpend,
			ThresholdValue: policy.MonthlyBudgetCents * int64(policy.BudgetAlertThresholdPercent) / 100,
			DetectedAt:     now,
			UpdatedAt:      now,
		}); err != nil {
			return nil, err
		}
	}
	if projectedSpend > policy.MonthlyBudgetCents {
		if _, err := s.upsertAlert(model.OperationalAlert{
			OrganizationID: organizationID,
			Fingerprint:    "budget-forecast:" + periodKey,
			Type:           "budget_forecast",
			Metric:         "projected_spend_cents",
			Message:        "projected monthly spend exceeds the configured budget",
			CurrentValue:   projectedSpend,
			ThresholdValue: policy.MonthlyBudgetCents,
			DetectedAt:     now,
			UpdatedAt:      now,
		}); err != nil {
			return nil, err
		}
	}

	forecasts, err := s.capacityForecasts(organizationID, now)
	if err != nil {
		return nil, err
	}
	for _, forecast := range forecasts {
		if forecast.Limit > 0 && forecast.ProjectedUtilization >= 80 {
			if _, err := s.upsertAlert(model.OperationalAlert{
				OrganizationID: organizationID,
				Fingerprint:    "capacity:" + forecast.Metric + ":" + periodKey,
				Type:           "capacity_forecast",
				Metric:         forecast.Metric,
				Message:        "projected usage has reached at least 80% of the active plan capacity",
				CurrentValue:   forecast.ProjectedPeriodValue,
				ThresholdValue: forecast.Limit * 80 / 100,
				DetectedAt:     now,
				UpdatedAt:      now,
			}); err != nil {
				return nil, err
			}
		}
	}

	slo, err := s.sloStatus(organizationID, policy, start, end, now)
	if err != nil {
		return nil, err
	}
	if !slo.WithinTarget {
		if _, err := s.upsertAlert(model.OperationalAlert{
			OrganizationID: organizationID,
			Fingerprint:    "slo:" + periodKey,
			Type:           "slo_breach",
			Metric:         "availability_basis_points",
			Message:        "availability is below the configured SLO target",
			CurrentValue:   int64(slo.AvailabilityBasisPoints),
			ThresholdValue: int64(slo.TargetBasisPoints),
			DetectedAt:     now,
			UpdatedAt:      now,
		}); err != nil {
			return nil, err
		}
	}

	incidents, err := s.repo.ListOperationalIncidents(organizationID, "", start.Add(-30*24*time.Hour), now.Add(time.Minute))
	if err != nil {
		return nil, err
	}
	for _, incident := range incidents {
		if incident.Status == model.IncidentStatusResolved {
			continue
		}
		escalateAt := incident.StartedAt.Add(time.Duration(policy.IncidentEscalationMinutes) * time.Minute)
		if !now.Before(escalateAt) {
			if _, err := s.upsertAlert(model.OperationalAlert{
				OrganizationID: organizationID,
				Fingerprint:    fmt.Sprintf("incident-escalation:%d", incident.ID),
				Type:           "incident_escalation",
				Metric:         "incident_age_minutes",
				Message:        "open incident exceeded the configured escalation threshold",
				CurrentValue:   int64(now.Sub(incident.StartedAt).Minutes()),
				ThresholdValue: int64(policy.IncidentEscalationMinutes),
				DetectedAt:     now,
				UpdatedAt:      now,
			}); err != nil {
				return nil, err
			}
		}
	}
	return s.repo.ListOperationalAlerts(organizationID, model.OperationalAlertOpen, 200)
}

func (s *OperationsService) Dashboard(actorUserID, organizationID int64) (model.OperationsDashboard, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.OperationsDashboard{}, err
	}
	now := time.Now().UTC()
	policy, err := s.policyOrDefault(organizationID, actorUserID, now)
	if err != nil {
		return model.OperationsDashboard{}, err
	}
	start, end := operationsMonth(now)
	costs, err := s.repo.ListCostAllocations(organizationID, start, end)
	if err != nil {
		return model.OperationsDashboard{}, err
	}
	currentSpend := allocatedSpend(costs, start, now)
	projectedSpend := projectPeriodValue(currentSpend, start, end, now)
	alerts, err := s.repo.ListOperationalAlerts(organizationID, model.OperationalAlertOpen, 100)
	if err != nil {
		return model.OperationsDashboard{}, err
	}
	incidents, err := s.repo.ListOperationalIncidents(organizationID, "", start.Add(-30*24*time.Hour), now.Add(time.Minute))
	if err != nil {
		return model.OperationsDashboard{}, err
	}
	openIncidents := make([]model.OperationalIncident, 0)
	for _, incident := range incidents {
		if incident.Status != model.IncidentStatusResolved {
			openIncidents = append(openIncidents, incident)
		}
	}
	windows, err := s.repo.ListMaintenanceWindows(organizationID, now, now.Add(30*24*time.Hour))
	if err != nil {
		return model.OperationsDashboard{}, err
	}
	upcoming := make([]model.MaintenanceWindow, 0)
	for _, window := range windows {
		if window.Status == model.MaintenanceStatusScheduled {
			upcoming = append(upcoming, window)
		}
	}
	slo, err := s.sloStatus(organizationID, policy, start, end, now)
	if err != nil {
		return model.OperationsDashboard{}, err
	}
	forecasts, err := s.capacityForecasts(organizationID, now)
	if err != nil {
		return model.OperationsDashboard{}, err
	}
	utilization := 0
	if policy.MonthlyBudgetCents > 0 {
		utilization = int(currentSpend * 100 / policy.MonthlyBudgetCents)
	}
	return model.OperationsDashboard{
		Policy:              policy,
		CurrentSpendCents:   currentSpend,
		ProjectedSpendCents: projectedSpend,
		BudgetUtilization:   utilization,
		OpenAlerts:          alerts,
		OpenIncidents:       openIncidents,
		UpcomingMaintenance: upcoming,
		SLO:                 slo,
		CapacityForecasts:   forecasts,
		GeneratedAt:         now,
	}, nil
}

func (s *OperationsService) capacityForecasts(organizationID int64, now time.Time) ([]model.CapacityForecast, error) {
	start, end := operationsMonth(now)
	usage, err := s.billing.ListBillingUsage(organizationID, start, end)
	if err != nil {
		return nil, err
	}
	limits, err := s.entitlements.EffectiveLimits(organizationID, now)
	if err != nil {
		return nil, err
	}
	currentAPI := int64(0)
	for _, item := range usage {
		if item.Metric == "api_operations" {
			currentAPI = item.Quantity
			break
		}
	}
	projectedAPI := projectPeriodValue(currentAPI, start, end, now)
	utilization := 0
	if limits.MonthlyAPIOperations > 0 {
		utilization = int(projectedAPI * 100 / limits.MonthlyAPIOperations)
	}
	return []model.CapacityForecast{{
		Metric:               "api_operations",
		CurrentValue:         currentAPI,
		ProjectedPeriodValue: projectedAPI,
		Limit:                limits.MonthlyAPIOperations,
		ProjectedUtilization: utilization,
	}}, nil
}

func (s *OperationsService) sloStatus(organizationID int64, policy model.OperationsPolicy, start, end, now time.Time) (model.SLOStatus, error) {
	effectiveEnd := end
	if now.Before(end) {
		effectiveEnd = now
	}
	if !effectiveEnd.After(start) {
		effectiveEnd = start.Add(time.Second)
	}
	incidents, err := s.repo.ListOperationalIncidents(organizationID, "", start, effectiveEnd)
	if err != nil {
		return model.SLOStatus{}, err
	}
	maintenance, err := s.repo.ListMaintenanceWindows(organizationID, start, effectiveEnd)
	if err != nil {
		return model.SLOStatus{}, err
	}
	incidentIntervals := make([]timeInterval, 0)
	for _, incident := range incidents {
		if incident.Severity != model.IncidentSeverity1 && incident.Severity != model.IncidentSeverity2 {
			continue
		}
		intervalEnd := effectiveEnd
		if incident.ResolvedAt != nil && incident.ResolvedAt.Before(intervalEnd) {
			intervalEnd = *incident.ResolvedAt
		}
		if intervalEnd.After(incident.StartedAt) {
			incidentIntervals = append(incidentIntervals, clippedInterval(incident.StartedAt, intervalEnd, start, effectiveEnd))
		}
	}
	maintenanceIntervals := make([]timeInterval, 0)
	for _, window := range maintenance {
		if window.Status == model.MaintenanceStatusCanceled {
			continue
		}
		maintenanceIntervals = append(maintenanceIntervals, clippedInterval(window.StartsAt, window.EndsAt, start, effectiveEnd))
	}
	incidentIntervals = mergeIntervals(incidentIntervals)
	maintenanceIntervals = mergeIntervals(maintenanceIntervals)
	downtime := durationOutsideMaintenance(incidentIntervals, maintenanceIntervals)
	eligible := effectiveEnd.Sub(start)
	if eligible <= 0 {
		eligible = time.Second
	}
	availability := 10000
	if downtime > 0 {
		availability = 10000 - int(downtime.Nanoseconds()*10000/eligible.Nanoseconds())
		if availability < 0 {
			availability = 0
		}
	}
	return model.SLOStatus{
		PeriodStart:             start,
		PeriodEnd:               effectiveEnd,
		TargetBasisPoints:       policy.SLOTargetBasisPoints,
		AvailabilityBasisPoints: availability,
		DowntimeSeconds:         int64(downtime.Seconds()),
		WithinTarget:            availability >= policy.SLOTargetBasisPoints,
	}, nil
}

func (s *OperationsService) policyOrDefault(organizationID, actorUserID int64, now time.Time) (model.OperationsPolicy, error) {
	item, err := s.repo.GetOperationsPolicy(organizationID)
	if err == nil {
		return item, nil
	}
	if !errors.Is(err, repository.ErrOperationsPolicyNotFound) {
		return model.OperationsPolicy{}, err
	}
	return model.OperationsPolicy{
		OrganizationID:              organizationID,
		MonthlyBudgetCents:          10000,
		BudgetAlertThresholdPercent: 80,
		SLOTargetBasisPoints:        9990,
		IncidentEscalationMinutes:   30,
		SupportTier:                 model.OperationsSupportStandard,
		UpdatedByUserID:             actorUserID,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	}, nil
}

func (s *OperationsService) upsertAlert(item model.OperationalAlert) (model.OperationalAlert, error) {
	item.Status = model.OperationalAlertOpen
	return s.repo.UpsertOperationalAlert(item)
}

func (s *OperationsService) requireAdmin(userID, organizationID int64) (model.OrganizationMember, error) {
	member, err := s.orgs.GetMember(organizationID, userID)
	if err != nil {
		return model.OrganizationMember{}, ErrOperationsForbidden
	}
	switch member.Role {
	case model.OrganizationRoleOwner, model.OrganizationRoleAdmin, model.OrganizationRoleDelegatedAdmin:
		return member, nil
	default:
		return model.OrganizationMember{}, ErrOperationsForbidden
	}
}

func (s *OperationsService) audit(organizationID, actorUserID int64, action, resourceType, resourceID string, metadata map[string]any) {
	actor := actorUserID
	_ = s.orgs.RecordAudit(model.OrganizationAuditEvent{
		OrganizationID: organizationID,
		ActorUserID:    &actor,
		Action:         action,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Metadata:       metadata,
		CreatedAt:      time.Now().UTC(),
	})
}

func operationsMonth(at time.Time) (time.Time, time.Time) {
	at = at.UTC()
	start := time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}

func allocatedSpend(items []model.CostAllocation, start, end time.Time) int64 {
	if !end.After(start) {
		return 0
	}
	var total int64
	for _, item := range items {
		overlapStart := maxTime(item.PeriodStart, start)
		overlapEnd := minTime(item.PeriodEnd, end)
		if !overlapEnd.After(overlapStart) {
			continue
		}
		totalDuration := item.PeriodEnd.Sub(item.PeriodStart)
		if totalDuration <= 0 {
			continue
		}
		overlap := overlapEnd.Sub(overlapStart)
		total += item.AmountCents * overlap.Nanoseconds() / totalDuration.Nanoseconds()
	}
	return total
}

func projectPeriodValue(current int64, start, end, now time.Time) int64 {
	if current <= 0 {
		return 0
	}
	effectiveNow := minTime(maxTime(now, start.Add(time.Second)), end)
	elapsed := effectiveNow.Sub(start)
	total := end.Sub(start)
	if elapsed <= 0 || total <= 0 {
		return current
	}
	projected := current * total.Nanoseconds() / elapsed.Nanoseconds()
	if projected < current {
		return current
	}
	return projected
}

func validSupportTier(value string) bool {
	switch value {
	case model.OperationsSupportStandard, model.OperationsSupportPriority, model.OperationsSupportEnterprise:
		return true
	default:
		return false
	}
}

func validIncidentSeverity(value string) bool {
	switch value {
	case model.IncidentSeverity1, model.IncidentSeverity2, model.IncidentSeverity3, model.IncidentSeverity4:
		return true
	default:
		return false
	}
}

type timeInterval struct {
	start time.Time
	end   time.Time
}

func clippedInterval(start, end, lower, upper time.Time) timeInterval {
	return timeInterval{start: maxTime(start, lower), end: minTime(end, upper)}
}

func mergeIntervals(items []timeInterval) []timeInterval {
	filtered := make([]timeInterval, 0, len(items))
	for _, item := range items {
		if item.end.After(item.start) {
			filtered = append(filtered, item)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].start.Before(filtered[j].start) })
	merged := make([]timeInterval, 0, len(filtered))
	for _, item := range filtered {
		if len(merged) == 0 || item.start.After(merged[len(merged)-1].end) {
			merged = append(merged, item)
			continue
		}
		if item.end.After(merged[len(merged)-1].end) {
			merged[len(merged)-1].end = item.end
		}
	}
	return merged
}

func durationOutsideMaintenance(incidents, maintenance []timeInterval) time.Duration {
	var total time.Duration
	for _, incident := range incidents {
		cursor := incident.start
		for _, window := range maintenance {
			if !window.end.After(cursor) || !window.start.Before(incident.end) {
				continue
			}
			if window.start.After(cursor) {
				total += minTime(window.start, incident.end).Sub(cursor)
			}
			if window.end.After(cursor) {
				cursor = window.end
			}
			if !cursor.Before(incident.end) {
				break
			}
		}
		if cursor.Before(incident.end) {
			total += incident.end.Sub(cursor)
		}
	}
	return total
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
