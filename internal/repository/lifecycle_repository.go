package repository

import (
	"errors"
	"sort"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var ErrLifecycleRunNotFound = errors.New("lifecycle run not found")

type LifecycleRepository interface {
	ListConfiguredWorkspaceIDs() ([]int64, error)
	CreateLifecycleRun(run model.LifecycleRun) (model.LifecycleRun, error)
	FinishLifecycleRun(runID int64, status string, archived, purged int, skippedReason string, finishedAt time.Time) (model.LifecycleRun, error)
	ListLifecycleRuns(workspaceID int64, limit int) ([]model.LifecycleRun, error)
	ArchiveTask(task model.Task, archivedAt time.Time) error
	PurgeArchivedTasks(workspaceID int64, cutoff time.Time) (int, error)
	SavePrivacyExport(pkg model.PrivacyExportPackage) (model.PrivacyExportPackage, error)
	AddConsent(record model.ConsentRecord) (model.ConsentRecord, error)
	ListConsents(workspaceID int64) ([]model.ConsentRecord, error)
}

type InMemoryLifecycleRepository struct {
	mu          sync.Mutex
	configured  map[int64]bool
	runs        map[int64]model.LifecycleRun
	archives    map[int64]map[int64]time.Time
	exports     map[int64]model.PrivacyExportPackage
	consents    map[int64]model.ConsentRecord
	nextRun     int64
	nextExport  int64
	nextConsent int64
}

func NewInMemoryLifecycleRepository() *InMemoryLifecycleRepository {
	return &InMemoryLifecycleRepository{
		configured:  make(map[int64]bool),
		runs:        make(map[int64]model.LifecycleRun),
		archives:    make(map[int64]map[int64]time.Time),
		exports:     make(map[int64]model.PrivacyExportPackage),
		consents:    make(map[int64]model.ConsentRecord),
		nextRun:     1,
		nextExport:  1,
		nextConsent: 1,
	}
}

func (r *InMemoryLifecycleRepository) ConfigureWorkspace(workspaceID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configured[workspaceID] = true
}

func (r *InMemoryLifecycleRepository) ListConfiguredWorkspaceIDs() ([]int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]int64, 0, len(r.configured))
	for id := range r.configured {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func (r *InMemoryLifecycleRepository) CreateLifecycleRun(run model.LifecycleRun) (model.LifecycleRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run.ID = r.nextRun
	r.nextRun++
	r.runs[run.ID] = run
	return run, nil
}

func (r *InMemoryLifecycleRepository) FinishLifecycleRun(runID int64, status string, archived, purged int, skippedReason string, finishedAt time.Time) (model.LifecycleRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[runID]
	if !ok {
		return model.LifecycleRun{}, ErrLifecycleRunNotFound
	}
	run.Status = status
	run.ArchivedCount = archived
	run.PurgedCount = purged
	run.SkippedReason = skippedReason
	run.FinishedAt = &finishedAt
	r.runs[runID] = run
	return run, nil
}

func (r *InMemoryLifecycleRepository) ListLifecycleRuns(workspaceID int64, limit int) ([]model.LifecycleRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.LifecycleRun, 0)
	for _, run := range r.runs {
		if run.WorkspaceID == workspaceID {
			items = append(items, run)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].StartedAt.After(items[j].StartedAt) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryLifecycleRepository) ArchiveTask(task model.Task, archivedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.archives[task.WorkspaceID] == nil {
		r.archives[task.WorkspaceID] = make(map[int64]time.Time)
	}
	if _, exists := r.archives[task.WorkspaceID][task.ID]; !exists {
		r.archives[task.WorkspaceID][task.ID] = archivedAt
	}
	return nil
}

func (r *InMemoryLifecycleRepository) PurgeArchivedTasks(workspaceID int64, cutoff time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for id, archivedAt := range r.archives[workspaceID] {
		if archivedAt.Before(cutoff) {
			delete(r.archives[workspaceID], id)
			count++
		}
	}
	return count, nil
}

func (r *InMemoryLifecycleRepository) SavePrivacyExport(pkg model.PrivacyExportPackage) (model.PrivacyExportPackage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pkg.ID = r.nextExport
	r.nextExport++
	r.exports[pkg.ID] = pkg
	return pkg, nil
}

func (r *InMemoryLifecycleRepository) AddConsent(record model.ConsentRecord) (model.ConsentRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record.ID = r.nextConsent
	r.nextConsent++
	r.consents[record.ID] = record
	return record, nil
}

func (r *InMemoryLifecycleRepository) ListConsents(workspaceID int64) ([]model.ConsentRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.ConsentRecord, 0)
	for _, record := range r.consents {
		if record.WorkspaceID == workspaceID {
			items = append(items, record)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].RecordedAt.After(items[j].RecordedAt) })
	return items, nil
}
