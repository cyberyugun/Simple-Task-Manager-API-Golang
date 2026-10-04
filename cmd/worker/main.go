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
	notificationPoll, err := durationEnv("NOTIFICATION_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		logger.Error("notification_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	notificationSchedulePoll, err := durationEnv("NOTIFICATION_SCHEDULE_POLL_INTERVAL", time.Minute)
	if err != nil {
		logger.Error("notification_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	notificationHTTPTimeout, err := durationEnv("NOTIFICATION_HTTP_TIMEOUT", 10*time.Second)
	if err != nil {
		logger.Error("notification_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	reminderHorizon, err := durationEnv("NOTIFICATION_REMINDER_HORIZON", 24*time.Hour)
	if err != nil {
		logger.Error("notification_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	notificationBatch, err := intEnv("NOTIFICATION_BATCH_SIZE", 50)
	if err != nil {
		logger.Error("notification_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	attachmentPoll, err := durationEnv("ATTACHMENT_SCAN_POLL_INTERVAL", 5*time.Second)
	if err != nil {
		logger.Error("attachment_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	attachmentRetentionPoll, err := durationEnv("ATTACHMENT_RETENTION_POLL_INTERVAL", time.Hour)
	if err != nil {
		logger.Error("attachment_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	attachmentBatch, err := intEnv("ATTACHMENT_BATCH_SIZE", 50)
	if err != nil {
		logger.Error("attachment_worker_configuration_failed", "error", err)
		os.Exit(1)
	}
	allowInsecure, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv("WEBHOOK_ALLOW_INSECURE_HTTP")))
	host, _ := os.Hostname()
	workerID := fmt.Sprintf("%s-%d", host, os.Getpid())

	notificationService := service.NewNotificationService(
		repository.NewPostgresNotificationRepository(db),
		repository.NewPostgresUserRepository(db),
		repository.NewPostgresTaskRepository(db),
		repository.NewPostgresTaskCollaborationRepository(db),
		repository.NewPostgresWorkspaceRepository(db),
		repository.NewPostgresOrganizationRepository(db),
		service.NotificationConfig{
			EmailProviderURL: strings.TrimSpace(os.Getenv("NOTIFICATION_EMAIL_PROVIDER_URL")),
			PushProviderURL:  strings.TrimSpace(os.Getenv("NOTIFICATION_PUSH_PROVIDER_URL")),
			ProviderToken:    strings.TrimSpace(os.Getenv("NOTIFICATION_PROVIDER_TOKEN")),
			HTTPTimeout:      notificationHTTPTimeout,
			AllowInsecure:    allowInsecure,
			ReminderHorizon:  reminderHorizon,
		},
	)
	worker := events.NewWorker(repository.NewPostgresEventRepository(db), events.WorkerOptions{
		WorkerID: workerID, BatchSize: batch, PollInterval: poll,
		HTTPTimeout: timeout, AllowInsecure: allowInsecure, Logger: logger,
		Consumers: []events.EventConsumer{notificationService},
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
	go runNotificationPlatform(
		ctx,
		notificationService,
		workerID+"-notification",
		notificationPoll,
		notificationSchedulePoll,
		notificationBatch,
		logger,
	)

	attachmentConfig := service.AttachmentConfig{
		Provider:        strings.TrimSpace(os.Getenv("ATTACHMENT_STORAGE_PROVIDER")),
		Bucket:          strings.TrimSpace(os.Getenv("ATTACHMENT_STORAGE_BUCKET")),
		BaseURL:         strings.TrimSpace(os.Getenv("ATTACHMENT_STORAGE_BASE_URL")),
		SigningSecret:   strings.TrimSpace(os.Getenv("ATTACHMENT_SIGNING_SECRET")),
		Encryption:      strings.TrimSpace(os.Getenv("ATTACHMENT_ENCRYPTION")),
		EncryptionKeyID: strings.TrimSpace(os.Getenv("ATTACHMENT_ENCRYPTION_KEY_ID")),
		Deduplicate:     strings.EqualFold(strings.TrimSpace(os.Getenv("ATTACHMENT_DEDUPLICATE")), "true"),
		AllowInsecure:   allowInsecure,
	}
	if attachmentConfig.SigningSecret == "" {
		attachmentConfig.SigningSecret = strings.TrimSpace(os.Getenv("JWT_SECRET"))
	}
	attachmentStore, err := service.NewSignedObjectStore(attachmentConfig)
	if err != nil {
		logger.Error("attachment_storage_configuration_failed", "error", err)
		os.Exit(1)
	}
	attachmentService := service.NewAttachmentService(
		repository.NewPostgresAttachmentRepository(db),
		repository.NewPostgresTaskRepository(db),
		repository.NewPostgresTaskCollaborationRepository(db),
		repository.NewPostgresWorkspaceRepository(db),
		attachmentStore,
		service.NoopAttachmentScanner{},
		attachmentConfig,
	)
	go runAttachmentPlatform(ctx, attachmentService, attachmentPoll, attachmentRetentionPoll, attachmentBatch, logger)

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

func runNotificationPlatform(
	ctx context.Context,
	notifications *service.NotificationService,
	workerID string,
	deliveryPoll time.Duration,
	schedulePoll time.Duration,
	batch int,
	logger *slog.Logger,
) {
	deliveryTicker := time.NewTicker(deliveryPoll)
	defer deliveryTicker.Stop()
	scheduleTicker := time.NewTicker(schedulePoll)
	defer scheduleTicker.Stop()

	processDeliveries := func() {
		items, err := notifications.ProcessDeliveries(ctx, workerID, batch)
		if err != nil {
			logger.Error("notification_delivery_batch_failed", "error", err)
			return
		}
		for _, delivery := range items {
			logger.Info(
				"notification_delivery_processed",
				"delivery_id", delivery.ID,
				"notification_id", delivery.NotificationID,
				"user_id", delivery.UserID,
				"channel", delivery.Channel,
				"status", delivery.Status,
			)
		}
	}
	processScheduled := func() {
		counts, err := notifications.ProcessScheduled(time.Now().UTC())
		if err != nil {
			logger.Error("notification_schedule_failed", "error", err)
			return
		}
		logger.Info(
			"notification_schedule_completed",
			"reminders", counts["reminders"],
			"approvals", counts["approvals"],
			"digests", counts["digests"],
		)
	}

	logger.Info(
		"notification_worker_started",
		"delivery_poll_interval", deliveryPoll,
		"schedule_poll_interval", schedulePoll,
		"batch_size", batch,
	)
	processDeliveries()
	processScheduled()
	for {
		select {
		case <-ctx.Done():
			logger.Info("notification_worker_stopped")
			return
		case <-deliveryTicker.C:
			processDeliveries()
		case <-scheduleTicker.C:
			processScheduled()
		}
	}
}

func runAttachmentPlatform(ctx context.Context, attachments *service.AttachmentService, scanPoll, retentionPoll time.Duration, batch int, logger *slog.Logger) {
	logger.Info("attachment_worker_started", "scan_poll_interval", scanPoll, "retention_poll_interval", retentionPoll, "batch_size", batch)
	scanTicker := time.NewTicker(scanPoll)
	retentionTicker := time.NewTicker(retentionPoll)
	defer scanTicker.Stop()
	defer retentionTicker.Stop()

	runScan := func() {
		items, err := attachments.ProcessPendingScans(batch)
		if err != nil {
			logger.Error("attachment_scan_batch_failed", "error", err)
			return
		}
		for _, item := range items {
			logger.Info("attachment_scan_completed", "workspace_id", item.WorkspaceID, "attachment_id", item.ID, "status", item.Status, "engine", item.ScanEngine)
		}
	}
	runRetention := func() {
		items, err := attachments.ProcessRetention(batch)
		if err != nil {
			logger.Error("attachment_retention_batch_failed", "error", err)
			return
		}
		for _, item := range items {
			logger.Info("attachment_retention_deleted", "workspace_id", item.WorkspaceID, "attachment_id", item.ID)
		}
	}
	runScan()
	runRetention()
	for {
		select {
		case <-ctx.Done():
			logger.Info("attachment_worker_stopped")
			return
		case <-scanTicker.C:
			runScan()
		case <-retentionTicker.C:
			runRetention()
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
