package repository

import (
	"errors"
	"sync"

	"go-simple-task-api/internal/model"
)

var ErrTaskNotFound = errors.New("task not found")

type TaskRepository interface {
	Create(task model.Task) (model.Task, error)
	FindAll(userID int64) ([]model.Task, error)
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

func (r *InMemoryTaskRepository) FindAll(userID int64) ([]model.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tasks := make([]model.Task, 0)
	for _, task := range r.tasks {
		if task.UserID == userID {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
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
