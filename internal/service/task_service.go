package service

import (
	"errors"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrInvalidTask      = errors.New("invalid task")
	ErrInvalidTaskQuery = errors.New("invalid task query")
)

type TaskService struct {
	repo   repository.TaskRepository
	collab *TaskCollaborationService
}

func NewTaskService(repo repository.TaskRepository) *TaskService {
	return &TaskService{repo: repo}
}

func (s *TaskService) SetCollaborationService(collab *TaskCollaborationService) {
	s.collab = collab
}

func (s *TaskService) Create(userID int64, access model.WorkspaceAccess, req model.CreateTaskRequest) (model.Task, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return model.Task{}, ErrInvalidTask
	}
	status := normalizeTaskStatus(req.Status)
	priority := normalizeTaskPriority(req.Priority)
	if !validTaskStatus(status) || status == model.TaskStatusArchived || !validTaskPriority(priority) ||
		!validTaskDates(req.StartAt, req.DueAt) || !validMinutes(req.EstimatedMinutes) {
		return model.Task{}, ErrInvalidTask
	}
	if s.collab != nil {
		if err := s.collab.ValidateTaskReferences(access.ID, req.ProjectID, req.ListID, req.ParentTaskID); err != nil {
			return model.Task{}, err
		}
	}

	now := time.Now().UTC()
	completed := status == model.TaskStatusDone
	var completedAt *time.Time
	if completed {
		completedAt = &now
	}
	task := model.Task{
		WorkspaceID: access.ID, UserID: userID, PersonalWorkspace: access.IsPersonal,
		Title: title, Description: strings.TrimSpace(req.Description),
		Completed: completed, Status: status, Priority: priority,
		StartAt: req.StartAt, DueAt: req.DueAt, CompletedAt: completedAt,
		ProjectID: req.ProjectID, ListID: req.ListID, ParentTaskID: req.ParentTaskID,
		Position: req.Position, EstimatedMinutes: req.EstimatedMinutes,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	created, err := s.repo.Create(task)
	if err != nil {
		return model.Task{}, err
	}
	if s.collab != nil {
		_ = s.collab.RecordTaskMutation(userID, access.ID, created.ID, "task.created", "", nil, nil)
	}
	return created, nil
}

func (s *TaskService) FindAll(workspaceID int64, query model.TaskQuery) (model.TaskPage, error) {
	normalized, err := normalizeTaskQuery(query)
	if err != nil {
		return model.TaskPage{}, err
	}
	return s.repo.FindAll(workspaceID, normalized)
}

func normalizeTaskQuery(query model.TaskQuery) (model.TaskQuery, error) {
	if query.Page == 0 {
		query.Page = 1
	}
	if query.Limit == 0 {
		query.Limit = 10
	}
	if query.Page < 1 || query.Limit < 1 || query.Limit > 100 {
		return model.TaskQuery{}, ErrInvalidTaskQuery
	}

	query.Search = strings.TrimSpace(query.Search)
	query.Status = strings.ToUpper(strings.TrimSpace(query.Status))
	query.Priority = strings.ToUpper(strings.TrimSpace(query.Priority))
	query.Sort = strings.ToLower(strings.TrimSpace(query.Sort))
	query.Order = strings.ToLower(strings.TrimSpace(query.Order))
	if query.Sort == "" {
		query.Sort = "created_at"
	}
	if query.Order == "" {
		query.Order = "desc"
	}
	if query.Status != "" && !validTaskStatus(query.Status) {
		return model.TaskQuery{}, ErrInvalidTaskQuery
	}
	if query.Priority != "" && !validTaskPriority(query.Priority) {
		return model.TaskQuery{}, ErrInvalidTaskQuery
	}
	allowedSort := map[string]bool{
		"id": true, "title": true, "created_at": true, "updated_at": true,
		"completed": true, "status": true, "priority": true, "due_at": true, "position": true,
	}
	if !allowedSort[query.Sort] || (query.Order != "asc" && query.Order != "desc") {
		return model.TaskQuery{}, ErrInvalidTaskQuery
	}
	return query, nil
}

func (s *TaskService) FindByID(workspaceID, id int64) (model.Task, error) {
	task, err := s.repo.FindByID(workspaceID, id)
	if err != nil {
		return model.Task{}, err
	}
	if task.DeletedAt != nil {
		return model.Task{}, repository.ErrTaskNotFound
	}
	return task, nil
}

func (s *TaskService) Update(workspaceID, id int64, actorUserID int64, req model.UpdateTaskRequest) (model.Task, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return model.Task{}, ErrInvalidTask
	}
	task, err := s.repo.FindByID(workspaceID, id)
	if err != nil {
		return model.Task{}, err
	}
	if task.DeletedAt != nil {
		return model.Task{}, repository.ErrTaskNotFound
	}
	before := task

	status := task.Status
	if strings.TrimSpace(req.Status) != "" {
		status = normalizeTaskStatus(req.Status)
	}
	priority := task.Priority
	if strings.TrimSpace(req.Priority) != "" {
		priority = normalizeTaskPriority(req.Priority)
	}
	if !validTaskStatus(status) || !validTaskPriority(priority) ||
		!validTaskDates(req.StartAt, req.DueAt) ||
		!validMinutes(req.EstimatedMinutes) || !validMinutes(req.ActualMinutes) {
		return model.Task{}, ErrInvalidTask
	}
	if s.collab != nil {
		if err := s.collab.ValidateTaskReferences(workspaceID, req.ProjectID, req.ListID, req.ParentTaskID); err != nil {
			return model.Task{}, err
		}
	}
	if req.ParentTaskID != nil && *req.ParentTaskID == id {
		return model.Task{}, ErrInvalidTask
	}

	task.Title = title
	task.Description = strings.TrimSpace(req.Description)
	task.Status = status
	task.Priority = priority
	task.StartAt = req.StartAt
	task.DueAt = req.DueAt
	task.ProjectID = req.ProjectID
	task.ListID = req.ListID
	task.ParentTaskID = req.ParentTaskID
	task.Position = req.Position
	task.EstimatedMinutes = req.EstimatedMinutes
	task.ActualMinutes = req.ActualMinutes
	task.Completed = status == model.TaskStatusDone
	if task.Completed && !before.Completed {
		now := time.Now().UTC()
		task.CompletedAt = &now
	} else if !task.Completed {
		task.CompletedAt = nil
	}
	if status == model.TaskStatusArchived {
		now := time.Now().UTC()
		task.ArchivedAt = &now
	} else if before.Status == model.TaskStatusArchived {
		task.ArchivedAt = nil
	}
	if req.Version > 0 {
		task.Version = req.Version
	}
	task.UpdatedAt = time.Now().UTC()
	updated, err := s.repo.Update(task)
	if err != nil {
		return model.Task{}, err
	}
	s.recordTaskChanges(actorUserID, before, updated)
	return updated, nil
}

func (s *TaskService) Complete(workspaceID, id, actorUserID int64) (model.Task, error) {
	task, err := s.repo.FindByID(workspaceID, id)
	if err != nil {
		return model.Task{}, err
	}
	if task.DeletedAt != nil {
		return model.Task{}, repository.ErrTaskNotFound
	}
	before := task
	now := time.Now().UTC()
	task.Completed = true
	task.Status = model.TaskStatusDone
	task.CompletedAt = &now
	task.UpdatedAt = now
	updated, err := s.repo.Update(task)
	if err != nil {
		return model.Task{}, err
	}
	s.recordTaskChanges(actorUserID, before, updated)
	return updated, nil
}

func (s *TaskService) Archive(workspaceID, id, actorUserID int64) (model.Task, error) {
	item, err := s.repo.Archive(workspaceID, id, true, time.Now().UTC())
	if err != nil {
		return model.Task{}, err
	}
	if s.collab != nil {
		_ = s.collab.RecordTaskMutation(actorUserID, workspaceID, id, "task.archived", model.EventTaskArchived, nil, item)
	}
	return item, nil
}

func (s *TaskService) Restore(workspaceID, id, actorUserID int64) (model.Task, error) {
	item, err := s.repo.Restore(workspaceID, id, time.Now().UTC())
	if err != nil {
		return model.Task{}, err
	}
	if s.collab != nil {
		_ = s.collab.RecordTaskMutation(actorUserID, workspaceID, id, "task.restored", model.EventTaskRestored, nil, item)
	}
	return item, nil
}

func (s *TaskService) Delete(workspaceID, id, actorUserID int64) error {
	item, err := s.repo.SoftDelete(workspaceID, id, time.Now().UTC())
	if err != nil {
		return err
	}
	if s.collab != nil {
		_ = s.collab.RecordTaskMutation(actorUserID, workspaceID, id, "task.soft_deleted", model.EventTaskDeleted, nil, item)
	}
	return nil
}

func (s *TaskService) recordTaskChanges(actorUserID int64, before, after model.Task) {
	if s.collab == nil {
		return
	}
	_ = s.collab.RecordTaskMutation(actorUserID, after.WorkspaceID, after.ID, "task.updated", "", taskMutationMetadata(before, after), nil)
	if before.Status != after.Status {
		_ = s.collab.RecordTaskMutation(actorUserID, after.WorkspaceID, after.ID, "task.status_changed", model.EventTaskStatusChanged,
			map[string]any{"from": before.Status, "to": after.Status}, after)
	}
	if before.Priority != after.Priority {
		_ = s.collab.RecordTaskMutation(actorUserID, after.WorkspaceID, after.ID, "task.priority_changed", model.EventTaskPriorityChanged,
			map[string]any{"from": before.Priority, "to": after.Priority}, after)
	}
	if !taskTimeEqual(before.DueAt, after.DueAt) {
		_ = s.collab.RecordTaskMutation(actorUserID, after.WorkspaceID, after.ID, "task.due_date_changed", model.EventTaskDueDateChanged,
			map[string]any{"from": before.DueAt, "to": after.DueAt}, after)
	}
}

func normalizeTaskStatus(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return model.TaskStatusTodo
	}
	return value
}

func normalizeTaskPriority(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return model.TaskPriorityMedium
	}
	return value
}

func validTaskStatus(value string) bool {
	switch value {
	case model.TaskStatusBacklog, model.TaskStatusTodo, model.TaskStatusInProgress,
		model.TaskStatusBlocked, model.TaskStatusInReview, model.TaskStatusDone, model.TaskStatusArchived:
		return true
	default:
		return false
	}
}

func validTaskPriority(value string) bool {
	switch value {
	case model.TaskPriorityLow, model.TaskPriorityMedium, model.TaskPriorityHigh, model.TaskPriorityUrgent:
		return true
	default:
		return false
	}
}

func validTaskDates(start, due *time.Time) bool {
	return start == nil || due == nil || !due.Before(*start)
}

func validMinutes(value *int) bool {
	return value == nil || (*value >= 0 && *value <= 10000000)
}
