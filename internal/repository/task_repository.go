package repository

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrTaskNotFound        = errors.New("task not found")
	ErrTaskVersionConflict = errors.New("task version conflict")
)

type TaskRepository interface {
	Create(task model.Task) (model.Task, error)
	FindAll(workspaceID int64, query model.TaskQuery) (model.TaskPage, error)
	FindByID(workspaceID, id int64) (model.Task, error)
	Update(task model.Task) (model.Task, error)
	SoftDelete(workspaceID, id int64, at time.Time) (model.Task, error)
	Restore(workspaceID, id int64, at time.Time) (model.Task, error)
	Archive(workspaceID, id int64, archived bool, at time.Time) (model.Task, error)
	Delete(workspaceID, id int64) error
}

type InMemoryTaskRepository struct {
	mu     sync.RWMutex
	tasks  map[int64]model.Task
	nextID int64
}

func NewInMemoryTaskRepository() *InMemoryTaskRepository {
	return &InMemoryTaskRepository{tasks: make(map[int64]model.Task), nextID: 1}
}

func (r *InMemoryTaskRepository) Create(task model.Task) (model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task.ID = r.nextID
	r.nextID++
	if task.Version <= 0 {
		task.Version = 1
	}
	r.tasks[task.ID] = task
	return task, nil
}

func (r *InMemoryTaskRepository) FindAll(workspaceID int64, query model.TaskQuery) (model.TaskPage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	search := strings.ToLower(query.Search)
	tasks := make([]model.Task, 0)
	for _, task := range r.tasks {
		if task.WorkspaceID != workspaceID {
			continue
		}
		if query.Deleted == nil || !*query.Deleted {
			if task.DeletedAt != nil {
				continue
			}
		} else if task.DeletedAt == nil {
			continue
		}
		if query.Archived == nil || !*query.Archived {
			if task.ArchivedAt != nil {
				continue
			}
		} else if task.ArchivedAt == nil {
			continue
		}
		if query.Completed != nil && task.Completed != *query.Completed {
			continue
		}
		if query.Status != "" && task.Status != query.Status {
			continue
		}
		if query.Priority != "" && task.Priority != query.Priority {
			continue
		}
		if query.ProjectID != nil && (task.ProjectID == nil || *task.ProjectID != *query.ProjectID) {
			continue
		}
		if query.ListID != nil && (task.ListID == nil || *task.ListID != *query.ListID) {
			continue
		}
		if search != "" {
			title := strings.ToLower(task.Title)
			description := strings.ToLower(task.Description)
			if !strings.Contains(title, search) && !strings.Contains(description, search) {
				continue
			}
		}
		tasks = append(tasks, task)
	}

	sort.Slice(tasks, func(i, j int) bool {
		comparison := compareTask(tasks[i], tasks[j], query.Sort)
		if comparison == 0 {
			comparison = compareInt64(tasks[i].ID, tasks[j].ID)
		}
		if query.Order == "desc" {
			return comparison > 0
		}
		return comparison < 0
	})

	total := int64(len(tasks))
	start := (query.Page - 1) * query.Limit
	if start >= len(tasks) {
		tasks = []model.Task{}
	} else {
		end := start + query.Limit
		if end > len(tasks) {
			end = len(tasks)
		}
		tasks = tasks[start:end]
	}
	return model.TaskPage{
		Items: tasks,
		Pagination: model.Pagination{
			Page: query.Page, Limit: query.Limit, Total: total, TotalPages: totalPages(total, query.Limit),
		},
	}, nil
}

func compareTask(a, b model.Task, field string) int {
	switch field {
	case "id":
		return compareInt64(a.ID, b.ID)
	case "title":
		return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	case "updated_at":
		return a.UpdatedAt.Compare(b.UpdatedAt)
	case "completed":
		if a.Completed == b.Completed {
			return 0
		}
		if !a.Completed {
			return -1
		}
		return 1
	case "status":
		return strings.Compare(a.Status, b.Status)
	case "priority":
		return compareInt64(int64(taskPriorityRank(a.Priority)), int64(taskPriorityRank(b.Priority)))
	case "due_at":
		return compareOptionalTime(a.DueAt, b.DueAt)
	case "position":
		return compareInt64(a.Position, b.Position)
	default:
		return a.CreatedAt.Compare(b.CreatedAt)
	}
}

func taskPriorityRank(value string) int {
	switch value {
	case model.TaskPriorityUrgent:
		return 4
	case model.TaskPriorityHigh:
		return 3
	case model.TaskPriorityMedium:
		return 2
	case model.TaskPriorityLow:
		return 1
	default:
		return 0
	}
}

func compareOptionalTime(a, b *time.Time) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return 1
	}
	if b == nil {
		return -1
	}
	return a.Compare(*b)
}

func compareInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func totalPages(total int64, limit int) int {
	if total == 0 {
		return 0
	}
	return int((total + int64(limit) - 1) / int64(limit))
}

func (r *InMemoryTaskRepository) FindByID(workspaceID, id int64) (model.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	task, ok := r.tasks[id]
	if !ok || task.WorkspaceID != workspaceID {
		return model.Task{}, ErrTaskNotFound
	}
	return task, nil
}

func (r *InMemoryTaskRepository) Update(task model.Task) (model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.tasks[task.ID]
	if !ok || existing.WorkspaceID != task.WorkspaceID {
		return model.Task{}, ErrTaskNotFound
	}
	if task.Version > 0 && task.Version != existing.Version {
		return model.Task{}, ErrTaskVersionConflict
	}
	task.Version = existing.Version + 1
	r.tasks[task.ID] = task
	return task, nil
}

func (r *InMemoryTaskRepository) SoftDelete(workspaceID, id int64, at time.Time) (model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[id]
	if !ok || task.WorkspaceID != workspaceID {
		return model.Task{}, ErrTaskNotFound
	}
	task.DeletedAt = &at
	task.UpdatedAt = at
	task.Version++
	r.tasks[id] = task
	return task, nil
}

func (r *InMemoryTaskRepository) Restore(workspaceID, id int64, at time.Time) (model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[id]
	if !ok || task.WorkspaceID != workspaceID {
		return model.Task{}, ErrTaskNotFound
	}
	task.DeletedAt = nil
	task.ArchivedAt = nil
	if task.Status == model.TaskStatusArchived {
		task.Status = model.TaskStatusTodo
		task.Completed = false
		task.CompletedAt = nil
	}
	task.UpdatedAt = at
	task.Version++
	r.tasks[id] = task
	return task, nil
}

func (r *InMemoryTaskRepository) Archive(workspaceID, id int64, archived bool, at time.Time) (model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[id]
	if !ok || task.WorkspaceID != workspaceID {
		return model.Task{}, ErrTaskNotFound
	}
	if archived {
		task.ArchivedAt = &at
		task.Status = model.TaskStatusArchived
	} else {
		task.ArchivedAt = nil
		if task.Status == model.TaskStatusArchived {
			task.Status = model.TaskStatusTodo
			task.Completed = false
			task.CompletedAt = nil
		}
	}
	task.UpdatedAt = at
	task.Version++
	r.tasks[id] = task
	return task, nil
}

func (r *InMemoryTaskRepository) Delete(workspaceID, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[id]
	if !ok || task.WorkspaceID != workspaceID {
		return ErrTaskNotFound
	}
	delete(r.tasks, id)
	return nil
}
