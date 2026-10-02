package repository

import (
	"errors"
	"sort"
	"strings"
	"sync"

	"go-simple-task-api/internal/model"
)

var ErrTaskNotFound = errors.New("task not found")

type TaskRepository interface {
	Create(task model.Task) (model.Task, error)
	FindAll(userID int64, query model.TaskQuery) (model.TaskPage, error)
	FindByID(userID, id int64) (model.Task, error)
	Update(task model.Task) (model.Task, error)
	Delete(userID, id int64) error
}

type InMemoryTaskRepository struct {
	mu     sync.RWMutex
	tasks  map[int64]model.Task
	nextID int64
}

func NewInMemoryTaskRepository() *InMemoryTaskRepository {
	return &InMemoryTaskRepository{
		tasks:  make(map[int64]model.Task),
		nextID: 1,
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

func (r *InMemoryTaskRepository) FindAll(userID int64, query model.TaskQuery) (model.TaskPage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	search := strings.ToLower(query.Search)
	tasks := make([]model.Task, 0)
	for _, task := range r.tasks {
		if task.UserID != userID {
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

func (r *InMemoryTaskRepository) FindByID(userID, id int64) (model.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	task, ok := r.tasks[id]
	if !ok || task.UserID != userID {
		return model.Task{}, ErrTaskNotFound
	}
	return task, nil
}

func (r *InMemoryTaskRepository) Update(task model.Task) (model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.tasks[task.ID]
	if !ok || existing.UserID != task.UserID {
		return model.Task{}, ErrTaskNotFound
	}

	r.tasks[task.ID] = task
	return task, nil
}

func (r *InMemoryTaskRepository) Delete(userID, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	task, ok := r.tasks[id]
	if !ok || task.UserID != userID {
		return ErrTaskNotFound
	}

	delete(r.tasks, id)
	return nil
}
