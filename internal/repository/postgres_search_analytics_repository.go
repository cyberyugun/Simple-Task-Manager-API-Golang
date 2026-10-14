package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresSearchAnalyticsRepository struct {
	db *sql.DB
}

func NewPostgresSearchAnalyticsRepository(db *sql.DB) *PostgresSearchAnalyticsRepository {
	return &PostgresSearchAnalyticsRepository{db: db}
}

func (r *PostgresSearchAnalyticsRepository) SearchTasks(workspaceID int64, query model.SearchQuery) (model.SearchPage, error) {
	args := []any{workspaceID}
	conditions := []string{"workspace_id = $1", "deleted_at IS NULL", "archived_at IS NULL"}
	add := func(template string, value any) string {
		args = append(args, value)
		return fmt.Sprintf(template, len(args))
	}

	rankExpr := "0::double precision"
	headlineExpr := "description"
	if strings.TrimSpace(query.Query) != "" {
		args = append(args, strings.TrimSpace(query.Query))
		pos := len(args)
		conditions = append(conditions, fmt.Sprintf("search_vector @@ websearch_to_tsquery('simple', $%d)", pos))
		rankExpr = fmt.Sprintf("ts_rank_cd(search_vector, websearch_to_tsquery('simple', $%d))::double precision", pos)
		headlineExpr = fmt.Sprintf("ts_headline('simple', description, websearch_to_tsquery('simple', $%d), 'MaxWords=24, MinWords=8, StartSel=<mark>, StopSel=</mark>')", pos)
	}
	if query.Status != "" {
		conditions = append(conditions, add("status = $%d", query.Status))
	}
	if query.Priority != "" {
		conditions = append(conditions, add("priority = $%d", query.Priority))
	}
	if query.ProjectID != nil {
		conditions = append(conditions, add("project_id = $%d", *query.ProjectID))
	}
	if query.AssigneeID != nil {
		conditions = append(conditions, add("EXISTS (SELECT 1 FROM task_assignees ta WHERE ta.task_id = tasks.id AND ta.user_id = $%d)", *query.AssigneeID))
	}
	where := strings.Join(conditions, " AND ")

	var total int64
	if err := r.db.QueryRow("SELECT COUNT(*) FROM tasks WHERE "+where, args...).Scan(&total); err != nil {
		return model.SearchPage{}, err
	}

	args = append(args, query.Limit)
	limitPos := len(args)
	args = append(args, (query.Page-1)*query.Limit)
	offsetPos := len(args)

	rows, err := r.db.Query(fmt.Sprintf(`
		SELECT id, workspace_id, COALESCE(created_by_user_id, user_id),
			title, description, completed, status, priority, start_at, due_at, completed_at,
			project_id, list_id, parent_task_id, position, estimated_minutes, actual_minutes,
			archived_at, deleted_at, version, created_at, updated_at,
			%s AS rank, %s AS headline
		FROM tasks
		WHERE %s
		ORDER BY rank DESC, updated_at DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, rankExpr, headlineExpr, where, limitPos, offsetPos), args...)
	if err != nil {
		return model.SearchPage{}, err
	}
	defer rows.Close()

	items := make([]model.SearchHit, 0)
	for rows.Next() {
		var task model.Task
		var rank float64
		var headline string
		if err := rows.Scan(
			&task.ID, &task.WorkspaceID, &task.UserID, &task.Title, &task.Description,
			&task.Completed, &task.Status, &task.Priority, &task.StartAt, &task.DueAt,
			&task.CompletedAt, &task.ProjectID, &task.ListID, &task.ParentTaskID,
			&task.Position, &task.EstimatedMinutes, &task.ActualMinutes, &task.ArchivedAt,
			&task.DeletedAt, &task.Version, &task.CreatedAt, &task.UpdatedAt, &rank, &headline,
		); err != nil {
			return model.SearchPage{}, err
		}
		matchType := "filter"
		if strings.TrimSpace(query.Query) != "" {
			matchType = "full_text"
		}
		items = append(items, model.SearchHit{Task: task, Rank: rank, Headline: headline, MatchType: matchType})
	}
	if err := rows.Err(); err != nil {
		return model.SearchPage{}, err
	}
	return model.SearchPage{
		Items:      items,
		Pagination: model.Pagination{Page: query.Page, Limit: query.Limit, Total: total, TotalPages: totalPages(total, query.Limit)},
	}, nil
}

func (r *PostgresSearchAnalyticsRepository) Analytics(workspaceID int64, projectID *int64, days int, now time.Time) (model.AnalyticsDashboard, error) {
	dashboard := model.AnalyticsDashboard{
		ByStatus: map[string]int64{}, ByPriority: map[string]int64{},
		Workload: []model.WorkloadMetric{}, Trend: []model.TrendMetric{}, Days: days,
	}
	err := r.db.QueryRow(`
		SELECT
			COUNT(*)::bigint,
			COUNT(*) FILTER (WHERE status <> 'DONE')::bigint,
			COUNT(*) FILTER (WHERE status = 'DONE')::bigint,
			COUNT(*) FILTER (WHERE due_at < $2 AND status <> 'DONE')::bigint,
			COALESCE(AVG(EXTRACT(EPOCH FROM (completed_at - created_at)) / 3600.0) FILTER (WHERE completed_at IS NOT NULL), 0)::double precision,
			COALESCE(AVG(EXTRACT(EPOCH FROM (completed_at - start_at)) / 3600.0) FILTER (WHERE completed_at IS NOT NULL AND start_at IS NOT NULL), 0)::double precision
		FROM tasks
		WHERE workspace_id = $1
		  AND ($3::bigint IS NULL OR project_id = $3)
		  AND deleted_at IS NULL
		  AND archived_at IS NULL
	`, workspaceID, now, projectID).Scan(
		&dashboard.Summary.TotalTasks, &dashboard.Summary.OpenTasks,
		&dashboard.Summary.CompletedTasks, &dashboard.Summary.OverdueTasks,
		&dashboard.Summary.AverageCycleHours, &dashboard.Summary.AverageLeadTimeHours,
	)
	if err != nil {
		return model.AnalyticsDashboard{}, err
	}
	if dashboard.Summary.TotalTasks > 0 {
		dashboard.Summary.CompletionRate = float64(dashboard.Summary.CompletedTasks) / float64(dashboard.Summary.TotalTasks)
	}

	if err := r.fillDimension(workspaceID, projectID, "status", dashboard.ByStatus); err != nil {
		return model.AnalyticsDashboard{}, err
	}
	if err := r.fillDimension(workspaceID, projectID, "priority", dashboard.ByPriority); err != nil {
		return model.AnalyticsDashboard{}, err
	}

	workloadRows, err := r.db.Query(`
		SELECT wm.user_id, COALESCE(u.name, ''),
			COUNT(t.id) FILTER (WHERE t.status <> 'DONE')::bigint,
			COUNT(t.id) FILTER (WHERE t.due_at < $2 AND t.status <> 'DONE')::bigint,
			COALESCE(SUM(t.estimated_minutes) FILTER (WHERE t.status <> 'DONE'), 0)::bigint,
			COALESCE(SUM(t.actual_minutes), 0)::bigint
		FROM workspace_members wm
		JOIN users u ON u.id = wm.user_id
		LEFT JOIN task_assignees ta ON ta.user_id = wm.user_id
		LEFT JOIN tasks t ON t.id = ta.task_id
			AND t.workspace_id = wm.workspace_id
			AND ($3::bigint IS NULL OR t.project_id = $3)
			AND t.deleted_at IS NULL
			AND t.archived_at IS NULL
		WHERE wm.workspace_id = $1
		GROUP BY wm.user_id, u.name
		ORDER BY COUNT(t.id) FILTER (WHERE t.status <> 'DONE') DESC, wm.user_id
	`, workspaceID, now, projectID)
	if err != nil {
		return model.AnalyticsDashboard{}, err
	}
	for workloadRows.Next() {
		var item model.WorkloadMetric
		if err := workloadRows.Scan(&item.UserID, &item.Name, &item.OpenTasks, &item.OverdueTasks, &item.EstimatedMinutes, &item.ActualMinutes); err != nil {
			workloadRows.Close()
			return model.AnalyticsDashboard{}, err
		}
		dashboard.Workload = append(dashboard.Workload, item)
	}
	if err := workloadRows.Close(); err != nil {
		return model.AnalyticsDashboard{}, err
	}

	trendRows, err := r.db.Query(`
		WITH dates AS (
			SELECT generate_series(($2::date - ($3::int - 1)), $2::date, interval '1 day')::date AS day
		)
		SELECT d.day::text,
			COUNT(t.id) FILTER (WHERE t.created_at::date = d.day)::bigint,
			COUNT(t.id) FILTER (WHERE t.completed_at::date = d.day)::bigint,
			COUNT(t.id) FILTER (
				WHERE t.due_at::date = d.day
				  AND t.due_at < $2
				  AND t.status <> 'DONE'
			)::bigint
		FROM dates d
		LEFT JOIN tasks t ON t.workspace_id = $1
			AND ($4::bigint IS NULL OR t.project_id = $4)
			AND t.deleted_at IS NULL
			AND t.archived_at IS NULL
			AND (
				t.created_at::date = d.day OR
				t.completed_at::date = d.day OR
				t.due_at::date = d.day
			)
		GROUP BY d.day
		ORDER BY d.day
	`, workspaceID, now, days, projectID)
	if err != nil {
		return model.AnalyticsDashboard{}, err
	}
	defer trendRows.Close()
	for trendRows.Next() {
		var item model.TrendMetric
		if err := trendRows.Scan(&item.Date, &item.Created, &item.Completed, &item.Overdue); err != nil {
			return model.AnalyticsDashboard{}, err
		}
		dashboard.Trend = append(dashboard.Trend, item)
	}
	return dashboard, trendRows.Err()
}

func (r *PostgresSearchAnalyticsRepository) fillDimension(workspaceID int64, projectID *int64, column string, target map[string]int64) error {
	if column != "status" && column != "priority" {
		return errors.New("unsupported analytics dimension")
	}
	rows, err := r.db.Query(fmt.Sprintf(`
		SELECT %s, COUNT(*)::bigint
		FROM tasks
		WHERE workspace_id = $1
		  AND ($2::bigint IS NULL OR project_id = $2)
		  AND deleted_at IS NULL
		  AND archived_at IS NULL
		GROUP BY %s
		ORDER BY %s
	`, column, column, column), workspaceID, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var count int64
		if err := rows.Scan(&key, &count); err != nil {
			return err
		}
		target[key] = count
	}
	return rows.Err()
}

func (r *PostgresSearchAnalyticsRepository) ExportRows(workspaceID int64, filters map[string]any, limit int) ([]model.ReportTaskRow, error) {
	args := []any{workspaceID}
	conditions := []string{"workspace_id = $1", "deleted_at IS NULL", "archived_at IS NULL"}
	add := func(template string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(template, len(args)))
	}
	if value, ok := filters["status"].(string); ok && value != "" {
		add("status = $%d", value)
	}
	if value, ok := filters["priority"].(string); ok && value != "" {
		add("priority = $%d", value)
	}
	if value, ok := filters["query"].(string); ok && strings.TrimSpace(value) != "" {
		add("search_vector @@ websearch_to_tsquery('simple', $%d)", strings.TrimSpace(value))
	}
	if value, ok := analyticsInt64(filters["project_id"]); ok {
		add("project_id = $%d", value)
	}
	if value, ok := analyticsInt64(filters["assignee_id"]); ok {
		add("EXISTS (SELECT 1 FROM task_assignees ta WHERE ta.task_id = tasks.id AND ta.user_id = $%d)", value)
	}
	args = append(args, limit)
	rows, err := r.db.Query(fmt.Sprintf(`
		SELECT id, title, status, priority, project_id, due_at, completed_at,
			estimated_minutes, actual_minutes, created_at, updated_at
		FROM tasks
		WHERE %s
		ORDER BY created_at ASC, id ASC
		LIMIT $%d
	`, strings.Join(conditions, " AND "), len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.ReportTaskRow{}
	for rows.Next() {
		var item model.ReportTaskRow
		if err := rows.Scan(
			&item.ID, &item.Title, &item.Status, &item.Priority, &item.ProjectID,
			&item.DueAt, &item.CompletedAt, &item.EstimatedMinutes, &item.ActualMinutes,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresSearchAnalyticsRepository) CreateSavedView(item model.SavedSearchView) (model.SavedSearchView, error) {
	raw, err := json.Marshal(item.Query)
	if err != nil {
		return model.SavedSearchView{}, err
	}
	err = r.db.QueryRow(`
		INSERT INTO saved_search_views (workspace_id, user_id, name, query, shared, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id
	`, item.WorkspaceID, item.UserID, item.Name, raw, item.Shared, item.CreatedAt, item.UpdatedAt).Scan(&item.ID)
	return item, err
}

func (r *PostgresSearchAnalyticsRepository) ListSavedViews(workspaceID, userID int64) ([]model.SavedSearchView, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, user_id, name, query, shared, created_at, updated_at
		FROM saved_search_views
		WHERE workspace_id = $1 AND (user_id = $2 OR shared = TRUE)
		ORDER BY LOWER(name), id
	`, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.SavedSearchView{}
	for rows.Next() {
		var item model.SavedSearchView
		var raw []byte
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.UserID, &item.Name, &raw, &item.Shared, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Query); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresSearchAnalyticsRepository) DeleteSavedView(workspaceID, userID, viewID int64) error {
	result, err := r.db.Exec("DELETE FROM saved_search_views WHERE id = $1 AND workspace_id = $2 AND user_id = $3", viewID, workspaceID, userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrSavedSearchViewNotFound
	}
	return nil
}

func (r *PostgresSearchAnalyticsRepository) CreateScheduledReport(item model.ScheduledReport) (model.ScheduledReport, error) {
	raw, err := json.Marshal(item.Filters)
	if err != nil {
		return model.ScheduledReport{}, err
	}
	err = r.db.QueryRow(`
		INSERT INTO scheduled_reports (
			workspace_id, user_id, name, format, frequency, timezone, hour, filters,
			active, next_run_at, last_run_at, last_status, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id
	`,
		item.WorkspaceID, item.UserID, item.Name, item.Format, item.Frequency, item.Timezone,
		item.Hour, raw, item.Active, item.NextRunAt, item.LastRunAt, item.LastStatus,
		item.CreatedAt, item.UpdatedAt,
	).Scan(&item.ID)
	return item, err
}

func (r *PostgresSearchAnalyticsRepository) ListScheduledReports(workspaceID, userID int64) ([]model.ScheduledReport, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, user_id, name, format, frequency, timezone, hour, filters,
			active, next_run_at, last_run_at, last_status, created_at, updated_at
		FROM scheduled_reports
		WHERE workspace_id = $1 AND user_id = $2
		ORDER BY id
	`, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.ScheduledReport{}
	for rows.Next() {
		item, err := scanScheduledReport(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresSearchAnalyticsRepository) DeleteScheduledReport(workspaceID, userID, reportID int64) error {
	result, err := r.db.Exec("DELETE FROM scheduled_reports WHERE id = $1 AND workspace_id = $2 AND user_id = $3", reportID, workspaceID, userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrScheduledReportNotFound
	}
	return nil
}

func (r *PostgresSearchAnalyticsRepository) ClaimDueReports(limit int, now time.Time) ([]model.ScheduledReport, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`
		SELECT id, workspace_id, user_id, name, format, frequency, timezone, hour, filters,
			active, next_run_at, last_run_at, last_status, created_at, updated_at
		FROM scheduled_reports
		WHERE active = TRUE
		  AND next_run_at <= $1
		  AND (last_status <> 'running' OR updated_at < $1 - interval '15 minutes')
		ORDER BY next_run_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, err
	}
	items := []model.ScheduledReport{}
	for rows.Next() {
		item, err := scanScheduledReport(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range items {
		items[i].LastStatus = model.ReportRunRunning
		items[i].UpdatedAt = now
		if _, err := tx.Exec("UPDATE scheduled_reports SET last_status = 'running', updated_at = $2 WHERE id = $1", items[i].ID, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *PostgresSearchAnalyticsRepository) UpdateScheduledReport(item model.ScheduledReport) (model.ScheduledReport, error) {
	raw, err := json.Marshal(item.Filters)
	if err != nil {
		return model.ScheduledReport{}, err
	}
	result, err := r.db.Exec(`
		UPDATE scheduled_reports
		SET name=$2, format=$3, frequency=$4, timezone=$5, hour=$6, filters=$7,
			active=$8, next_run_at=$9, last_run_at=$10, last_status=$11, updated_at=$12
		WHERE id=$1
	`, item.ID, item.Name, item.Format, item.Frequency, item.Timezone, item.Hour, raw,
		item.Active, item.NextRunAt, item.LastRunAt, item.LastStatus, item.UpdatedAt)
	if err != nil {
		return model.ScheduledReport{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return model.ScheduledReport{}, err
	}
	if count == 0 {
		return model.ScheduledReport{}, ErrScheduledReportNotFound
	}
	return item, nil
}

func (r *PostgresSearchAnalyticsRepository) CreateReportRun(item model.ReportRun) (model.ReportRun, error) {
	err := r.db.QueryRow(`
		INSERT INTO report_runs (
			schedule_id, workspace_id, status, format, row_count, payload, error,
			started_at, finished_at, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id
	`, item.ScheduleID, item.WorkspaceID, item.Status, item.Format, item.RowCount,
		item.Payload, item.Error, item.StartedAt, item.FinishedAt, item.CreatedAt).Scan(&item.ID)
	return item, err
}

func (r *PostgresSearchAnalyticsRepository) CompleteReportRun(item model.ReportRun) (model.ReportRun, error) {
	result, err := r.db.Exec(`
		UPDATE report_runs
		SET status=$2, row_count=$3, payload=$4, error=$5, finished_at=$6
		WHERE id=$1
	`, item.ID, item.Status, item.RowCount, item.Payload, item.Error, item.FinishedAt)
	if err != nil {
		return model.ReportRun{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return model.ReportRun{}, err
	}
	if count == 0 {
		return model.ReportRun{}, ErrReportRunNotFound
	}
	return item, nil
}

func (r *PostgresSearchAnalyticsRepository) ListReportRuns(workspaceID, userID int64, limit int) ([]model.ReportRunSummary, error) {
	rows, err := r.db.Query(`
		SELECT rr.id, rr.schedule_id, rr.workspace_id, rr.status, rr.format, rr.row_count,
			rr.error, rr.started_at, rr.finished_at, rr.created_at
		FROM report_runs rr
		JOIN scheduled_reports sr ON sr.id = rr.schedule_id
		WHERE rr.workspace_id = $1 AND sr.user_id = $2
		ORDER BY rr.created_at DESC, rr.id DESC
		LIMIT $3
	`, workspaceID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.ReportRunSummary{}
	for rows.Next() {
		var item model.ReportRunSummary
		if err := rows.Scan(
			&item.ID, &item.ScheduleID, &item.WorkspaceID, &item.Status, &item.Format,
			&item.RowCount, &item.Error, &item.StartedAt, &item.FinishedAt, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type scheduledReportScanner interface {
	Scan(dest ...any) error
}

func scanScheduledReport(scanner scheduledReportScanner) (model.ScheduledReport, error) {
	var item model.ScheduledReport
	var raw []byte
	err := scanner.Scan(
		&item.ID, &item.WorkspaceID, &item.UserID, &item.Name, &item.Format,
		&item.Frequency, &item.Timezone, &item.Hour, &raw, &item.Active,
		&item.NextRunAt, &item.LastRunAt, &item.LastStatus, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ScheduledReport{}, ErrScheduledReportNotFound
		}
		return model.ScheduledReport{}, err
	}
	if err := json.Unmarshal(raw, &item.Filters); err != nil {
		return model.ScheduledReport{}, err
	}
	return item, nil
}

func analyticsInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int64:
		return v, v > 0
	case int:
		return int64(v), v > 0
	case float64:
		return int64(v), v > 0 && v == float64(int64(v))
	case json.Number:
		n, err := v.Int64()
		return n, err == nil && n > 0
	default:
		return 0, false
	}
}
