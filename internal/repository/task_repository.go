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
	ErrTaskNotFound         = errors.New("task not found")
	ErrIdempotencyConflict  = errors.New("idempotency key was already used with a different request")
)

type TaskRepository interface {
	Create(task model.Task) (model.Task, error)
	CreateIdempotent(task model.Task, key, requestHash string, now time.Time) (model.TaskCreateResult, error)
	FindAll(workspaceID int64, query model.TaskQuery) (model.TaskPage, error)
	FindByID(workspaceID, id int64) (model.Task, error)
	Update(task model.Task) (model.Task, error)
	Complete(workspaceID, id int64, updatedAt time.Time) (model.Task, error)
	Delete(workspaceID, id int64) error
}

type InMemoryTaskRepository struct {
	mu     sync.RWMutex
	tasks  map[int64]model.Task
	nextID      int64
	idempotency map[string]inMemoryIdempotency
}

type inMemoryIdempotency struct {
	RequestHash string
	TaskID      int64
}

func NewInMemoryTaskRepository() *InMemoryTaskRepository {
	return &InMemoryTaskRepository{
		tasks:       make(map[int64]model.Task),
		nextID:      1,
		idempotency: make(map[string]inMemoryIdempotency),
	}
}

func (r *InMemoryTaskRepository) Create(task model.Task) (model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	task.ID = r.nextID
	r.nextID++
	r.tasks[task.ID] = task
	return task, nil
}

func (r *InMemoryTaskRepository) CreateIdempotent(task model.Task, key, requestHash string, _ time.Time) (model.TaskCreateResult, error) {
	if key == "" {
		created, err := r.Create(task)
		return model.TaskCreateResult{Task: created}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	cacheKey := fmt.Sprintf("%d:%s", task.WorkspaceID, key)
	if existing, ok := r.idempotency[cacheKey]; ok {
		if existing.RequestHash != requestHash {
			return model.TaskCreateResult{}, ErrIdempotencyConflict
		}
		stored, ok := r.tasks[existing.TaskID]
		if !ok {
			return model.TaskCreateResult{}, ErrTaskNotFound
		}
		return model.TaskCreateResult{Task: stored, Replayed: true}, nil
	}

	task.ID = r.nextID
	r.nextID++
	r.tasks[task.ID] = task
	r.idempotency[cacheKey] = inMemoryIdempotency{RequestHash: requestHash, TaskID: task.ID}
	return model.TaskCreateResult{Task: task}, nil
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
		if query.Completed != nil && task.Completed != *query.Completed {
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
			Page:       query.Page,
			Limit:      query.Limit,
			Total:      total,
			TotalPages: totalPages(total, query.Limit),
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
	default:
		return a.CreatedAt.Compare(b.CreatedAt)
	}
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

	r.tasks[task.ID] = task
	return task, nil
}

func (r *InMemoryTaskRepository) Complete(workspaceID, id int64, updatedAt time.Time) (model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	task, ok := r.tasks[id]
	if !ok || task.WorkspaceID != workspaceID {
		return model.Task{}, ErrTaskNotFound
	}
	task.Completed = true
	task.UpdatedAt = updatedAt
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
