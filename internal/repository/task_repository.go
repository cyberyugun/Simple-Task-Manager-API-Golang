package repository

import (
	"errors"
	"sync"

	"go-simple-task-api/internal/model"
)

var ErrTaskNotFound = errors.New("task not found")

type TaskRepository interface {
	Create(task model.Task) (model.Task, error)
	FindAll() ([]model.Task, error)
	FindByID(id int64) (model.Task, error)
	Update(task model.Task) (model.Task, error)
	Delete(id int64) error
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

func (r *InMemoryTaskRepository) FindAll() ([]model.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tasks := make([]model.Task, 0, len(r.tasks))
	for _, task := range r.tasks {
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (r *InMemoryTaskRepository) FindByID(id int64) (model.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	task, ok := r.tasks[id]
	if !ok {
		return model.Task{}, ErrTaskNotFound
	}
	return task, nil
}

func (r *InMemoryTaskRepository) Update(task model.Task) (model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.tasks[task.ID]; !ok {
		return model.Task{}, ErrTaskNotFound
	}

	r.tasks[task.ID] = task
	return task, nil
}

func (r *InMemoryTaskRepository) Delete(id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.tasks[id]; !ok {
		return ErrTaskNotFound
	}

	delete(r.tasks, id)
	return nil
}
