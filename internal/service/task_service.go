package service

import (
	"errors"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var ErrInvalidTask = errors.New("title is required")

type TaskService struct {
	repo repository.TaskRepository
}

func NewTaskService(repo repository.TaskRepository) *TaskService {
	return &TaskService{repo: repo}
}

func (s *TaskService) Create(req model.CreateTaskRequest) (model.Task, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return model.Task{}, ErrInvalidTask
	}

	now := time.Now()
	task := model.Task{
		Title:       title,
		Description: strings.TrimSpace(req.Description),
		Completed:   false,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	return s.repo.Create(task)
}

func (s *TaskService) FindAll() ([]model.Task, error) {
	return s.repo.FindAll()
}

func (s *TaskService) FindByID(id int64) (model.Task, error) {
	return s.repo.FindByID(id)
}

func (s *TaskService) Update(id int64, req model.UpdateTaskRequest) (model.Task, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return model.Task{}, ErrInvalidTask
	}

	task, err := s.repo.FindByID(id)
	if err != nil {
		return model.Task{}, err
	}

	task.Title = title
	task.Description = strings.TrimSpace(req.Description)
	task.UpdatedAt = time.Now()
	return s.repo.Update(task)
}

func (s *TaskService) Complete(id int64) (model.Task, error) {
	task, err := s.repo.FindByID(id)
	if err != nil {
		return model.Task{}, err
	}

	task.Completed = true
	task.UpdatedAt = time.Now()
	return s.repo.Update(task)
}

func (s *TaskService) Delete(id int64) error {
	return s.repo.Delete(id)
}
