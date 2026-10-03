package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrInvalidTaskProject     = errors.New("invalid task project")
	ErrInvalidTaskList        = errors.New("invalid task list")
	ErrInvalidTaskLabel       = errors.New("invalid task label")
	ErrInvalidTaskComment     = errors.New("invalid task comment")
	ErrInvalidTaskDependency  = errors.New("invalid task dependency")
	ErrTaskDependencyCycle    = errors.New("task dependency would create a cycle")
	ErrInvalidTaskRecurrence  = errors.New("invalid task recurrence")
	ErrInvalidTaskCustomField = errors.New("invalid task custom field")
)

type TaskCollaborationService struct {
	repo       repository.TaskCollaborationRepository
	tasks      repository.TaskRepository
	workspaces repository.WorkspaceRepository
}

func NewTaskCollaborationService(
	repo repository.TaskCollaborationRepository,
	tasks repository.TaskRepository,
	workspaces repository.WorkspaceRepository,
) *TaskCollaborationService {
	return &TaskCollaborationService{repo: repo, tasks: tasks, workspaces: workspaces}
}

func (s *TaskCollaborationService) CreateProject(actorUserID int64, access model.WorkspaceAccess, req model.CreateTaskProjectRequest) (model.TaskProject, error) {
	name := strings.TrimSpace(req.Name)
	description := strings.TrimSpace(req.Description)
	if name == "" || len(name) > 200 || len(description) > 8000 {
		return model.TaskProject{}, ErrInvalidTaskProject
	}
	now := time.Now().UTC()
	return s.repo.CreateProject(model.TaskProject{
		WorkspaceID: access.ID, Name: name, Description: description,
		CreatedByUserID: actorUserID, CreatedAt: now, UpdatedAt: now,
	})
}

func (s *TaskCollaborationService) Projects(workspaceID int64, includeArchived bool) ([]model.TaskProject, error) {
	return s.repo.ListProjects(workspaceID, includeArchived)
}

func (s *TaskCollaborationService) ArchiveProject(workspaceID, projectID int64, archived bool) (model.TaskProject, error) {
	return s.repo.ArchiveProject(workspaceID, projectID, archived, time.Now().UTC())
}

func (s *TaskCollaborationService) CreateList(actorUserID int64, access model.WorkspaceAccess, req model.CreateTaskListRequest) (model.TaskList, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 200 || req.Position < 0 {
		return model.TaskList{}, ErrInvalidTaskList
	}
	if req.ProjectID != nil {
		ok, err := s.projectExists(access.ID, *req.ProjectID)
		if err != nil {
			return model.TaskList{}, err
		}
		if !ok {
			return model.TaskList{}, repository.ErrTaskProjectNotFound
		}
	}
	now := time.Now().UTC()
	return s.repo.CreateList(model.TaskList{
		WorkspaceID: access.ID, ProjectID: req.ProjectID, Name: name, Position: req.Position,
		CreatedByUserID: actorUserID, CreatedAt: now, UpdatedAt: now,
	})
}

func (s *TaskCollaborationService) Lists(workspaceID int64, projectID *int64, includeArchived bool) ([]model.TaskList, error) {
	return s.repo.ListLists(workspaceID, projectID, includeArchived)
}

func (s *TaskCollaborationService) ArchiveList(workspaceID, listID int64, archived bool) (model.TaskList, error) {
	return s.repo.ArchiveList(workspaceID, listID, archived, time.Now().UTC())
}

func (s *TaskCollaborationService) CreateLabel(access model.WorkspaceAccess, req model.CreateTaskLabelRequest) (model.TaskLabel, error) {
	name := strings.TrimSpace(req.Name)
	color := strings.TrimSpace(req.Color)
	if color == "" {
		color = "#808080"
	}
	if name == "" || len(name) > 80 || !validHexColor(color) {
		return model.TaskLabel{}, ErrInvalidTaskLabel
	}
	now := time.Now().UTC()
	return s.repo.CreateLabel(model.TaskLabel{
		WorkspaceID: access.ID, Name: name, Color: strings.ToUpper(color),
		CreatedAt: now, UpdatedAt: now,
	})
}

func (s *TaskCollaborationService) Labels(workspaceID int64) ([]model.TaskLabel, error) {
	return s.repo.ListLabels(workspaceID)
}

func (s *TaskCollaborationService) AddLabel(actorUserID, workspaceID, taskID, labelID int64) error {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return err
	}
	if err := s.repo.AddTaskLabel(workspaceID, taskID, labelID, time.Now().UTC()); err != nil {
		return err
	}
	return s.record(actorUserID, workspaceID, taskID, "task.label.added", "", map[string]any{"label_id": labelID}, nil)
}

func (s *TaskCollaborationService) RemoveLabel(actorUserID, workspaceID, taskID, labelID int64) error {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return err
	}
	if err := s.repo.RemoveTaskLabel(workspaceID, taskID, labelID); err != nil {
		return err
	}
	return s.record(actorUserID, workspaceID, taskID, "task.label.removed", "", map[string]any{"label_id": labelID}, nil)
}

func (s *TaskCollaborationService) TaskLabels(workspaceID, taskID int64) ([]model.TaskLabel, error) {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return nil, err
	}
	return s.repo.ListTaskLabels(workspaceID, taskID)
}

func (s *TaskCollaborationService) AddAssignee(actorUserID, workspaceID, taskID, userID int64) error {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return err
	}
	if _, err := s.workspaces.ResolveAccess(userID, workspaceID, time.Now().UTC()); err != nil {
		return repository.ErrWorkspaceMemberNotFound
	}
	now := time.Now().UTC()
	if err := s.repo.AddAssignee(workspaceID, taskID, userID, actorUserID, now); err != nil {
		return err
	}
	return s.record(actorUserID, workspaceID, taskID, "task.assigned", model.EventTaskAssigned,
		map[string]any{"user_id": userID}, map[string]any{"task_id": taskID, "user_id": userID})
}

func (s *TaskCollaborationService) RemoveAssignee(actorUserID, workspaceID, taskID, userID int64) error {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return err
	}
	if err := s.repo.RemoveAssignee(workspaceID, taskID, userID); err != nil {
		return err
	}
	return s.record(actorUserID, workspaceID, taskID, "task.unassigned", model.EventTaskUnassigned,
		map[string]any{"user_id": userID}, map[string]any{"task_id": taskID, "user_id": userID})
}

func (s *TaskCollaborationService) Assignees(workspaceID, taskID int64) ([]int64, error) {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return nil, err
	}
	return s.repo.ListAssignees(workspaceID, taskID)
}

func (s *TaskCollaborationService) AddWatcher(actorUserID, workspaceID, taskID, userID int64) error {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return err
	}
	if _, err := s.workspaces.ResolveAccess(userID, workspaceID, time.Now().UTC()); err != nil {
		return repository.ErrWorkspaceMemberNotFound
	}
	if err := s.repo.AddWatcher(workspaceID, taskID, userID, time.Now().UTC()); err != nil {
		return err
	}
	return s.record(actorUserID, workspaceID, taskID, "task.watcher.added", model.EventTaskWatcherAdded,
		map[string]any{"user_id": userID}, map[string]any{"task_id": taskID, "user_id": userID})
}

func (s *TaskCollaborationService) RemoveWatcher(actorUserID, workspaceID, taskID, userID int64) error {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return err
	}
	if err := s.repo.RemoveWatcher(workspaceID, taskID, userID); err != nil {
		return err
	}
	return s.record(actorUserID, workspaceID, taskID, "task.watcher.removed", "",
		map[string]any{"user_id": userID}, nil)
}

func (s *TaskCollaborationService) Watchers(workspaceID, taskID int64) ([]int64, error) {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return nil, err
	}
	return s.repo.ListWatchers(workspaceID, taskID)
}

func (s *TaskCollaborationService) CreateComment(actorUserID, workspaceID, taskID int64, req model.CreateTaskCommentRequest) (model.TaskComment, error) {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return model.TaskComment{}, err
	}
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > 20000 {
		return model.TaskComment{}, ErrInvalidTaskComment
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateComment(model.TaskComment{
		TaskID: taskID, WorkspaceID: workspaceID, UserID: actorUserID,
		Body: body, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return model.TaskComment{}, err
	}
	if err := s.record(actorUserID, workspaceID, taskID, "task.comment.created", model.EventTaskCommentCreated,
		map[string]any{"comment_id": item.ID}, item); err != nil {
		return model.TaskComment{}, err
	}
	return item, nil
}

func (s *TaskCollaborationService) UpdateComment(actorUserID, workspaceID, taskID, commentID int64, req model.UpdateTaskCommentRequest) (model.TaskComment, error) {
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > 20000 {
		return model.TaskComment{}, ErrInvalidTaskComment
	}
	item, err := s.repo.UpdateComment(workspaceID, taskID, commentID, actorUserID, body, time.Now().UTC())
	if err != nil {
		return model.TaskComment{}, err
	}
	if err := s.record(actorUserID, workspaceID, taskID, "task.comment.updated", model.EventTaskCommentUpdated,
		map[string]any{"comment_id": item.ID}, item); err != nil {
		return model.TaskComment{}, err
	}
	return item, nil
}

func (s *TaskCollaborationService) DeleteComment(actorUserID, workspaceID, taskID, commentID int64) (model.TaskComment, error) {
	item, err := s.repo.DeleteComment(workspaceID, taskID, commentID, actorUserID, time.Now().UTC())
	if err != nil {
		return model.TaskComment{}, err
	}
	if err := s.record(actorUserID, workspaceID, taskID, "task.comment.deleted", model.EventTaskCommentDeleted,
		map[string]any{"comment_id": item.ID}, map[string]any{"comment_id": item.ID, "task_id": taskID}); err != nil {
		return model.TaskComment{}, err
	}
	return item, nil
}

func (s *TaskCollaborationService) Comments(workspaceID, taskID int64) ([]model.TaskComment, error) {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return nil, err
	}
	return s.repo.ListComments(workspaceID, taskID)
}

func (s *TaskCollaborationService) AddDependency(actorUserID, workspaceID, taskID int64, req model.AddTaskDependencyRequest) (model.TaskDependency, error) {
	if req.DependsOnTaskID <= 0 || req.DependsOnTaskID == taskID {
		return model.TaskDependency{}, ErrInvalidTaskDependency
	}
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return model.TaskDependency{}, err
	}
	if _, err := s.requireTask(workspaceID, req.DependsOnTaskID); err != nil {
		return model.TaskDependency{}, err
	}
	cyclic, err := s.createsDependencyCycle(workspaceID, taskID, req.DependsOnTaskID)
	if err != nil {
		return model.TaskDependency{}, err
	}
	if cyclic {
		return model.TaskDependency{}, ErrTaskDependencyCycle
	}
	item, err := s.repo.CreateDependency(model.TaskDependency{
		TaskID: taskID, DependsOnTaskID: req.DependsOnTaskID,
		CreatedByUserID: actorUserID, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return model.TaskDependency{}, err
	}
	if err := s.record(actorUserID, workspaceID, taskID, "task.dependency.created", model.EventTaskDependencyCreated,
		map[string]any{"depends_on_task_id": item.DependsOnTaskID}, item); err != nil {
		return model.TaskDependency{}, err
	}
	return item, nil
}

func (s *TaskCollaborationService) RemoveDependency(actorUserID, workspaceID, taskID, dependsOnTaskID int64) error {
	if err := s.repo.DeleteDependency(workspaceID, taskID, dependsOnTaskID); err != nil {
		return err
	}
	return s.record(actorUserID, workspaceID, taskID, "task.dependency.removed", model.EventTaskDependencyRemoved,
		map[string]any{"depends_on_task_id": dependsOnTaskID},
		map[string]any{"task_id": taskID, "depends_on_task_id": dependsOnTaskID})
}

func (s *TaskCollaborationService) Dependencies(workspaceID, taskID int64) ([]model.TaskDependency, error) {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return nil, err
	}
	return s.repo.ListDependencies(workspaceID, taskID)
}

func (s *TaskCollaborationService) SetRecurrence(actorUserID, workspaceID, taskID int64, req model.SetTaskRecurrenceRequest) (model.TaskRecurrenceRule, error) {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return model.TaskRecurrenceRule{}, err
	}
	frequency := strings.ToLower(strings.TrimSpace(req.Frequency))
	timezone := strings.TrimSpace(req.Timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	if (frequency != model.RecurrenceDaily && frequency != model.RecurrenceWeekly && frequency != model.RecurrenceMonthly) ||
		req.IntervalCount < 1 || req.IntervalCount > 365 {
		return model.TaskRecurrenceRule{}, ErrInvalidTaskRecurrence
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return model.TaskRecurrenceRule{}, ErrInvalidTaskRecurrence
	}
	now := time.Now().UTC()
	nextRun := req.NextRunAt
	if nextRun == nil && req.Active {
		calculated := nextRecurrence(now, frequency, req.IntervalCount)
		nextRun = &calculated
	}
	item, err := s.repo.SetRecurrence(model.TaskRecurrenceRule{
		TaskID: taskID, Frequency: frequency, IntervalCount: req.IntervalCount,
		Timezone: timezone, NextRunAt: nextRun, Active: req.Active,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return model.TaskRecurrenceRule{}, err
	}
	if err := s.record(actorUserID, workspaceID, taskID, "task.recurrence.updated", model.EventTaskRecurrenceUpdated,
		map[string]any{"frequency": frequency, "interval_count": req.IntervalCount}, item); err != nil {
		return model.TaskRecurrenceRule{}, err
	}
	return item, nil
}

func (s *TaskCollaborationService) Recurrence(workspaceID, taskID int64) (model.TaskRecurrenceRule, error) {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return model.TaskRecurrenceRule{}, err
	}
	return s.repo.GetRecurrence(workspaceID, taskID)
}

func (s *TaskCollaborationService) DeleteRecurrence(actorUserID, workspaceID, taskID int64) error {
	if err := s.repo.DeleteRecurrence(workspaceID, taskID); err != nil {
		return err
	}
	return s.record(actorUserID, workspaceID, taskID, "task.recurrence.removed", "",
		nil, nil)
}

func (s *TaskCollaborationService) CreateCustomField(access model.WorkspaceAccess, req model.CreateTaskCustomFieldRequest) (model.TaskCustomFieldDefinition, error) {
	name := strings.TrimSpace(req.Name)
	fieldType := strings.ToLower(strings.TrimSpace(req.FieldType))
	if name == "" || len(name) > 100 || !validCustomFieldType(fieldType) {
		return model.TaskCustomFieldDefinition{}, ErrInvalidTaskCustomField
	}
	now := time.Now().UTC()
	return s.repo.CreateCustomField(model.TaskCustomFieldDefinition{
		WorkspaceID: access.ID, Name: name, FieldType: fieldType,
		Required: req.Required, CreatedAt: now, UpdatedAt: now,
	})
}

func (s *TaskCollaborationService) CustomFields(workspaceID int64) ([]model.TaskCustomFieldDefinition, error) {
	return s.repo.ListCustomFields(workspaceID)
}

func (s *TaskCollaborationService) SetCustomFieldValue(actorUserID, workspaceID, taskID, fieldID int64, req model.SetTaskCustomFieldValueRequest) (model.TaskCustomFieldValue, error) {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return model.TaskCustomFieldValue{}, err
	}
	fields, err := s.repo.ListCustomFields(workspaceID)
	if err != nil {
		return model.TaskCustomFieldValue{}, err
	}
	var field *model.TaskCustomFieldDefinition
	for i := range fields {
		if fields[i].ID == fieldID {
			field = &fields[i]
			break
		}
	}
	if field == nil {
		return model.TaskCustomFieldValue{}, repository.ErrTaskCustomFieldNotFound
	}
	if !validCustomFieldValue(field.FieldType, req.Value) {
		return model.TaskCustomFieldValue{}, ErrInvalidTaskCustomField
	}
	item, err := s.repo.SetCustomFieldValue(workspaceID, model.TaskCustomFieldValue{
		TaskID: taskID, FieldID: fieldID, Value: req.Value, UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		return model.TaskCustomFieldValue{}, err
	}
	if err := s.record(actorUserID, workspaceID, taskID, "task.custom_field.updated", "",
		map[string]any{"field_id": fieldID}, nil); err != nil {
		return model.TaskCustomFieldValue{}, err
	}
	return item, nil
}

func (s *TaskCollaborationService) TaskCustomFieldValues(workspaceID, taskID int64) ([]model.TaskCustomFieldValue, error) {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return nil, err
	}
	return s.repo.ListTaskCustomFieldValues(workspaceID, taskID)
}

func (s *TaskCollaborationService) Activity(workspaceID, taskID int64) ([]model.TaskActivity, error) {
	if _, err := s.requireTask(workspaceID, taskID); err != nil {
		return nil, err
	}
	return s.repo.ListActivity(workspaceID, taskID, 200)
}

func (s *TaskCollaborationService) RecordTaskMutation(actorUserID, workspaceID, taskID int64, action, eventType string, metadata map[string]any, data any) error {
	return s.record(actorUserID, workspaceID, taskID, action, eventType, metadata, data)
}

func (s *TaskCollaborationService) ValidateTaskReferences(workspaceID int64, projectID, listID, parentTaskID *int64) error {
	if projectID != nil {
		ok, err := s.projectExists(workspaceID, *projectID)
		if err != nil {
			return err
		}
		if !ok {
			return repository.ErrTaskProjectNotFound
		}
	}
	if listID != nil {
		lists, err := s.repo.ListLists(workspaceID, nil, true)
		if err != nil {
			return err
		}
		found := false
		for _, item := range lists {
			if item.ID != *listID {
				continue
			}
			if projectID != nil && (item.ProjectID == nil || *item.ProjectID != *projectID) {
				return ErrInvalidTaskList
			}
			found = true
			break
		}
		if !found {
			return repository.ErrTaskListNotFound
		}
	}
	if parentTaskID != nil {
		parent, err := s.tasks.FindByID(workspaceID, *parentTaskID)
		if err != nil {
			return err
		}
		if parent.DeletedAt != nil {
			return repository.ErrTaskNotFound
		}
	}
	return nil
}

func (s *TaskCollaborationService) requireTask(workspaceID, taskID int64) (model.Task, error) {
	task, err := s.tasks.FindByID(workspaceID, taskID)
	if err != nil {
		return model.Task{}, err
	}
	if task.DeletedAt != nil {
		return model.Task{}, repository.ErrTaskNotFound
	}
	return task, nil
}

func (s *TaskCollaborationService) projectExists(workspaceID, projectID int64) (bool, error) {
	items, err := s.repo.ListProjects(workspaceID, true)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if item.ID == projectID {
			return true, nil
		}
	}
	return false, nil
}

func (s *TaskCollaborationService) createsDependencyCycle(workspaceID, taskID, dependsOnTaskID int64) (bool, error) {
	seen := map[int64]bool{}
	stack := []int64{dependsOnTaskID}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current == taskID {
			return true, nil
		}
		if seen[current] {
			continue
		}
		seen[current] = true
		deps, err := s.repo.ListDependencies(workspaceID, current)
		if err != nil {
			return false, err
		}
		for _, dep := range deps {
			stack = append(stack, dep.DependsOnTaskID)
		}
	}
	return false, nil
}

func (s *TaskCollaborationService) record(actorUserID, workspaceID, taskID int64, action, eventType string, metadata map[string]any, data any) error {
	actor := actorUserID
	return s.repo.RecordActivityAndEvent(model.TaskActivity{
		WorkspaceID: workspaceID, TaskID: taskID, ActorUserID: &actor,
		Action: action, Metadata: metadata, CreatedAt: time.Now().UTC(),
	}, eventType, data)
}

func validHexColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, c := range value[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

func validCustomFieldType(value string) bool {
	switch value {
	case "text", "number", "boolean", "date":
		return true
	default:
		return false
	}
}

func validCustomFieldValue(fieldType string, value any) bool {
	switch fieldType {
	case "text":
		_, ok := value.(string)
		return ok
	case "number":
		switch value.(type) {
		case float64, float32, int, int64, int32, json.Number:
			return true
		default:
			return false
		}
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "date":
		text, ok := value.(string)
		if !ok {
			return false
		}
		if _, err := time.Parse(time.RFC3339, text); err == nil {
			return true
		}
		_, err := time.Parse("2006-01-02", text)
		return err == nil
	default:
		return false
	}
}

func nextRecurrence(from time.Time, frequency string, interval int) time.Time {
	switch frequency {
	case model.RecurrenceDaily:
		return from.AddDate(0, 0, interval)
	case model.RecurrenceWeekly:
		return from.AddDate(0, 0, interval*7)
	case model.RecurrenceMonthly:
		return from.AddDate(0, interval, 0)
	default:
		return from
	}
}

func taskMutationMetadata(before, after model.Task) map[string]any {
	return map[string]any{
		"before_status": before.Status,
		"status": after.Status,
		"before_priority": before.Priority,
		"priority": after.Priority,
	}
}

func taskTimeEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func formatTaskID(id int64) string {
	return fmt.Sprint(id)
}
