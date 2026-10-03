package service

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestTaskManagement2CollaborationLifecycle(t *testing.T) {
	tasks := repository.NewInMemoryTaskRepository()
	collaboration := repository.NewInMemoryTaskCollaborationRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	taskService := NewTaskService(tasks)
	collabService := NewTaskCollaborationService(collaboration, tasks, workspaces)
	taskService.SetCollaborationService(collabService)

	now := time.Now().UTC()
	access, err := workspaces.Create(1, "Product", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.AddMember(access.ID, 2, model.WorkspaceRoleMember, now); err != nil {
		t.Fatal(err)
	}

	project, err := collabService.CreateProject(1, access, model.CreateTaskProjectRequest{
		Name: "Launch", Description: "Launch project",
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := collabService.CreateList(1, access, model.CreateTaskListRequest{
		ProjectID: &project.ID, Name: "Sprint", Position: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	due := now.Add(48 * time.Hour)
	parent, err := taskService.Create(1, access, model.CreateTaskRequest{
		Title: "Parent", Status: model.TaskStatusInProgress, Priority: model.TaskPriorityHigh,
		ProjectID: &project.ID, ListID: &list.ID, DueAt: &due,
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := taskService.Create(1, access, model.CreateTaskRequest{
		Title: "Child", ParentTaskID: &parent.ID, ProjectID: &project.ID, ListID: &list.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != model.TaskStatusTodo || child.Priority != model.TaskPriorityMedium || child.Version != 1 {
		t.Fatalf("child defaults=%+v", child)
	}

	if err := collabService.AddAssignee(1, access.ID, parent.ID, 2); err != nil {
		t.Fatal(err)
	}
	if err := collabService.AddWatcher(1, access.ID, parent.ID, 2); err != nil {
		t.Fatal(err)
	}
	assignees, _ := collabService.Assignees(access.ID, parent.ID)
	if len(assignees) != 1 || assignees[0] != 2 {
		t.Fatalf("assignees=%v", assignees)
	}

	label, err := collabService.CreateLabel(access, model.CreateTaskLabelRequest{Name: "Backend", Color: "#336699"})
	if err != nil {
		t.Fatal(err)
	}
	if err := collabService.AddLabel(1, access.ID, parent.ID, label.ID); err != nil {
		t.Fatal(err)
	}

	comment, err := collabService.CreateComment(2, access.ID, parent.ID, model.CreateTaskCommentRequest{Body: "Working on this"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collabService.UpdateComment(2, access.ID, parent.ID, comment.ID, model.UpdateTaskCommentRequest{Body: "Almost done"}); err != nil {
		t.Fatal(err)
	}

	dep, err := collabService.AddDependency(1, access.ID, child.ID, model.AddTaskDependencyRequest{DependsOnTaskID: parent.ID})
	if err != nil || dep.DependsOnTaskID != parent.ID {
		t.Fatalf("dependency=%+v err=%v", dep, err)
	}
	if _, err := collabService.AddDependency(1, access.ID, parent.ID, model.AddTaskDependencyRequest{DependsOnTaskID: child.ID}); !errors.Is(err, ErrTaskDependencyCycle) {
		t.Fatalf("cycle error=%v", err)
	}

	rule, err := collabService.SetRecurrence(1, access.ID, parent.ID, model.SetTaskRecurrenceRequest{
		Frequency: model.RecurrenceWeekly, IntervalCount: 1, Timezone: "UTC", Active: true,
	})
	if err != nil || rule.NextRunAt == nil {
		t.Fatalf("recurrence=%+v err=%v", rule, err)
	}

	field, err := collabService.CreateCustomField(access, model.CreateTaskCustomFieldRequest{
		Name: "Story Points", FieldType: "number",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collabService.SetCustomFieldValue(1, access.ID, parent.ID, field.ID, model.SetTaskCustomFieldValueRequest{Value: float64(8)}); err != nil {
		t.Fatal(err)
	}

	updated, err := taskService.Update(access.ID, parent.ID, 1, model.UpdateTaskRequest{
		Title: parent.Title, Description: parent.Description, Status: model.TaskStatusInReview,
		Priority: model.TaskPriorityUrgent, DueAt: &due, ProjectID: &project.ID, ListID: &list.ID,
		Version: parent.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != model.TaskStatusInReview || updated.Priority != model.TaskPriorityUrgent || updated.Version != 2 {
		t.Fatalf("updated=%+v", updated)
	}

	archived, err := taskService.Archive(access.ID, parent.ID, 1)
	if err != nil || archived.Status != model.TaskStatusArchived || archived.ArchivedAt == nil {
		t.Fatalf("archived=%+v err=%v", archived, err)
	}
	if err := taskService.Delete(access.ID, parent.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := taskService.FindByID(access.ID, parent.ID); !errors.Is(err, repository.ErrTaskNotFound) {
		t.Fatalf("deleted task error=%v", err)
	}
	restored, err := taskService.Restore(access.ID, parent.ID, 1)
	if err != nil || restored.DeletedAt != nil || restored.ArchivedAt != nil {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}

	activity, err := collabService.Activity(access.ID, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(activity) < 8 {
		t.Fatalf("expected rich task activity, got %d", len(activity))
	}
}

func TestTaskV2OptimisticVersionConflict(t *testing.T) {
	tasks := repository.NewInMemoryTaskRepository()
	service := NewTaskService(tasks)
	access := workspaceAccess(10)
	item, err := service.Create(1, access, model.CreateTaskRequest{Title: "Versioned"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Update(10, item.ID, 1, model.UpdateTaskRequest{
		Title: "Versioned update", Version: item.Version + 99,
	})
	if !errors.Is(err, repository.ErrTaskVersionConflict) {
		t.Fatalf("error=%v want version conflict", err)
	}
}
