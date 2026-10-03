package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/events"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/observability"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
)

func main() {
	logger := observability.NewJSONLogger(os.Getenv("LOG_LEVEL"))
	slog.SetDefault(logger)

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		logger.Error("event_worker_configuration_failed", "error", "DATABASE_URL is required")
		os.Exit(1)
	}
	db, err := appdb.OpenPostgres(databaseURL)
	if err != nil {
		logger.Error("event_worker_database_failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	poll, err := durationEnv("WORKER_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		logger.Error("event_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	timeout, err := durationEnv("WEBHOOK_TIMEOUT", 10*time.Second)
	if err != nil {
		logger.Error("event_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	batch, err := intEnv("WORKER_BATCH_SIZE", 50)
	if err != nil {
		logger.Error("event_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	lifecyclePoll, err := durationEnv("LIFECYCLE_POLL_INTERVAL", time.Hour)
	if err != nil {
		logger.Error("lifecycle_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	automationPoll, err := durationEnv("AUTOMATION_POLL_INTERVAL", 5*time.Minute)
	if err != nil {
		logger.Error("automation_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	workflowPoll, err := durationEnv("WORKFLOW_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		logger.Error("workflow_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	notificationPoll, err := durationEnv("NOTIFICATION_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		logger.Error("notification_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	reminderPoll, err := durationEnv("NOTIFICATION_REMINDER_POLL_INTERVAL", time.Minute)
	if err != nil {
		logger.Error("notification_reminder_configuration_failed", "error", err)
		os.Exit(1)
	}
	integrationPoll, err := durationEnv("INTEGRATION_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		logger.Error("integration_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	integrationBatch, err := intEnv("INTEGRATION_BATCH_SIZE", 25)
	if err != nil {
		logger.Error("integration_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	allowInsecure, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv("WEBHOOK_ALLOW_INSECURE_HTTP")))
	host, _ := os.Hostname()
	workerID := fmt.Sprintf("%s-%d", host, os.Getpid())

	worker := events.NewWorker(repository.NewPostgresEventRepository(db), events.WorkerOptions{
		WorkerID: workerID, BatchSize: batch, PollInterval: poll,
		HTTPTimeout: timeout, AllowInsecure: allowInsecure, Logger: logger,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	lifecycleService := service.NewLifecycleService(
		repository.NewPostgresLifecycleRepository(db),
		repository.NewPostgresGovernanceRepository(db),
		repository.NewPostgresWorkspaceRepository(db),
		repository.NewPostgresTaskRepository(db),
	)
	go runLifecycleAutomation(ctx, lifecycleService, lifecyclePoll, logger)

	organizationRepo := repository.NewPostgresOrganizationRepository(db)
	billingRepo := repository.NewPostgresBillingRepository(db)
	billingService := service.NewBillingService(billingRepo, organizationRepo)
	automationService := service.NewAutomationService(
		repository.NewPostgresAutomationRepository(db),
		organizationRepo,
		repository.NewPostgresOperationsRepository(db),
		billingRepo,
		billingService,
	)
	go runAutomationPolicies(ctx, automationService, automationPoll, logger)

	workflowService := service.NewWorkflowService(
		repository.NewPostgresWorkflowRepository(db),
		organizationRepo,
		service.NewWorkflowExecutorRegistry(repository.NewPostgresTaskRepository(db), repository.NewPostgresOperationsRepository(db)),
	)
	go runWorkflowExecutions(ctx, workflowService, workflowPoll, logger)

	notificationService := service.NewNotificationService(
		repository.NewPostgresNotificationRepository(db),
		repository.NewPostgresUserRepository(db),
		organizationRepo,
		service.NewLogNotificationSender(model.NotificationChannelInApp, logger),
		service.NewLogNotificationSender(model.NotificationChannelEmail, logger),
		service.NewLogNotificationSender(model.NotificationChannelPush, logger),
		service.NewLogNotificationSender(model.NotificationChannelWebhook, logger),
	)
	workflowService.SetNotificationEmitter(notificationService)
	billingService.SetNotificationEmitter(notificationService)
	automationService.SetNotificationEmitter(notificationService)
	go runNotificationDeliveries(ctx, notificationService, notificationPoll, logger)
	go runNotificationReminders(ctx, notificationService, reminderPoll, logger)

	integrationCipher, err := service.NewIntegrationCredentialCipher(os.Getenv("JWT_SECRET"))
	if err != nil {
		logger.Error("integration_cipher_configuration_failed", "error", err)
		os.Exit(1)
	}
	integrationService := service.NewIntegrationService(
		repository.NewPostgresIntegrationRepository(db),
		organizationRepo,
		integrationCipher,
		allowInsecure,
	)
	go runIntegrationDeliveries(ctx, integrationService, workerID+"-integration", integrationPoll, integrationBatch, logger)

	logger.Info("event_worker_started", "worker_id", workerID, "batch_size", batch, "poll_interval", poll)
	if err := worker.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("event_worker_stopped", "error", err)
		os.Exit(1)
	}
	logger.Info("event_worker_stopped")
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return value, nil
}

func intEnv(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func runLifecycleAutomation(ctx context.Context, lifecycle *service.LifecycleService, poll time.Duration, logger *slog.Logger) {
	run := func() {
		workspaceIDs, err := lifecycle.ConfiguredWorkspaceIDs()
		if err != nil {
			logger.Error("lifecycle_workspace_list_failed", "error", err)
			return
		}
		for _, workspaceID := range workspaceIDs {
			run, err := lifecycle.RunSystem(workspaceID)
			if err != nil {
				logger.Error("lifecycle_run_failed", "workspace_id", workspaceID, "error", err)
				continue
			}
			logger.Info("lifecycle_run_completed", "workspace_id", workspaceID, "status", run.Status, "archived_count", run.ArchivedCount, "purged_count", run.PurgedCount)
		}
	}

	logger.Info("lifecycle_worker_started", "poll_interval", poll)
	run()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("lifecycle_worker_stopped")
			return
		case <-ticker.C:
			run()
		}
	}
}

func runAutomationPolicies(ctx context.Context, automation *service.AutomationService, poll time.Duration, logger *slog.Logger) {
	run := func() {
		executions, err := automation.EvaluateAllSystem()
		if err != nil {
			logger.Error("automation_evaluation_failed", "error", err)
			return
		}
		for _, execution := range executions {
			logger.Info(
				"automation_execution_evaluated",
				"organization_id", execution.OrganizationID,
				"policy_id", execution.PolicyID,
				"execution_id", execution.ID,
				"status", execution.Status,
			)
		}
	}

	logger.Info("automation_worker_started", "poll_interval", poll)
	run()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("automation_worker_stopped")
			return
		case <-ticker.C:
			run()
		}
	}
}

func runWorkflowExecutions(ctx context.Context, workflows *service.WorkflowService, poll time.Duration, logger *slog.Logger) {
	run := func() {
		executions, err := workflows.ProcessDueSystem(50)
		if err != nil {
			logger.Error("workflow_resume_failed", "error", err)
			return
		}
		for _, execution := range executions {
			logger.Info(
				"workflow_execution_resumed",
				"organization_id", execution.OrganizationID,
				"workflow_id", execution.WorkflowID,
				"execution_id", execution.ID,
				"status", execution.Status,
			)
		}
	}

	logger.Info("workflow_worker_started", "poll_interval", poll)
	run()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("workflow_worker_stopped")
			return
		case <-ticker.C:
			run()
		}
	}
}

func runNotificationDeliveries(ctx context.Context, notifications *service.NotificationService, poll time.Duration, logger *slog.Logger) {
	run := func() {
		count, err := notifications.ProcessDeliveries(ctx, 100)
		if err != nil {
			logger.Error("notification_delivery_batch_failed", "error", err)
			return
		}
		if count > 0 {
			logger.Info("notification_delivery_batch_completed", "count", count)
		}
	}
	logger.Info("notification_worker_started", "poll_interval", poll)
	run()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("notification_worker_stopped")
			return
		case <-ticker.C:
			run()
		}
	}
}

func runNotificationReminders(ctx context.Context, notifications *service.NotificationService, poll time.Duration, logger *slog.Logger) {
	run := func() {
		count, err := notifications.ProcessReminders(500)
		if err != nil {
			logger.Error("notification_reminder_batch_failed", "error", err)
			return
		}
		if count > 0 {
			logger.Info("notification_reminder_batch_completed", "count", count)
		}
	}
	logger.Info("notification_reminder_worker_started", "poll_interval", poll)
	run()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("notification_reminder_worker_stopped")
			return
		case <-ticker.C:
			run()
		}
	}
}

func runIntegrationDeliveries(ctx context.Context, integrations *service.IntegrationService, workerID string, poll time.Duration, batch int, logger *slog.Logger) {
	run := func() {
		deliveries, err := integrations.ProcessBatch(workerID, batch)
		if err != nil {
			logger.Error("integration_delivery_batch_failed", "error", err)
			return
		}
		for _, delivery := range deliveries {
			logger.Info(
				"integration_delivery_processed",
				"organization_id", delivery.OrganizationID,
				"connection_id", delivery.ConnectionID,
				"delivery_id", delivery.ID,
				"status", delivery.Status,
			)
		}
	}

	logger.Info("integration_worker_started", "poll_interval", poll, "batch_size", batch)
	run()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("integration_worker_stopped")
			return
		case <-ticker.C:
			run()
		}
	}
}
