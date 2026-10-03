package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var ErrWorkflowExecutorNotAllowed = errors.New("workflow executor is not allowlisted")

type WorkflowActionContext struct {
	OrganizationID int64
	ExecutionID    int64
	ActorUserID    int64
	DryRun         bool
	Params         map[string]any
	Variables      map[string]any
}

type WorkflowActionExecutor interface {
	Name() string
	Execute(context.Context, WorkflowActionContext) (map[string]any, error)
}

type WorkflowExecutorRegistry struct {
	executors map[string]WorkflowActionExecutor
}

func NewWorkflowExecutorRegistry(taskRepo repository.TaskRepository, operationsRepo repository.OperationsRepository) *WorkflowExecutorRegistry {
	registry := &WorkflowExecutorRegistry{executors: make(map[string]WorkflowActionExecutor)}
	registry.Register(workflowNoopExecutor{})
	if taskRepo != nil {
		registry.Register(workflowTaskStatusExecutor{tasks: taskRepo})
	}
	if operationsRepo != nil {
		registry.Register(workflowIncidentExecutor{operations: operationsRepo})
	}
	return registry
}

func (r *WorkflowExecutorRegistry) Register(executor WorkflowActionExecutor) {
	if r == nil || executor == nil {
		return
	}
	name := strings.ToLower(strings.TrimSpace(executor.Name()))
	if name == "" {
		return
	}
	r.executors[name] = executor
}

func (r *WorkflowExecutorRegistry) Get(name string) (WorkflowActionExecutor, bool) {
	if r == nil {
		return nil, false
	}
	executor, ok := r.executors[strings.ToLower(strings.TrimSpace(name))]
	return executor, ok
}

func (r *WorkflowExecutorRegistry) Names() []string {
	if r == nil {
		return []string{}
	}
	names := make([]string, 0, len(r.executors))
	for name := range r.executors {
		names = append(names, name)
	}
	return names
}

type workflowNoopExecutor struct{}

func (workflowNoopExecutor) Name() string { return "workflow.noop" }

func (workflowNoopExecutor) Execute(_ context.Context, ctx WorkflowActionContext) (map[string]any, error) {
	return map[string]any{
		"executor":     "workflow.noop",
		"dry_run":      ctx.DryRun,
		"execution_id": ctx.ExecutionID,
		"params":       ctx.Params,
	}, nil
}

type workflowTaskStatusExecutor struct {
	tasks repository.TaskRepository
}

func (workflowTaskStatusExecutor) Name() string { return "task.set_status" }

func (e workflowTaskStatusExecutor) Execute(_ context.Context, ctx WorkflowActionContext) (map[string]any, error) {
	workspaceID, ok := workflowInt64(ctx.Params["workspace_id"])
	if !ok || workspaceID <= 0 {
		return nil, errors.New("task.set_status requires workspace_id")
	}
	taskID, ok := workflowInt64(ctx.Params["task_id"])
	if !ok || taskID <= 0 {
		return nil, errors.New("task.set_status requires task_id")
	}
	status := strings.ToUpper(strings.TrimSpace(fmt.Sprint(ctx.Params["status"])))
	if !workflowTaskStatusAllowed(status) {
		return nil, errors.New("task.set_status has invalid status")
	}
	if ctx.DryRun {
		return map[string]any{
			"executor":     e.Name(),
			"workspace_id": workspaceID,
			"task_id":      taskID,
			"status":       status,
			"dry_run":      true,
		}, nil
	}
	task, err := e.tasks.FindByID(workspaceID, taskID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	task.Status = status
	task.Completed = status == model.TaskStatusDone
	if task.Completed {
		task.CompletedAt = &now
	} else {
		task.CompletedAt = nil
	}
	if status == model.TaskStatusArchived {
		task.ArchivedAt = &now
	} else {
		task.ArchivedAt = nil
	}
	task.UpdatedAt = now
	updated, err := e.tasks.Update(task)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"executor": e.Name(),
		"task_id":  updated.ID,
		"status":   updated.Status,
		"version":  updated.Version,
	}, nil
}

type workflowIncidentExecutor struct {
	operations repository.OperationsRepository
}

func (workflowIncidentExecutor) Name() string { return "operations.open_incident" }

func (e workflowIncidentExecutor) Execute(_ context.Context, ctx WorkflowActionContext) (map[string]any, error) {
	severity := strings.ToLower(strings.TrimSpace(fmt.Sprint(ctx.Params["severity"])))
	if severity == "" {
		severity = model.IncidentSeverity3
	}
	if severity != model.IncidentSeverity1 && severity != model.IncidentSeverity2 &&
		severity != model.IncidentSeverity3 && severity != model.IncidentSeverity4 {
		return nil, errors.New("operations.open_incident has invalid severity")
	}
	title := strings.TrimSpace(fmt.Sprint(ctx.Params["title"]))
	if title == "" {
		title = "Workflow incident"
	}
	summary := strings.TrimSpace(fmt.Sprint(ctx.Params["summary"]))
	if ctx.DryRun {
		return map[string]any{
			"executor": e.Name(),
			"severity": severity,
			"title":    title,
			"dry_run":  true,
		}, nil
	}
	now := time.Now().UTC()
	incident, err := e.operations.CreateOperationalIncident(model.OperationalIncident{
		OrganizationID:  ctx.OrganizationID,
		Title:           title,
		Severity:        severity,
		Status:          model.IncidentStatusOpen,
		Summary:         summary,
		StartedAt:       now,
		CreatedByUserID: ctx.ActorUserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"executor":    e.Name(),
		"incident_id": incident.ID,
		"severity":    incident.Severity,
		"status":      incident.Status,
	}, nil
}

func workflowTaskStatusAllowed(status string) bool {
	switch status {
	case model.TaskStatusBacklog, model.TaskStatusTodo, model.TaskStatusInProgress,
		model.TaskStatusBlocked, model.TaskStatusInReview, model.TaskStatusDone, model.TaskStatusArchived:
		return true
	default:
		return false
	}
}

func workflowInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		return int64(typed), typed == float64(int64(typed))
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
