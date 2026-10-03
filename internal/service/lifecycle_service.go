package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrLifecycleUnavailable     = errors.New("data lifecycle service is unavailable")
	ErrPrivacyExportType        = errors.New("privacy request must be access or export")
	ErrPrivacyEraseType         = errors.New("privacy request must be a pending delete request")
	ErrPrivacyLegalHold         = errors.New("privacy erasure is blocked by an active legal hold")
	ErrInvalidConsent           = errors.New("invalid consent record")
)

type LifecycleService struct {
	repo       repository.LifecycleRepository
	governance repository.GovernanceRepository
	workspaces repository.WorkspaceRepository
	tasks      repository.TaskRepository
}

func NewLifecycleService(
	repo repository.LifecycleRepository,
	governance repository.GovernanceRepository,
	workspaces repository.WorkspaceRepository,
	tasks repository.TaskRepository,
) *LifecycleService {
	return &LifecycleService{repo: repo, governance: governance, workspaces: workspaces, tasks: tasks}
}

func (s *LifecycleService) ConfiguredWorkspaceIDs() ([]int64, error) {
	return s.repo.ListConfiguredWorkspaceIDs()
}

func (s *LifecycleService) Run(actorUserID, workspaceID int64) (model.LifecycleRun, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.LifecycleRun{}, err
	}
	return s.run(workspaceID, &actorUserID)
}

func (s *LifecycleService) RunSystem(workspaceID int64) (model.LifecycleRun, error) {
	return s.run(workspaceID, nil)
}

func (s *LifecycleService) run(workspaceID int64, actorUserID *int64) (model.LifecycleRun, error) {
	if s.tasks == nil {
		return model.LifecycleRun{}, ErrLifecycleUnavailable
	}
	now := time.Now()
	run, err := s.repo.CreateLifecycleRun(model.LifecycleRun{
		WorkspaceID: workspaceID, Status: model.LifecycleRunRunning,
		TriggeredByUserID: actorUserID, StartedAt: now,
	})
	if err != nil {
		return model.LifecycleRun{}, err
	}

	held, err := s.governance.HasActiveLegalHold(workspaceID)
	if err != nil {
		return s.failRun(run, err)
	}
	if held {
		return s.repo.FinishLifecycleRun(run.ID, model.LifecycleRunSkipped, 0, 0, "active legal hold", time.Now())
	}

	policy, err := s.governance.GetPolicy(workspaceID)
	if errors.Is(err, repository.ErrGovernancePolicyNotFound) {
		policy = defaultGovernancePolicy(workspaceID)
		err = nil
	}
	if err != nil {
		return s.failRun(run, err)
	}

	allTasks, err := s.collectTasks(workspaceID)
	if err != nil {
		return s.failRun(run, err)
	}
	cutoff := now.AddDate(0, 0, -policy.OperationalRetentionDays)
	archived := 0
	for _, task := range allTasks {
		if !task.CreatedAt.Before(cutoff) {
			continue
		}
		if err := s.repo.ArchiveTask(task, now); err != nil {
			return s.failRun(run, err)
		}
		if err := s.tasks.Delete(workspaceID, task.ID); err != nil {
			return s.failRun(run, err)
		}
		archived++
	}

	purgeCutoff := now.AddDate(0, 0, -policy.OperationalRetentionDays)
	purged, err := s.repo.PurgeArchivedTasks(workspaceID, purgeCutoff)
	if err != nil {
		return s.failRun(run, err)
	}

	finished, err := s.repo.FinishLifecycleRun(run.ID, model.LifecycleRunCompleted, archived, purged, "", time.Now())
	if err != nil {
		return model.LifecycleRun{}, err
	}

	s.auditSystem(workspaceID, actorUserID, "governance.lifecycle.completed", "lifecycle_run", strconv.FormatInt(finished.ID, 10), map[string]any{
		"archived_count": archived, "purged_count": purged, "retention_days": policy.OperationalRetentionDays,
	})
	if actorUserID != nil {
		_, _ = s.governance.CreateComplianceEvidence(model.ComplianceEvidence{
			WorkspaceID: workspaceID,
			Framework: "INTERNAL",
			Control: "DATA-LIFECYCLE",
			EvidenceType: "automated_run",
			Description: "Data lifecycle retention run completed",
			Metadata: map[string]any{"run_id": finished.ID, "archived_count": archived, "purged_count": purged},
			CreatedByUserID: *actorUserID,
			CreatedAt: time.Now(),
		})
	}
	return finished, nil
}

func (s *LifecycleService) failRun(run model.LifecycleRun, cause error) (model.LifecycleRun, error) {
	_, _ = s.repo.FinishLifecycleRun(run.ID, model.LifecycleRunFailed, run.ArchivedCount, run.PurgedCount, cause.Error(), time.Now())
	return model.LifecycleRun{}, cause
}

func (s *LifecycleService) ListRuns(actorUserID, workspaceID int64, limit int) ([]model.LifecycleRun, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	return s.repo.ListLifecycleRuns(workspaceID, limit)
}

func (s *LifecycleService) ExportPrivacyRequest(actorUserID, workspaceID, requestID int64) (model.PrivacyExportPackage, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.PrivacyExportPackage{}, err
	}
	request, err := s.findPrivacyRequest(workspaceID, requestID)
	if err != nil {
		return model.PrivacyExportPackage{}, err
	}
	if request.Type != model.PrivacyRequestAccess && request.Type != model.PrivacyRequestExport {
		return model.PrivacyExportPackage{}, ErrPrivacyExportType
	}
	tasks, err := s.collectSubjectTasks(workspaceID, request.SubjectUserID)
	if err != nil {
		return model.PrivacyExportPackage{}, err
	}
	generatedAt := time.Now()
	payload := map[string]any{
		"privacy_request": request,
		"subject_user_id": request.SubjectUserID,
		"tasks": tasks,
		"generated_at": generatedAt,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return model.PrivacyExportPackage{}, err
	}
	sum := sha256.Sum256(raw)
	pkg, err := s.repo.SavePrivacyExport(model.PrivacyExportPackage{
		WorkspaceID: workspaceID, PrivacyRequestID: request.ID, SubjectUserID: request.SubjectUserID,
		ChecksumSHA256: hex.EncodeToString(sum[:]), Payload: payload,
		CreatedByUserID: actorUserID, CreatedAt: generatedAt,
	})
	if err == nil {
		s.auditSystem(workspaceID, &actorUserID, "governance.privacy_export.created", "privacy_export", strconv.FormatInt(pkg.ID, 10), map[string]any{
			"privacy_request_id": request.ID, "checksum_sha256": pkg.ChecksumSHA256,
		})
	}
	return pkg, err
}

func (s *LifecycleService) ErasePrivacyRequest(actorUserID, workspaceID, requestID int64) (int, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return 0, err
	}
	request, err := s.findPrivacyRequest(workspaceID, requestID)
	if err != nil {
		return 0, err
	}
	if request.Type != model.PrivacyRequestDelete || request.Status != model.PrivacyStatusPending {
		return 0, ErrPrivacyEraseType
	}
	held, err := s.governance.HasActiveLegalHold(workspaceID)
	if err != nil {
		return 0, err
	}
	if held {
		return 0, ErrPrivacyLegalHold
	}
	tasks, err := s.collectSubjectTasks(workspaceID, request.SubjectUserID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, task := range tasks {
		task.Title = "[anonymized]"
		task.Description = ""
		task.UpdatedAt = time.Now()
		if _, err := s.tasks.Update(task); err != nil {
			return count, err
		}
		count++
	}
	if _, err := s.governance.CompletePrivacyRequest(
		workspaceID, requestID, actorUserID, model.PrivacyStatusCompleted,
		fmt.Sprintf("content anonymized for %d task(s)", count), time.Now(),
	); err != nil {
		return count, err
	}
	s.auditSystem(workspaceID, &actorUserID, "governance.privacy_delete.executed", "privacy_request", strconv.FormatInt(requestID, 10), map[string]any{
		"anonymized_tasks": count,
	})
	return count, nil
}

func (s *LifecycleService) AddConsent(actorUserID, workspaceID int64, req model.CreateConsentRequest) (model.ConsentRecord, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.ConsentRecord{}, err
	}
	purpose := strings.ToLower(strings.TrimSpace(req.Purpose))
	status := strings.ToLower(strings.TrimSpace(req.Status))
	version := strings.TrimSpace(req.PolicyVersion)
	source := strings.ToLower(strings.TrimSpace(req.Source))
	if req.SubjectUserID <= 0 || purpose == "" || version == "" || source == "" ||
		(status != model.ConsentGranted && status != model.ConsentWithdrawn) {
		return model.ConsentRecord{}, ErrInvalidConsent
	}
	if _, err := s.workspaces.ResolveAccess(req.SubjectUserID, workspaceID, time.Now()); err != nil {
		return model.ConsentRecord{}, ErrInvalidConsent
	}
	item, err := s.repo.AddConsent(model.ConsentRecord{
		WorkspaceID: workspaceID, SubjectUserID: req.SubjectUserID, Purpose: purpose,
		Status: status, PolicyVersion: version, Source: source,
		RecordedByUserID: actorUserID, RecordedAt: time.Now(),
	})
	if err == nil {
		s.auditSystem(workspaceID, &actorUserID, "governance.consent.recorded", "consent", strconv.FormatInt(item.ID, 10), map[string]any{
			"subject_user_id": req.SubjectUserID, "purpose": purpose, "status": status,
		})
	}
	return item, err
}

func (s *LifecycleService) ListConsents(actorUserID, workspaceID int64) ([]model.ConsentRecord, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListConsents(workspaceID)
}

func (s *LifecycleService) Report(actorUserID, workspaceID int64) (model.GovernanceReport, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.GovernanceReport{}, err
	}
	policy, err := s.governance.GetPolicy(workspaceID)
	if errors.Is(err, repository.ErrGovernancePolicyNotFound) {
		policy = defaultGovernancePolicy(workspaceID)
		err = nil
	}
	if err != nil {
		return model.GovernanceReport{}, err
	}
	inventory, err := s.governance.ListDataInventory(workspaceID)
	if err != nil {
		return model.GovernanceReport{}, err
	}
	holds, err := s.governance.ListLegalHolds(workspaceID, true)
	if err != nil {
		return model.GovernanceReport{}, err
	}
	privacy, err := s.governance.ListPrivacyRequests(workspaceID)
	if err != nil {
		return model.GovernanceReport{}, err
	}
	consents, err := s.repo.ListConsents(workspaceID)
	if err != nil {
		return model.GovernanceReport{}, err
	}
	runs, err := s.repo.ListLifecycleRuns(workspaceID, 1)
	if err != nil {
		return model.GovernanceReport{}, err
	}
	now := time.Now()
	report := model.GovernanceReport{
		WorkspaceID: workspaceID,
		DefaultClassification: policy.DefaultClassification,
		OperationalRetentionDays: policy.OperationalRetentionDays,
		DataInventoryEntries: len(inventory),
		ActiveLegalHolds: len(holds),
		ConsentRecords: len(consents),
		GeneratedAt: now,
	}
	for _, entry := range inventory {
		if entry.ContainsPersonalData {
			report.PersonalDataEntries++
		}
	}
	for _, request := range privacy {
		if request.Status == model.PrivacyStatusPending {
			report.PendingPrivacyRequests++
			if request.DueAt.Before(now) {
				report.OverduePrivacyRequests++
			}
		}
	}
	if len(runs) > 0 {
		latest := runs[0]
		report.LatestLifecycleRun = &latest
	}
	return report, nil
}

func (s *LifecycleService) requireAdmin(userID, workspaceID int64) error {
	access, err := s.workspaces.ResolveAccess(userID, workspaceID, time.Now())
	if err != nil {
		return err
	}
	if access.Role != model.WorkspaceRoleOwner && access.Role != model.WorkspaceRoleAdmin {
		return ErrGovernanceAccessDenied
	}
	return nil
}

func (s *LifecycleService) findPrivacyRequest(workspaceID, requestID int64) (model.PrivacyRequest, error) {
	items, err := s.governance.ListPrivacyRequests(workspaceID)
	if err != nil {
		return model.PrivacyRequest{}, err
	}
	for _, item := range items {
		if item.ID == requestID {
			return item, nil
		}
	}
	return model.PrivacyRequest{}, repository.ErrPrivacyRequestNotFound
}

func (s *LifecycleService) collectTasks(workspaceID int64) ([]model.Task, error) {
	items := make([]model.Task, 0)
	for page := 1; ; page++ {
		result, err := s.tasks.FindAll(workspaceID, model.TaskQuery{
			Page: page, Limit: 100, Sort: "created_at", Order: "asc",
		})
		if err != nil {
			return nil, err
		}
		items = append(items, result.Items...)
		if page >= result.Pagination.TotalPages || len(result.Items) == 0 {
			break
		}
	}
	return items, nil
}

func (s *LifecycleService) collectSubjectTasks(workspaceID, subjectUserID int64) ([]model.Task, error) {
	items, err := s.collectTasks(workspaceID)
	if err != nil {
		return nil, err
	}
	filtered := make([]model.Task, 0)
	for _, task := range items {
		if task.UserID == subjectUserID {
			filtered = append(filtered, task)
		}
	}
	return filtered, nil
}

func (s *LifecycleService) auditSystem(workspaceID int64, actorUserID *int64, action, resourceType, resourceID string, metadata map[string]any) {
	wid := workspaceID
	_ = s.workspaces.RecordAudit(model.AuditEvent{
		WorkspaceID: &wid, ActorUserID: actorUserID, Action: action, ResourceType: resourceType,
		ResourceID: resourceID, Metadata: metadata, CreatedAt: time.Now(),
	})
}
