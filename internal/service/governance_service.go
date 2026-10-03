package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrGovernanceAccessDenied    = errors.New("governance administration requires workspace owner or admin")
	ErrInvalidGovernancePolicy   = errors.New("invalid governance policy")
	ErrInvalidDataInventory      = errors.New("invalid data inventory entry")
	ErrInvalidLegalHold          = errors.New("invalid legal hold")
	ErrInvalidPrivacyRequest     = errors.New("invalid privacy request")
	ErrInvalidComplianceEvidence = errors.New("invalid compliance evidence")
)

type GovernanceService struct {
	repo       repository.GovernanceRepository
	workspaces repository.WorkspaceRepository
}

func NewGovernanceService(repo repository.GovernanceRepository, workspaces repository.WorkspaceRepository) *GovernanceService {
	return &GovernanceService{repo: repo, workspaces: workspaces}
}

func defaultGovernancePolicy(workspaceID int64) model.GovernancePolicy {
	return model.GovernancePolicy{
		WorkspaceID:              workspaceID,
		DefaultClassification:    model.DataClassificationInternal,
		AuditRetentionDays:       365,
		OperationalRetentionDays: 365,
		PrivacyRequestSLAHours:   720,
		AllowedDataRegions:       []string{},
	}
}

func (s *GovernanceService) GetPolicy(actorUserID, workspaceID int64) (model.GovernancePolicy, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.GovernancePolicy{}, err
	}
	item, err := s.repo.GetPolicy(workspaceID)
	if errors.Is(err, repository.ErrGovernancePolicyNotFound) {
		return defaultGovernancePolicy(workspaceID), nil
	}
	return item, err
}

func (s *GovernanceService) UpdatePolicy(actorUserID, workspaceID int64, req model.UpdateGovernancePolicyRequest) (model.GovernancePolicy, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.GovernancePolicy{}, err
	}
	classification := normalizeClassification(req.DefaultClassification)
	regions := normalizeGovernanceStrings(req.AllowedDataRegions)
	if classification == "" || req.AuditRetentionDays < 30 || req.AuditRetentionDays > 3650 ||
		req.OperationalRetentionDays < 1 || req.OperationalRetentionDays > 3650 ||
		req.PrivacyRequestSLAHours < 1 || req.PrivacyRequestSLAHours > 2160 ||
		(req.RestrictCrossRegionTransfer && len(regions) == 0) {
		return model.GovernancePolicy{}, ErrInvalidGovernancePolicy
	}
	now := time.Now()
	item, err := s.repo.UpsertPolicy(model.GovernancePolicy{
		WorkspaceID:                 workspaceID,
		DefaultClassification:       classification,
		AuditRetentionDays:          req.AuditRetentionDays,
		OperationalRetentionDays:    req.OperationalRetentionDays,
		PrivacyRequestSLAHours:      req.PrivacyRequestSLAHours,
		RequireDPA:                  req.RequireDPA,
		RestrictCrossRegionTransfer: req.RestrictCrossRegionTransfer,
		AllowedDataRegions:          regions,
		UpdatedByUserID:             actorUserID,
		UpdatedAt:                   now,
	})
	if err == nil {
		s.audit(workspaceID, actorUserID, "governance.policy.updated", "governance_policy", strconv.FormatInt(workspaceID, 10), map[string]any{
			"default_classification":     classification,
			"audit_retention_days":       req.AuditRetentionDays,
			"operational_retention_days": req.OperationalRetentionDays,
			"privacy_request_sla_hours":  req.PrivacyRequestSLAHours,
		})
	}
	return item, err
}

func (s *GovernanceService) UpsertDataInventory(actorUserID, workspaceID int64, req model.UpsertDataInventoryRequest) (model.DataInventoryEntry, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.DataInventoryEntry{}, err
	}
	resourceType := strings.ToLower(strings.TrimSpace(req.ResourceType))
	fieldName := strings.ToLower(strings.TrimSpace(req.FieldName))
	classification := normalizeClassification(req.Classification)
	region := strings.ToLower(strings.TrimSpace(req.DataRegion))
	if resourceType == "" || fieldName == "" || classification == "" || req.RetentionDays < 1 || req.RetentionDays > 3650 {
		return model.DataInventoryEntry{}, ErrInvalidDataInventory
	}
	item, err := s.repo.UpsertDataInventory(model.DataInventoryEntry{
		WorkspaceID:          workspaceID,
		ResourceType:         resourceType,
		FieldName:            fieldName,
		Classification:       classification,
		ContainsPersonalData: req.ContainsPersonalData,
		DataRegion:           region,
		RetentionDays:        req.RetentionDays,
		UpdatedByUserID:      actorUserID,
		UpdatedAt:            time.Now(),
	})
	if err == nil {
		s.audit(workspaceID, actorUserID, "governance.data_inventory.upserted", "data_inventory", fmt.Sprintf("%s.%s", resourceType, fieldName), map[string]any{
			"classification": classification, "contains_personal_data": req.ContainsPersonalData,
		})
	}
	return item, err
}

func (s *GovernanceService) ListDataInventory(actorUserID, workspaceID int64) ([]model.DataInventoryEntry, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListDataInventory(workspaceID)
}

func (s *GovernanceService) CreateLegalHold(actorUserID, workspaceID int64, req model.CreateLegalHoldRequest) (model.LegalHold, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.LegalHold{}, err
	}
	name := strings.TrimSpace(req.Name)
	reason := strings.TrimSpace(req.Reason)
	resourceType := strings.ToLower(strings.TrimSpace(req.ResourceType))
	resourceID := strings.TrimSpace(req.ResourceID)
	if name == "" || reason == "" || resourceType == "" {
		return model.LegalHold{}, ErrInvalidLegalHold
	}
	item, err := s.repo.CreateLegalHold(model.LegalHold{
		WorkspaceID: workspaceID, Name: name, Reason: reason, ResourceType: resourceType,
		ResourceID: resourceID, CreatedByUserID: actorUserID, CreatedAt: time.Now(),
	})
	if err == nil {
		s.audit(workspaceID, actorUserID, "governance.legal_hold.created", "legal_hold", strconv.FormatInt(item.ID, 10), map[string]any{
			"name": name, "resource_type": resourceType, "resource_id": resourceID,
		})
	}
	return item, err
}

func (s *GovernanceService) ListLegalHolds(actorUserID, workspaceID int64, activeOnly bool) ([]model.LegalHold, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListLegalHolds(workspaceID, activeOnly)
}

func (s *GovernanceService) ReleaseLegalHold(actorUserID, workspaceID, holdID int64) error {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return err
	}
	if err := s.repo.ReleaseLegalHold(workspaceID, holdID, actorUserID, time.Now()); err != nil {
		return err
	}
	s.audit(workspaceID, actorUserID, "governance.legal_hold.released", "legal_hold", strconv.FormatInt(holdID, 10), nil)
	return nil
}

func (s *GovernanceService) HasActiveLegalHold(workspaceID int64) (bool, error) {
	return s.repo.HasActiveLegalHold(workspaceID)
}

func (s *GovernanceService) CreatePrivacyRequest(actorUserID, workspaceID int64, req model.CreatePrivacyRequestRequest) (model.PrivacyRequest, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.PrivacyRequest{}, err
	}
	requestType := strings.ToLower(strings.TrimSpace(req.Type))
	if req.SubjectUserID <= 0 || !validPrivacyType(requestType) {
		return model.PrivacyRequest{}, ErrInvalidPrivacyRequest
	}
	if _, err := s.workspaces.ResolveAccess(req.SubjectUserID, workspaceID, time.Now()); err != nil {
		return model.PrivacyRequest{}, ErrInvalidPrivacyRequest
	}
	policy, err := s.repo.GetPolicy(workspaceID)
	if errors.Is(err, repository.ErrGovernancePolicyNotFound) {
		policy = defaultGovernancePolicy(workspaceID)
		err = nil
	}
	if err != nil {
		return model.PrivacyRequest{}, err
	}
	now := time.Now()
	item, err := s.repo.CreatePrivacyRequest(model.PrivacyRequest{
		WorkspaceID: workspaceID, SubjectUserID: req.SubjectUserID, Type: requestType,
		Status: model.PrivacyStatusPending, Reason: strings.TrimSpace(req.Reason),
		RequestedByUserID: actorUserID, RequestedAt: now,
		DueAt: now.Add(time.Duration(policy.PrivacyRequestSLAHours) * time.Hour),
	})
	if err == nil {
		s.audit(workspaceID, actorUserID, "governance.privacy_request.created", "privacy_request", strconv.FormatInt(item.ID, 10), map[string]any{
			"type": requestType, "subject_user_id": req.SubjectUserID, "due_at": item.DueAt,
		})
	}
	return item, err
}

func (s *GovernanceService) ListPrivacyRequests(actorUserID, workspaceID int64) ([]model.PrivacyRequest, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListPrivacyRequests(workspaceID)
}

func (s *GovernanceService) CompletePrivacyRequest(actorUserID, workspaceID, requestID int64, req model.CompletePrivacyRequestRequest) (model.PrivacyRequest, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.PrivacyRequest{}, err
	}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	reason := strings.TrimSpace(req.Reason)
	if status != model.PrivacyStatusCompleted && status != model.PrivacyStatusRejected {
		return model.PrivacyRequest{}, ErrInvalidPrivacyRequest
	}
	if status == model.PrivacyStatusRejected && reason == "" {
		return model.PrivacyRequest{}, ErrInvalidPrivacyRequest
	}
	item, err := s.repo.CompletePrivacyRequest(workspaceID, requestID, actorUserID, status, reason, time.Now())
	if err == nil {
		s.audit(workspaceID, actorUserID, "governance.privacy_request.completed", "privacy_request", strconv.FormatInt(requestID, 10), map[string]any{"status": status})
	}
	return item, err
}

func (s *GovernanceService) CreateComplianceEvidence(actorUserID, workspaceID int64, req model.CreateComplianceEvidenceRequest) (model.ComplianceEvidence, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.ComplianceEvidence{}, err
	}
	framework := strings.ToUpper(strings.TrimSpace(req.Framework))
	control := strings.TrimSpace(req.Control)
	evidenceType := strings.ToLower(strings.TrimSpace(req.EvidenceType))
	description := strings.TrimSpace(req.Description)
	if framework == "" || control == "" || evidenceType == "" || description == "" {
		return model.ComplianceEvidence{}, ErrInvalidComplianceEvidence
	}
	item, err := s.repo.CreateComplianceEvidence(model.ComplianceEvidence{
		WorkspaceID: workspaceID, Framework: framework, Control: control, EvidenceType: evidenceType,
		Description: description, Metadata: req.Metadata, CreatedByUserID: actorUserID, CreatedAt: time.Now(),
	})
	if err == nil {
		s.audit(workspaceID, actorUserID, "governance.compliance_evidence.created", "compliance_evidence", strconv.FormatInt(item.ID, 10), map[string]any{
			"framework": framework, "control": control, "evidence_type": evidenceType,
		})
	}
	return item, err
}

func (s *GovernanceService) ListComplianceEvidence(actorUserID, workspaceID int64, limit int) ([]model.ComplianceEvidence, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	return s.repo.ListComplianceEvidence(workspaceID, limit)
}

func (s *GovernanceService) requireAdmin(userID, workspaceID int64) error {
	access, err := s.workspaces.ResolveAccess(userID, workspaceID, time.Now())
	if err != nil {
		return err
	}
	if access.Role != model.WorkspaceRoleOwner && access.Role != model.WorkspaceRoleAdmin {
		return ErrGovernanceAccessDenied
	}
	return nil
}

func (s *GovernanceService) audit(workspaceID, actorUserID int64, action, resourceType, resourceID string, metadata map[string]any) {
	wid := workspaceID
	uid := actorUserID
	_ = s.workspaces.RecordAudit(model.AuditEvent{
		WorkspaceID: &wid, ActorUserID: &uid, Action: action, ResourceType: resourceType,
		ResourceID: resourceID, Metadata: metadata, CreatedAt: time.Now(),
	})
}

func normalizeClassification(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case model.DataClassificationPublic:
		return model.DataClassificationPublic
	case model.DataClassificationInternal:
		return model.DataClassificationInternal
	case model.DataClassificationConfidential:
		return model.DataClassificationConfidential
	case model.DataClassificationRestricted:
		return model.DataClassificationRestricted
	default:
		return ""
	}
}

func validPrivacyType(value string) bool {
	return value == model.PrivacyRequestAccess || value == model.PrivacyRequestExport ||
		value == model.PrivacyRequestDelete || value == model.PrivacyRequestCorrect
}

func normalizeGovernanceStrings(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
