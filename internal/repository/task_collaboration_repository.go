package repository

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrTaskProjectNotFound     = errors.New("task project not found")
	ErrTaskListNotFound        = errors.New("task list not found")
	ErrTaskLabelNotFound       = errors.New("task label not found")
	ErrTaskCommentNotFound     = errors.New("task comment not found")
	ErrTaskDependencyNotFound  = errors.New("task dependency not found")
	ErrTaskRecurrenceNotFound  = errors.New("task recurrence not found")
	ErrTaskCustomFieldNotFound = errors.New("task custom field not found")
	ErrTaskRelationExists      = errors.New("task relation already exists")
)

type TaskCollaborationRepository interface {
	CreateProject(project model.TaskProject) (model.TaskProject, error)
	ListProjects(workspaceID int64, includeArchived bool) ([]model.TaskProject, error)
	ArchiveProject(workspaceID, projectID int64, archived bool, at time.Time) (model.TaskProject, error)

	CreateList(list model.TaskList) (model.TaskList, error)
	ListLists(workspaceID int64, projectID *int64, includeArchived bool) ([]model.TaskList, error)
	ArchiveList(workspaceID, listID int64, archived bool, at time.Time) (model.TaskList, error)

	CreateLabel(label model.TaskLabel) (model.TaskLabel, error)
	ListLabels(workspaceID int64) ([]model.TaskLabel, error)
	AddTaskLabel(workspaceID, taskID, labelID int64, at time.Time) error
	RemoveTaskLabel(workspaceID, taskID, labelID int64) error
	ListTaskLabels(workspaceID, taskID int64) ([]model.TaskLabel, error)

	AddAssignee(workspaceID, taskID, userID, assignedBy int64, at time.Time) error
	RemoveAssignee(workspaceID, taskID, userID int64) error
	ListAssignees(workspaceID, taskID int64) ([]int64, error)

	AddWatcher(workspaceID, taskID, userID int64, at time.Time) error
	RemoveWatcher(workspaceID, taskID, userID int64) error
	ListWatchers(workspaceID, taskID int64) ([]int64, error)

	CreateComment(comment model.TaskComment) (model.TaskComment, error)
	UpdateComment(workspaceID, taskID, commentID, userID int64, body string, at time.Time) (model.TaskComment, error)
	DeleteComment(workspaceID, taskID, commentID, userID int64, at time.Time) (model.TaskComment, error)
	ListComments(workspaceID, taskID int64) ([]model.TaskComment, error)

	CreateDependency(dependency model.TaskDependency) (model.TaskDependency, error)
	DeleteDependency(workspaceID, taskID, dependsOnTaskID int64) error
	ListDependencies(workspaceID, taskID int64) ([]model.TaskDependency, error)

	SetRecurrence(rule model.TaskRecurrenceRule) (model.TaskRecurrenceRule, error)
	GetRecurrence(workspaceID, taskID int64) (model.TaskRecurrenceRule, error)
	DeleteRecurrence(workspaceID, taskID int64) error

	CreateCustomField(field model.TaskCustomFieldDefinition) (model.TaskCustomFieldDefinition, error)
	ListCustomFields(workspaceID int64) ([]model.TaskCustomFieldDefinition, error)
	SetCustomFieldValue(workspaceID int64, value model.TaskCustomFieldValue) (model.TaskCustomFieldValue, error)
	ListTaskCustomFieldValues(workspaceID, taskID int64) ([]model.TaskCustomFieldValue, error)

	RecordActivityAndEvent(activity model.TaskActivity, eventType string, data any) error
	ListActivity(workspaceID, taskID int64, limit int) ([]model.TaskActivity, error)
}

type InMemoryTaskCollaborationRepository struct {
	mu sync.Mutex

	projects map[int64]model.TaskProject
	lists map[int64]model.TaskList
	labels map[int64]model.TaskLabel
	taskLabels map[int64]map[int64]bool
	assignees map[int64]map[int64]bool
	watchers map[int64]map[int64]bool
	comments map[int64]model.TaskComment
	dependencies map[int64]map[int64]model.TaskDependency
	recurrences map[int64]model.TaskRecurrenceRule
	customFields map[int64]model.TaskCustomFieldDefinition
	customValues map[string]model.TaskCustomFieldValue
	activities map[int64][]model.TaskActivity

	nextProjectID int64
	nextListID int64
	nextLabelID int64
	nextCommentID int64
	nextFieldID int64
	nextActivityID int64
}

func NewInMemoryTaskCollaborationRepository() *InMemoryTaskCollaborationRepository {
	return &InMemoryTaskCollaborationRepository{
		projects: make(map[int64]model.TaskProject),
		lists: make(map[int64]model.TaskList),
		labels: make(map[int64]model.TaskLabel),
		taskLabels: make(map[int64]map[int64]bool),
		assignees: make(map[int64]map[int64]bool),
		watchers: make(map[int64]map[int64]bool),
		comments: make(map[int64]model.TaskComment),
		dependencies: make(map[int64]map[int64]model.TaskDependency),
		recurrences: make(map[int64]model.TaskRecurrenceRule),
		customFields: make(map[int64]model.TaskCustomFieldDefinition),
		customValues: make(map[string]model.TaskCustomFieldValue),
		activities: make(map[int64][]model.TaskActivity),
		nextProjectID: 1, nextListID: 1, nextLabelID: 1,
		nextCommentID: 1, nextFieldID: 1, nextActivityID: 1,
	}
}

func (r *InMemoryTaskCollaborationRepository) CreateProject(project model.TaskProject) (model.TaskProject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	project.ID = r.nextProjectID
	r.nextProjectID++
	r.projects[project.ID] = project
	return project, nil
}

func (r *InMemoryTaskCollaborationRepository) ListProjects(workspaceID int64, includeArchived bool) ([]model.TaskProject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.TaskProject, 0)
	for _, item := range r.projects {
		if item.WorkspaceID == workspaceID && (includeArchived || item.ArchivedAt == nil) {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryTaskCollaborationRepository) ArchiveProject(workspaceID, projectID int64, archived bool, at time.Time) (model.TaskProject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.projects[projectID]
	if !ok || item.WorkspaceID != workspaceID {
		return model.TaskProject{}, ErrTaskProjectNotFound
	}
	if archived {
		item.ArchivedAt = &at
	} else {
		item.ArchivedAt = nil
	}
	item.UpdatedAt = at
	r.projects[projectID] = item
	return item, nil
}

func (r *InMemoryTaskCollaborationRepository) CreateList(list model.TaskList) (model.TaskList, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list.ID = r.nextListID
	r.nextListID++
	r.lists[list.ID] = list
	return list, nil
}

func (r *InMemoryTaskCollaborationRepository) ListLists(workspaceID int64, projectID *int64, includeArchived bool) ([]model.TaskList, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.TaskList, 0)
	for _, item := range r.lists {
		if item.WorkspaceID != workspaceID || (!includeArchived && item.ArchivedAt != nil) {
			continue
		}
		if projectID != nil && (item.ProjectID == nil || *item.ProjectID != *projectID) {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Position == items[j].Position {
			return items[i].ID < items[j].ID
		}
		return items[i].Position < items[j].Position
	})
	return items, nil
}

func (r *InMemoryTaskCollaborationRepository) ArchiveList(workspaceID, listID int64, archived bool, at time.Time) (model.TaskList, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.lists[listID]
	if !ok || item.WorkspaceID != workspaceID {
		return model.TaskList{}, ErrTaskListNotFound
	}
	if archived {
		item.ArchivedAt = &at
	} else {
		item.ArchivedAt = nil
	}
	item.UpdatedAt = at
	r.lists[listID] = item
	return item, nil
}

func (r *InMemoryTaskCollaborationRepository) CreateLabel(label model.TaskLabel) (model.TaskLabel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.labels {
		if existing.WorkspaceID == label.WorkspaceID && strings.EqualFold(existing.Name, label.Name) {
			return model.TaskLabel{}, ErrTaskRelationExists
		}
	}
	label.ID = r.nextLabelID
	r.nextLabelID++
	r.labels[label.ID] = label
	return label, nil
}

func (r *InMemoryTaskCollaborationRepository) ListLabels(workspaceID int64) ([]model.TaskLabel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.TaskLabel, 0)
	for _, item := range r.labels {
		if item.WorkspaceID == workspaceID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name) })
	return items, nil
}

func (r *InMemoryTaskCollaborationRepository) AddTaskLabel(workspaceID, taskID, labelID int64, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	label, ok := r.labels[labelID]
	if !ok || label.WorkspaceID != workspaceID {
		return ErrTaskLabelNotFound
	}
	if r.taskLabels[taskID] == nil {
		r.taskLabels[taskID] = make(map[int64]bool)
	}
	if r.taskLabels[taskID][labelID] {
		return ErrTaskRelationExists
	}
	r.taskLabels[taskID][labelID] = true
	return nil
}

func (r *InMemoryTaskCollaborationRepository) RemoveTaskLabel(workspaceID, taskID, labelID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.taskLabels[taskID][labelID] {
		return ErrTaskLabelNotFound
	}
	delete(r.taskLabels[taskID], labelID)
	return nil
}

func (r *InMemoryTaskCollaborationRepository) ListTaskLabels(workspaceID, taskID int64) ([]model.TaskLabel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.TaskLabel, 0)
	for labelID := range r.taskLabels[taskID] {
		label := r.labels[labelID]
		if label.WorkspaceID == workspaceID {
			items = append(items, label)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func addUserLink(store map[int64]map[int64]bool, taskID, userID int64) error {
	if store[taskID] == nil {
		store[taskID] = make(map[int64]bool)
	}
	if store[taskID][userID] {
		return ErrTaskRelationExists
	}
	store[taskID][userID] = true
	return nil
}

func removeUserLink(store map[int64]map[int64]bool, taskID, userID int64) error {
	if !store[taskID][userID] {
		return ErrWorkspaceMemberNotFound
	}
	delete(store[taskID], userID)
	return nil
}

func listUserLinks(store map[int64]map[int64]bool, taskID int64) []int64 {
	items := make([]int64, 0)
	for userID := range store[taskID] {
		items = append(items, userID)
	}
	sort.Slice(items, func(i, j int) bool { return items[i] < items[j] })
	return items
}

func (r *InMemoryTaskCollaborationRepository) AddAssignee(workspaceID, taskID, userID, assignedBy int64, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return addUserLink(r.assignees, taskID, userID)
}

func (r *InMemoryTaskCollaborationRepository) RemoveAssignee(workspaceID, taskID, userID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return removeUserLink(r.assignees, taskID, userID)
}

func (r *InMemoryTaskCollaborationRepository) ListAssignees(workspaceID, taskID int64) ([]int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return listUserLinks(r.assignees, taskID), nil
}

func (r *InMemoryTaskCollaborationRepository) AddWatcher(workspaceID, taskID, userID int64, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return addUserLink(r.watchers, taskID, userID)
}

func (r *InMemoryTaskCollaborationRepository) RemoveWatcher(workspaceID, taskID, userID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return removeUserLink(r.watchers, taskID, userID)
}

func (r *InMemoryTaskCollaborationRepository) ListWatchers(workspaceID, taskID int64) ([]int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return listUserLinks(r.watchers, taskID), nil
}

func (r *InMemoryTaskCollaborationRepository) CreateComment(comment model.TaskComment) (model.TaskComment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	comment.ID = r.nextCommentID
	r.nextCommentID++
	r.comments[comment.ID] = comment
	return comment, nil
}

func (r *InMemoryTaskCollaborationRepository) UpdateComment(workspaceID, taskID, commentID, userID int64, body string, at time.Time) (model.TaskComment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.comments[commentID]
	if !ok || item.WorkspaceID != workspaceID || item.TaskID != taskID || item.UserID != userID || item.DeletedAt != nil {
		return model.TaskComment{}, ErrTaskCommentNotFound
	}
	item.Body = body
	item.EditedAt = &at
	item.UpdatedAt = at
	r.comments[commentID] = item
	return item, nil
}

func (r *InMemoryTaskCollaborationRepository) DeleteComment(workspaceID, taskID, commentID, userID int64, at time.Time) (model.TaskComment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.comments[commentID]
	if !ok || item.WorkspaceID != workspaceID || item.TaskID != taskID || item.UserID != userID || item.DeletedAt != nil {
		return model.TaskComment{}, ErrTaskCommentNotFound
	}
	item.DeletedAt = &at
	item.UpdatedAt = at
	r.comments[commentID] = item
	return item, nil
}

func (r *InMemoryTaskCollaborationRepository) ListComments(workspaceID, taskID int64) ([]model.TaskComment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.TaskComment, 0)
	for _, item := range r.comments {
		if item.WorkspaceID == workspaceID && item.TaskID == taskID && item.DeletedAt == nil {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func (r *InMemoryTaskCollaborationRepository) CreateDependency(dependency model.TaskDependency) (model.TaskDependency, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dependencies[dependency.TaskID] == nil {
		r.dependencies[dependency.TaskID] = make(map[int64]model.TaskDependency)
	}
	if _, ok := r.dependencies[dependency.TaskID][dependency.DependsOnTaskID]; ok {
		return model.TaskDependency{}, ErrTaskRelationExists
	}
	r.dependencies[dependency.TaskID][dependency.DependsOnTaskID] = dependency
	return dependency, nil
}

func (r *InMemoryTaskCollaborationRepository) DeleteDependency(workspaceID, taskID, dependsOnTaskID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.dependencies[taskID][dependsOnTaskID]; !ok {
		return ErrTaskDependencyNotFound
	}
	delete(r.dependencies[taskID], dependsOnTaskID)
	return nil
}

func (r *InMemoryTaskCollaborationRepository) ListDependencies(workspaceID, taskID int64) ([]model.TaskDependency, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.TaskDependency, 0)
	for _, item := range r.dependencies[taskID] {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].DependsOnTaskID < items[j].DependsOnTaskID })
	return items, nil
}

func (r *InMemoryTaskCollaborationRepository) SetRecurrence(rule model.TaskRecurrenceRule) (model.TaskRecurrenceRule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.recurrences[rule.TaskID]; ok {
		rule.CreatedAt = existing.CreatedAt
	}
	r.recurrences[rule.TaskID] = rule
	return rule, nil
}

func (r *InMemoryTaskCollaborationRepository) GetRecurrence(workspaceID, taskID int64) (model.TaskRecurrenceRule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.recurrences[taskID]
	if !ok {
		return model.TaskRecurrenceRule{}, ErrTaskRecurrenceNotFound
	}
	return item, nil
}

func (r *InMemoryTaskCollaborationRepository) DeleteRecurrence(workspaceID, taskID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.recurrences[taskID]; !ok {
		return ErrTaskRecurrenceNotFound
	}
	delete(r.recurrences, taskID)
	return nil
}

func (r *InMemoryTaskCollaborationRepository) CreateCustomField(field model.TaskCustomFieldDefinition) (model.TaskCustomFieldDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.customFields {
		if existing.WorkspaceID == field.WorkspaceID && strings.EqualFold(existing.Name, field.Name) {
			return model.TaskCustomFieldDefinition{}, ErrTaskRelationExists
		}
	}
	field.ID = r.nextFieldID
	r.nextFieldID++
	r.customFields[field.ID] = field
	return field, nil
}

func (r *InMemoryTaskCollaborationRepository) ListCustomFields(workspaceID int64) ([]model.TaskCustomFieldDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.TaskCustomFieldDefinition, 0)
	for _, item := range r.customFields {
		if item.WorkspaceID == workspaceID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func customValueKey(taskID, fieldID int64) string {
	return fmt.Sprintf("%d:%d", taskID, fieldID)
}

func (r *InMemoryTaskCollaborationRepository) SetCustomFieldValue(workspaceID int64, value model.TaskCustomFieldValue) (model.TaskCustomFieldValue, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	field, ok := r.customFields[value.FieldID]
	if !ok || field.WorkspaceID != workspaceID {
		return model.TaskCustomFieldValue{}, ErrTaskCustomFieldNotFound
	}
	r.customValues[customValueKey(value.TaskID, value.FieldID)] = value
	return value, nil
}

func (r *InMemoryTaskCollaborationRepository) ListTaskCustomFieldValues(workspaceID, taskID int64) ([]model.TaskCustomFieldValue, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.TaskCustomFieldValue, 0)
	for _, item := range r.customValues {
		if item.TaskID != taskID {
			continue
		}
		field, ok := r.customFields[item.FieldID]
		if ok && field.WorkspaceID == workspaceID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].FieldID < items[j].FieldID })
	return items, nil
}

func (r *InMemoryTaskCollaborationRepository) RecordActivityAndEvent(activity model.TaskActivity, eventType string, data any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	activity.ID = r.nextActivityID
	r.nextActivityID++
	r.activities[activity.TaskID] = append(r.activities[activity.TaskID], activity)
	return nil
}

func (r *InMemoryTaskCollaborationRepository) ListActivity(workspaceID, taskID int64, limit int) ([]model.TaskActivity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	source := r.activities[taskID]
	items := make([]model.TaskActivity, 0)
	for i := len(source)-1; i >= 0 && (limit <= 0 || len(items) < limit); i-- {
		if source[i].WorkspaceID == workspaceID {
			items = append(items, source[i])
		}
	}
	return items, nil
}
