//go:build integration

package repository_test

import (
	"testing"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestIntegrationPostgresTaskManagement2Collaboration(t *testing.T) {
	databaseURL := getenvRequired(t, "TEST_DATABASE_URL")
	db, err := appdb.OpenPostgres(databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgres() error = %v", err)
	}
	if err := resetDatabase(db); err != nil {
		_ = db.Close()
		t.Fatalf("reset database: %v", err)
	}
	t.Cleanup(func() {
		if err := resetDatabase(db); err != nil {
			t.Errorf("cleanup database: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	applyMigrations(t, db)

	users := repository.NewPostgresUserRepository(db)
	workspaces := repository.NewPostgresWorkspaceRepository(db)
	tasks := repository.NewPostgresTaskRepository(db)
	collab := repository.NewPostgresTaskCollaborationRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	owner, err := users.Create(model.User{Name: "Task Owner", Email: "task-owner@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	member, err := users.Create(model.User{Name: "Task Member", Email: "task-member@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	access, err := workspaces.Create(owner.ID, "Task Product", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.AddMember(access.ID, member.ID, model.WorkspaceRoleMember, now); err != nil {
		t.Fatal(err)
	}

	project, err := collab.CreateProject(model.TaskProject{
		WorkspaceID: access.ID, Name: "Launch", Description: "Phase 33",
		CreatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := collab.CreateList(model.TaskList{
		WorkspaceID: access.ID, ProjectID: &project.ID, Name: "Sprint", Position: 10,
		CreatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	due := now.Add(48 * time.Hour)
	parent, err := tasks.Create(model.Task{
		WorkspaceID: access.ID, UserID: owner.ID, Title: "Parent", Status: model.TaskStatusInProgress,
		Priority: model.TaskPriorityHigh, ProjectID: &project.ID, ListID: &list.ID, DueAt: &due,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := tasks.Create(model.Task{
		WorkspaceID: access.ID, UserID: owner.ID, Title: "Child", Status: model.TaskStatusTodo,
		Priority: model.TaskPriorityMedium, ParentTaskID: &parent.ID, ProjectID: &project.ID,
		ListID: &list.ID, Version: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	label, err := collab.CreateLabel(model.TaskLabel{
		WorkspaceID: access.ID, Name: "Backend", Color: "#336699", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := collab.AddTaskLabel(access.ID, parent.ID, label.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := collab.AddAssignee(access.ID, parent.ID, member.ID, owner.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := collab.AddWatcher(access.ID, parent.ID, member.ID, now); err != nil {
		t.Fatal(err)
	}
	comment, err := collab.CreateComment(model.TaskComment{
		TaskID: parent.ID, WorkspaceID: access.ID, UserID: member.ID,
		Body: "Working", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || comment.ID == 0 {
		t.Fatalf("comment=%+v err=%v", comment, err)
	}

	dependency, err := collab.CreateDependency(access.ID, model.TaskDependency{
		TaskID: child.ID, DependsOnTaskID: parent.ID, CreatedByUserID: owner.ID, CreatedAt: now,
	})
	if err != nil || dependency.DependsOnTaskID != parent.ID {
		t.Fatalf("dependency=%+v err=%v", dependency, err)
	}

	next := now.AddDate(0, 0, 7)
	rule, err := collab.SetRecurrence(model.TaskRecurrenceRule{
		TaskID: parent.ID, Frequency: model.RecurrenceWeekly, IntervalCount: 1,
		Timezone: "UTC", NextRunAt: &next, Active: true, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || rule.NextRunAt == nil {
		t.Fatalf("rule=%+v err=%v", rule, err)
	}

	field, err := collab.CreateCustomField(model.TaskCustomFieldDefinition{
		WorkspaceID: access.ID, Name: "Story Points", FieldType: "number", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := collab.SetCustomFieldValue(access.ID, model.TaskCustomFieldValue{
		TaskID: parent.ID, FieldID: field.ID, Value: float64(8), UpdatedAt: now,
	})
	if err != nil || value.FieldID != field.ID {
		t.Fatalf("value=%+v err=%v", value, err)
	}

	actor := owner.ID
	if err := collab.RecordActivityAndEvent(model.TaskActivity{
		WorkspaceID: access.ID, TaskID: parent.ID, ActorUserID: &actor,
		Action: "task.status_changed", Metadata: map[string]any{"to": model.TaskStatusInReview}, CreatedAt: now,
	}, model.EventTaskStatusChanged, parent); err != nil {
		t.Fatal(err)
	}
	activities, err := collab.ListActivity(access.ID, parent.ID, 10)
	if err != nil || len(activities) != 1 {
		t.Fatalf("activities=%+v err=%v", activities, err)
	}

	page, err := tasks.FindAll(access.ID, model.TaskQuery{
		Page: 1, Limit: 20, ProjectID: &project.ID, AssigneeID: &member.ID,
		Sort: "created_at", Order: "asc",
	})
	if err != nil || page.Pagination.Total != 1 || page.Items[0].ID != parent.ID {
		t.Fatalf("filtered page=%+v err=%v", page, err)
	}

	updated := parent
	updated.Status = model.TaskStatusInReview
	updated.Priority = model.TaskPriorityUrgent
	updated.UpdatedAt = now.Add(time.Minute)
	updated, err = tasks.Update(updated)
	if err != nil || updated.Version != 2 {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	stale := parent
	stale.Title = "stale"
	stale.UpdatedAt = now.Add(2 * time.Minute)
	if _, err := tasks.Update(stale); err != repository.ErrTaskVersionConflict {
		t.Fatalf("stale update error=%v", err)
	}

	deleted, err := tasks.SoftDelete(access.ID, parent.ID, now.Add(3*time.Minute))
	if err != nil || deleted.DeletedAt == nil {
		t.Fatalf("deleted=%+v err=%v", deleted, err)
	}
	restored, err := tasks.Restore(access.ID, parent.ID, now.Add(4*time.Minute))
	if err != nil || restored.DeletedAt != nil {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}

	var outboxCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE workspace_id = $1 AND aggregate_type = 'task'`, access.ID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if outboxCount < 3 {
		t.Fatalf("outbox events=%d want >=3", outboxCount)
	}
}
