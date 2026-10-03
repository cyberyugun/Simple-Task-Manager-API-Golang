package service

import (
	"errors"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrInvalidTask      = errors.New("title is required")
	ErrInvalidTaskQuery = errors.New("invalid task query")
)

type TaskService struct {
	repo repository.TaskRepository
}

func NewTaskService(repo repository.TaskRepository) *TaskService {
	return &TaskService{repo: repo}
}

func (s *TaskService) Create(userID int64, access model.WorkspaceAccess, req model.CreateTaskRequest) (model.Task, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return model.Task{}, ErrInvalidTask
	}

	now := time.Now()
	task := model.Task{
		WorkspaceID:       access.ID,
		UserID:            userID,
		PersonalWorkspace: access.IsPersonal,
		Title:             title,
		Description:       strings.TrimSpace(req.Description),
		Completed:         false,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	return s.repo.Create(task)
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
	query.Sort = strings.ToLower(strings.TrimSpace(query.Sort))
	query.Order = strings.ToLower(strings.TrimSpace(query.Order))
	if query.Sort == "" {
		query.Sort = "created_at"
	}
	if query.Order == "" {
		query.Order = "desc"
	}

	allowedSort := map[string]bool{
		"id":         true,
		"title":      true,
		"created_at": true,
		"updated_at": true,
		"completed":  true,
	}
	if !allowedSort[query.Sort] || (query.Order != "asc" && query.Order != "desc") {
		return model.TaskQuery{}, ErrInvalidTaskQuery
	}
	return query, nil
}

func (s *TaskService) FindByID(workspaceID, id int64) (model.Task, error) {
	return s.repo.FindByID(workspaceID, id)
}

func (s *TaskService) Update(workspaceID, id int64, req model.UpdateTaskRequest) (model.Task, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return model.Task{}, ErrInvalidTask
	}

	task, err := s.repo.FindByID(workspaceID, id)
	if err != nil {
		return model.Task{}, err
	}

	task.Title = title
	task.Description = strings.TrimSpace(req.Description)
	task.UpdatedAt = time.Now()
	return s.repo.Update(task)
}

func (s *TaskService) Complete(workspaceID, id int64) (model.Task, error) {
	task, err := s.repo.FindByID(workspaceID, id)
	if err != nil {
		return model.Task{}, err
	}

	task.Completed = true
	task.UpdatedAt = time.Now()
	return s.repo.Update(task)
}

func (s *TaskService) Delete(workspaceID, id int64) error {
	return s.repo.Delete(workspaceID, id)
}
