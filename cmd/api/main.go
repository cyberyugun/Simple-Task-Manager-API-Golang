package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"go-simple-task-api/internal/apidocs"
	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/buildinfo"
	"go-simple-task-api/internal/cache"
	"go-simple-task-api/internal/config"
	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/handler"
	"go-simple-task-api/internal/middleware"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/observability"
	"go-simple-task-api/internal/readiness"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

func main() {
	bootstrapLogger := observability.NewJSONLogger(os.Getenv("LOG_LEVEL"))
	slog.SetDefault(bootstrapLogger)

	cfg, err := config.Load()
	if err != nil {
		bootstrapLogger.Error("configuration_error", "error", err)
		os.Exit(1)
	}

	logger := observability.NewJSONLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	metrics := observability.NewMetrics()
	tracer, err := observability.NewTracer(
		cfg.OTELExporterEndpoint,
		cfg.OTELServiceName,
		cfg.AppEnv,
		logger,
		metrics,
	)
	if err != nil {
		logger.Error("tracing_configuration_failed", "error", err)
		os.Exit(1)
	}
	if tracer.Enabled() {
		logger.Info("tracing_configured", "protocol", "otlp_http_json", "endpoint", cfg.OTELExporterEndpoint)
	}

	var db *sql.DB
	var redisClient *redis.Client
	var taskRepo repository.TaskRepository
	var taskCollaborationRepo repository.TaskCollaborationRepository
	var userRepo repository.UserRepository
	var refreshRepo repository.RefreshTokenRepository
	var actionRepo repository.AuthActionTokenRepository
	var workspaceRepo repository.WorkspaceRepository
	var eventRepo repository.EventRepository
	var enterpriseRepo repository.EnterpriseIdentityRepository
	var governanceRepo repository.GovernanceRepository
	var lifecycleRepo repository.LifecycleRepository
	var organizationRepo repository.OrganizationRepository
	var billingRepo repository.BillingRepository
	var operationsRepo repository.OperationsRepository
	var automationRepo repository.AutomationRepository
	var integrationRepo repository.IntegrationRepository
	var mfaRepo repository.MFARepository
	var webAuthnRepo repository.WebAuthnRepository

	if cfg.DatabaseURL != "" {
		db, err = appdb.OpenPostgres(cfg.DatabaseURL, appdb.Options{
			MaxOpenConns:    cfg.DBMaxOpenConns,
			MaxIdleConns:    cfg.DBMaxIdleConns,
			ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
			ConnMaxLifetime: cfg.DBConnMaxLifetime,
		})
		if err != nil {
			logger.Error("postgres_connection_failed", "error", err)
			os.Exit(1)
		}
		defer db.Close()

		taskRepo = repository.NewPostgresTaskRepository(db)
		taskCollaborationRepo = repository.NewPostgresTaskCollaborationRepository(db)
		userRepo = repository.NewPostgresUserRepository(db)
		refreshRepo = repository.NewPostgresRefreshTokenRepository(db)
		actionRepo = repository.NewPostgresAuthActionTokenRepository(db)
		workspaceRepo = repository.NewPostgresWorkspaceRepository(db)
		eventRepo = repository.NewPostgresEventRepository(db)
		enterpriseRepo = repository.NewPostgresEnterpriseIdentityRepository(db)
		governanceRepo = repository.NewPostgresGovernanceRepository(db)
		lifecycleRepo = repository.NewPostgresLifecycleRepository(db)
		organizationRepo = repository.NewPostgresOrganizationRepository(db)
		billingRepo = repository.NewPostgresBillingRepository(db)
		operationsRepo = repository.NewPostgresOperationsRepository(db)
		automationRepo = repository.NewPostgresAutomationRepository(db)
		integrationRepo = repository.NewPostgresIntegrationRepository(db)
		mfaRepo = repository.NewPostgresMFARepository(db)
		webAuthnRepo = repository.NewPostgresWebAuthnRepository(db)
		logger.Info(
			"storage_configured",
			"backend", "postgresql",
			"max_open_connections", cfg.DBMaxOpenConns,
			"max_idle_connections", cfg.DBMaxIdleConns,
		)
	} else {
		taskRepo = repository.NewInMemoryTaskRepository()
		taskCollaborationRepo = repository.NewInMemoryTaskCollaborationRepository()
		userRepo = repository.NewInMemoryUserRepository()
		refreshRepo = repository.NewInMemoryRefreshTokenRepository()
		actionRepo = repository.NewInMemoryAuthActionTokenRepository()
		workspaceRepo = repository.NewInMemoryWorkspaceRepository()
		eventRepo = repository.NewInMemoryEventRepository()
		enterpriseRepo = repository.NewInMemoryEnterpriseIdentityRepository()
		governanceRepo = repository.NewInMemoryGovernanceRepository()
		lifecycleRepo = repository.NewInMemoryLifecycleRepository()
		organizationRepo = repository.NewInMemoryOrganizationRepository()
		billingRepo = repository.NewInMemoryBillingRepository()
		operationsRepo = repository.NewInMemoryOperationsRepository()
		automationRepo = repository.NewInMemoryAutomationRepository()
		integrationRepo = repository.NewInMemoryIntegrationRepository()
		mfaRepo = repository.NewInMemoryMFARepository()
		webAuthnRepo = repository.NewInMemoryWebAuthnRepository()
		logger.Warn("storage_configured", "backend", "in-memory")
	}

	if cfg.RedisURL != "" {
		redisClient, err = cache.OpenRedis(cfg.RedisURL, cache.Options{
			PoolSize:     cfg.RedisPoolSize,
			MinIdleConns: cfg.RedisMinIdleConns,
			PoolTimeout:  cfg.RedisPoolTimeout,
			DialTimeout:  cfg.RedisDialTimeout,
			ReadTimeout:  cfg.RedisReadTimeout,
			WriteTimeout: cfg.RedisWriteTimeout,
		})
		if err != nil {
			logger.Error("redis_connection_failed", "error", err)
			os.Exit(1)
		}
		defer redisClient.Close()
		logger.Info(
			"redis_configured",
			"pool_size", cfg.RedisPoolSize,
			"min_idle_connections", cfg.RedisMinIdleConns,
		)
	}

	tokenManager := auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTokenTTL)
	mfaCipher, err := auth.NewSecretCipher(cfg.JWTSecret)
	if err != nil {
		logger.Error("mfa_cipher_configuration_failed", "error", err)
		os.Exit(1)
	}
	authService := service.NewAuthService(
		userRepo,
		refreshRepo,
		actionRepo,
		tokenManager,
		cfg.AccessTokenTTL,
		cfg.RefreshTokenTTL,
		cfg.PasswordResetTTL,
		cfg.EmailVerificationTTL,
	)
	mfaService := service.NewMFAService(mfaRepo, userRepo, mfaCipher, "Simple Task Manager")
	webAuthnService, err := service.NewWebAuthnService(
		webAuthnRepo,
		userRepo,
		cfg.WebAuthnRPID,
		cfg.WebAuthnRPOrigins,
		cfg.WebAuthnRPDisplayName,
	)
	if err != nil {
		logger.Error("webauthn_configuration_failed", "error", err)
		os.Exit(1)
	}
	mfaService.SetWebAuthnCredentialChecker(webAuthnService)
	authService.SetMFAVerifier(mfaService)
	taskService := service.NewTaskService(taskRepo)
	taskCollaborationService := service.NewTaskCollaborationService(taskCollaborationRepo, taskRepo, workspaceRepo)
	taskService.SetCollaborationService(taskCollaborationService)
	workspaceService := service.NewWorkspaceService(workspaceRepo, userRepo)
	webhookService := service.NewWebhookService(workspaceRepo, eventRepo, cfg.WebhookAllowInsecure)
	enterpriseService := service.NewEnterpriseIdentityService(enterpriseRepo, workspaceRepo, userRepo, tokenManager)
	governanceService := service.NewGovernanceService(governanceRepo, workspaceRepo)
	lifecycleService := service.NewLifecycleService(lifecycleRepo, governanceRepo, workspaceRepo, taskRepo)
	organizationService := service.NewOrganizationService(organizationRepo, userRepo, workspaceRepo)
	billingService := service.NewBillingService(billingRepo, organizationRepo)
	operationsService := service.NewOperationsService(operationsRepo, organizationRepo, billingRepo, billingService)
	automationService := service.NewAutomationService(automationRepo, organizationRepo, operationsRepo, billingRepo, billingService)
	integrationCipher, err := service.NewIntegrationCredentialCipher(cfg.JWTSecret)
	if err != nil {
		logger.Error("integration_cipher_configuration_failed", "error", err)
		os.Exit(1)
	}
	integrationService := service.NewIntegrationService(integrationRepo, organizationRepo, integrationCipher, cfg.WebhookAllowInsecure)
	organizationService.SetEntitlementProvider(billingService)
	workspaceService.SetDeletionGuard(governanceService)
	authHandler := handler.NewAuthHandler(authService, cfg.ExposeAuthTokens)
	taskHandler := handler.NewTaskHandler(taskService)
	taskCollaborationHandler := handler.NewTaskCollaborationHandler(taskCollaborationService)
	workspaceHandler := handler.NewWorkspaceHandler(workspaceService)
	webhookHandler := handler.NewWebhookHandler(webhookService)
	enterpriseHandler := handler.NewEnterpriseIdentityHandler(enterpriseService)
	governanceHandler := handler.NewGovernanceHandler(governanceService)
	lifecycleHandler := handler.NewLifecycleHandler(lifecycleService)
	organizationHandler := handler.NewOrganizationHandler(organizationService)
	billingHandler := handler.NewBillingHandler(billingService, cfg.BillingWebhookSecret)
	operationsHandler := handler.NewOperationsHandler(operationsService)
	automationHandler := handler.NewAutomationHandler(automationService)
	integrationHandler := handler.NewIntegrationHandler(integrationService)
	mfaHandler := handler.NewMFAHandler(mfaService)
	webAuthnHandler := handler.NewWebAuthnHandler(webAuthnService, authService)
	authMiddleware := middleware.AuthWithRevocation(tokenManager, enterpriseRepo)
	serviceAuthMiddleware := middleware.EnterpriseAuth(tokenManager, enterpriseRepo)
	workspaceMiddleware := middleware.WorkspaceScope(workspaceRepo)
	enterprisePolicyMiddleware := middleware.EnterpriseWorkspacePolicy(enterpriseRepo)
	idempotencyMiddleware := middleware.Idempotency(eventRepo, cfg.IdempotencyTTL)

	var authRateLimiter middleware.AuthRateLimiter
	if redisClient != nil {
		authRateLimiter = middleware.NewRedisRateLimiter(
			redisClient,
			cfg.AuthRateLimitRequests,
			cfg.AuthRateLimitWindow,
			cfg.RateLimitFailOpen,
			logger,
			metrics,
		)
		logger.Info("rate_limiter_configured", "backend", "redis")
	} else {
		authRateLimiter = middleware.NewObservedRateLimiter(
			cfg.AuthRateLimitRequests,
			cfg.AuthRateLimitWindow,
			metrics,
		)
		logger.Info("rate_limiter_configured", "backend", "in-memory")
	}

	rateLimited := func(h http.HandlerFunc) http.Handler {
		return authRateLimiter.Handler(http.HandlerFunc(h))
	}
	protected := func(h http.HandlerFunc) http.Handler {
		return authMiddleware(http.HandlerFunc(h))
	}
	protectedFirstParty := func(h http.HandlerFunc) http.Handler {
		return authMiddleware(middleware.RequireFirstPartyUser(http.HandlerFunc(h)))
	}
	protectedIdentityAdmin := func(h http.HandlerFunc) http.Handler {
		return authMiddleware(middleware.RequireScope(model.ScopeIdentityAdmin)(http.HandlerFunc(h)))
	}
	protectedFirstPartyRateLimited := func(h http.HandlerFunc) http.Handler {
		return authMiddleware(middleware.RequireFirstPartyUser(authRateLimiter.Handler(http.HandlerFunc(h))))
	}
	protectedWorkspace := func(h http.HandlerFunc) http.Handler {
		return serviceAuthMiddleware(workspaceMiddleware(enterprisePolicyMiddleware(http.HandlerFunc(h))))
	}
	protectedWorkspaceIdempotent := func(h http.HandlerFunc) http.Handler {
		return serviceAuthMiddleware(workspaceMiddleware(enterprisePolicyMiddleware(idempotencyMiddleware(http.HandlerFunc(h)))))
	}

	ready := readiness.New(db, redisClient, cfg.ReadinessTimeout, metrics)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			response.JSON(w, http.StatusMethodNotAllowed, response.Envelope{Success: false, Message: "method not allowed"})
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "API is healthy"})
	})
	mux.HandleFunc("/ready", ready.Handler)
	mux.Handle("/metrics", metrics.HandlerWithDependencies(db, redisClient))
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			response.JSON(w, http.StatusMethodNotAllowed, response.Envelope{Success: false, Message: "method not allowed"})
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: buildinfo.Current()})
	})
	apidocs.Register(mux)

	mux.Handle("/api/auth/register", rateLimited(authHandler.Register))
	mux.Handle("/api/auth/login", rateLimited(authHandler.Login))
	mux.Handle("/api/auth/refresh", rateLimited(authHandler.Refresh))
	mux.Handle("/api/auth/logout", rateLimited(authHandler.Logout))
	mux.Handle("/api/auth/forgot-password", rateLimited(authHandler.ForgotPassword))
	mux.Handle("/api/auth/reset-password", rateLimited(authHandler.ResetPassword))
	mux.Handle("/api/auth/email-verification/confirm", rateLimited(authHandler.VerifyEmail))
	mux.Handle("/api/oauth/token", rateLimited(enterpriseHandler.OAuthToken))
	mux.Handle("/api/oauth/api-key", rateLimited(enterpriseHandler.APIKeyExchange))
	mux.Handle("/api/auth/mfa/webauthn/login/begin", rateLimited(webAuthnHandler.LoginBegin))
	mux.Handle("/api/auth/mfa/webauthn/login/finish", rateLimited(webAuthnHandler.LoginFinish))

	mux.Handle("/api/auth/change-password", protectedFirstPartyRateLimited(authHandler.ChangePassword))
	mux.Handle("/api/auth/logout-all", protectedFirstParty(authHandler.LogoutAll))
	mux.Handle("/api/auth/sessions", protectedFirstParty(authHandler.Sessions))
	mux.Handle("/api/auth/sessions/risk", protectedFirstParty(authHandler.SessionRisks))
	mux.Handle("/api/auth/sessions/", protectedFirstParty(authHandler.SessionByID))
	mux.Handle("/api/auth/email-verification/request", protectedFirstPartyRateLimited(authHandler.RequestEmailVerification))
	mux.Handle("/api/auth/token/introspect", protectedFirstParty(enterpriseHandler.Introspect))
	mux.Handle("/api/auth/token/revoke", protectedFirstParty(enterpriseHandler.RevokeToken))
	mux.Handle("/api/auth/mfa/status", protectedFirstParty(mfaHandler.Status))
	mux.Handle("/api/auth/mfa/totp/enroll", protectedFirstPartyRateLimited(mfaHandler.EnrollTOTP))
	mux.Handle("/api/auth/mfa/totp/confirm", protectedFirstPartyRateLimited(mfaHandler.ConfirmTOTP))
	mux.Handle("/api/auth/mfa/totp/disable", protectedFirstPartyRateLimited(mfaHandler.DisableTOTP))
	mux.Handle("/api/auth/mfa/webauthn/register/begin", protectedFirstPartyRateLimited(webAuthnHandler.RegistrationBegin))
	mux.Handle("/api/auth/mfa/webauthn/register/finish", protectedFirstPartyRateLimited(webAuthnHandler.RegistrationFinish))

	mux.Handle("/api/billing/plans", protectedFirstParty(billingHandler.Plans))
	mux.Handle("/api/billing/webhooks/{provider}", http.HandlerFunc(billingHandler.Webhook))
	mux.Handle("/api/integrations/connectors", protectedFirstParty(integrationHandler.Connectors))
	mux.Handle("/api/integrations/inbound/{connection_id}", http.HandlerFunc(integrationHandler.Inbound))

	mux.Handle("/api/organizations", protectedFirstParty(organizationHandler.Organizations))
	mux.Handle("/api/organizations/invitations/accept", protectedFirstParty(organizationHandler.AcceptInvitation))
	mux.Handle("/api/organizations/{id}", protectedFirstParty(organizationHandler.Organization))
	mux.Handle("/api/organizations/{id}/status", protectedFirstParty(organizationHandler.Status))
	mux.Handle("/api/organizations/{id}/quota", protectedFirstParty(organizationHandler.Quota))
	mux.Handle("/api/organizations/{id}/ownership", protectedFirstParty(organizationHandler.Ownership))
	mux.Handle("/api/organizations/{id}/directory", protectedFirstParty(organizationHandler.Directory))
	mux.Handle("/api/organizations/{id}/members/bulk", protectedFirstParty(organizationHandler.BulkMembers))
	mux.Handle("/api/organizations/{id}/members/{user_id}", protectedFirstParty(organizationHandler.Member))
	mux.Handle("/api/organizations/{id}/workspaces", protectedFirstParty(organizationHandler.Workspaces))
	mux.Handle("/api/organizations/{id}/workspaces/{workspace_id}", protectedFirstParty(organizationHandler.Workspace))
	mux.Handle("/api/organizations/{id}/invitations", protectedFirstParty(organizationHandler.Invitations))
	mux.Handle("/api/organizations/{id}/invitations/{invitation_id}", protectedFirstParty(organizationHandler.Invitation))
	mux.Handle("/api/organizations/{id}/teams", protectedFirstParty(organizationHandler.Teams))
	mux.Handle("/api/organizations/{id}/teams/{team_id}/members", protectedFirstParty(organizationHandler.TeamMembers))
	mux.Handle("/api/organizations/{id}/domains", protectedFirstParty(organizationHandler.Domains))
	mux.Handle("/api/organizations/{id}/domains/{domain_id}/verify", protectedFirstParty(organizationHandler.VerifyDomain))
	mux.Handle("/api/organizations/{id}/dashboard", protectedFirstParty(organizationHandler.Dashboard))
	mux.Handle("/api/organizations/{id}/audit", protectedFirstParty(organizationHandler.Audit))
	mux.Handle("/api/organizations/{id}/billing/subscription", protectedFirstParty(billingHandler.Subscription))
	mux.Handle("/api/organizations/{id}/billing/subscription/cancel", protectedFirstParty(billingHandler.CancelSubscription))
	mux.Handle("/api/organizations/{id}/billing/entitlements", protectedFirstParty(billingHandler.Entitlements))
	mux.Handle("/api/organizations/{id}/billing/usage", protectedFirstParty(billingHandler.Usage))
	mux.Handle("/api/organizations/{id}/billing/invoices", protectedFirstParty(billingHandler.Invoices))
	mux.Handle("/api/organizations/{id}/billing/dashboard", protectedFirstParty(billingHandler.Dashboard))
	mux.Handle("/api/organizations/{id}/operations/policy", protectedFirstParty(operationsHandler.Policy))
	mux.Handle("/api/organizations/{id}/operations/costs", protectedFirstParty(operationsHandler.Costs))
	mux.Handle("/api/organizations/{id}/operations/alerts", protectedFirstParty(operationsHandler.Alerts))
	mux.Handle("/api/organizations/{id}/operations/alerts/{alert_id}/ack", protectedFirstParty(operationsHandler.AcknowledgeAlert))
	mux.Handle("/api/organizations/{id}/operations/maintenance", protectedFirstParty(operationsHandler.Maintenance))
	mux.Handle("/api/organizations/{id}/operations/maintenance/{window_id}", protectedFirstParty(operationsHandler.MaintenanceByID))
	mux.Handle("/api/organizations/{id}/operations/incidents", protectedFirstParty(operationsHandler.Incidents))
	mux.Handle("/api/organizations/{id}/operations/incidents/{incident_id}", protectedFirstParty(operationsHandler.IncidentByID))
	mux.Handle("/api/organizations/{id}/operations/evaluate", protectedFirstParty(operationsHandler.Evaluate))
	mux.Handle("/api/organizations/{id}/operations/dashboard", protectedFirstParty(operationsHandler.Dashboard))
	mux.Handle("/api/organizations/{id}/automation/policies", protectedFirstParty(automationHandler.Policies))
	mux.Handle("/api/organizations/{id}/automation/policies/{policy_id}", protectedFirstParty(automationHandler.PolicyByID))
	mux.Handle("/api/organizations/{id}/automation/executions", protectedFirstParty(automationHandler.Executions))
	mux.Handle("/api/organizations/{id}/automation/executions/{execution_id}/decision", protectedFirstParty(automationHandler.DecideExecution))
	mux.Handle("/api/organizations/{id}/automation/evaluate", protectedFirstParty(automationHandler.Evaluate))
	mux.Handle("/api/organizations/{id}/integrations/connections", protectedFirstParty(integrationHandler.Connections))
	mux.Handle("/api/organizations/{id}/integrations/connections/{connection_id}", protectedFirstParty(integrationHandler.ConnectionByID))
	mux.Handle("/api/organizations/{id}/integrations/deliveries", protectedFirstParty(integrationHandler.Deliveries))
	mux.Handle("/api/organizations/{id}/integrations/deliveries/{delivery_id}/replay", protectedFirstParty(integrationHandler.ReplayDelivery))
	mux.Handle("/api/organizations/{id}/integrations/inbound-events", protectedFirstParty(integrationHandler.InboundEvents))

	mux.Handle("/api/workspaces", protected(workspaceHandler.Workspaces))
	mux.Handle("/api/workspaces/{id}/webhooks", protected(webhookHandler.Subscriptions))
	mux.Handle("/api/workspaces/{id}/webhooks/{subscription_id}", protected(webhookHandler.SubscriptionByID))
	mux.Handle("/api/workspaces/{id}/identity/oauth-clients", protectedIdentityAdmin(enterpriseHandler.OAuthClients))
	mux.Handle("/api/workspaces/{id}/identity/oauth/authorize", protectedFirstParty(enterpriseHandler.OAuthAuthorize))
	mux.Handle("/api/workspaces/{id}/identity/api-keys", protectedIdentityAdmin(enterpriseHandler.APIKeys))
	mux.Handle("/api/workspaces/{id}/identity/api-keys/{key_id}", protectedIdentityAdmin(enterpriseHandler.APIKeyByID))
	mux.Handle("/api/workspaces/{id}/identity/policy", protectedIdentityAdmin(enterpriseHandler.Policy))
	mux.Handle("/api/workspaces/{id}/identity/oidc", protectedIdentityAdmin(enterpriseHandler.OIDC))
	mux.Handle("/api/workspaces/{id}/identity/scim/users", protectedIdentityAdmin(enterpriseHandler.SCIMUsers))
	mux.Handle("/api/workspaces/{id}/governance/policy", protectedFirstParty(governanceHandler.Policy))
	mux.Handle("/api/workspaces/{id}/governance/data-inventory", protectedFirstParty(governanceHandler.DataInventory))
	mux.Handle("/api/workspaces/{id}/governance/legal-holds", protectedFirstParty(governanceHandler.LegalHolds))
	mux.Handle("/api/workspaces/{id}/governance/legal-holds/{hold_id}", protectedFirstParty(governanceHandler.LegalHoldByID))
	mux.Handle("/api/workspaces/{id}/governance/privacy-requests", protectedFirstParty(governanceHandler.PrivacyRequests))
	mux.Handle("/api/workspaces/{id}/governance/privacy-requests/{request_id}/complete", protectedFirstParty(governanceHandler.CompletePrivacyRequest))
	mux.Handle("/api/workspaces/{id}/governance/evidence", protectedFirstParty(governanceHandler.Evidence))
	mux.Handle("/api/workspaces/{id}/governance/lifecycle/runs", protectedFirstParty(lifecycleHandler.Runs))
	mux.Handle("/api/workspaces/{id}/governance/privacy-requests/{request_id}/export", protectedFirstParty(lifecycleHandler.ExportPrivacyRequest))
	mux.Handle("/api/workspaces/{id}/governance/privacy-requests/{request_id}/erase", protectedFirstParty(lifecycleHandler.ErasePrivacyRequest))
	mux.Handle("/api/workspaces/{id}/governance/consents", protectedFirstParty(lifecycleHandler.Consents))
	mux.Handle("/api/workspaces/{id}/governance/report", protectedFirstParty(lifecycleHandler.Report))
	mux.Handle("/api/workspaces/", protected(workspaceHandler.WorkspaceByID))

	mux.Handle("/api/task-projects", protectedWorkspaceIdempotent(taskCollaborationHandler.Projects))
	mux.Handle("/api/task-projects/{project_id}", protectedWorkspace(taskCollaborationHandler.ProjectByID))
	mux.Handle("/api/task-lists", protectedWorkspaceIdempotent(taskCollaborationHandler.Lists))
	mux.Handle("/api/task-lists/{list_id}", protectedWorkspace(taskCollaborationHandler.ListByID))
	mux.Handle("/api/task-labels", protectedWorkspaceIdempotent(taskCollaborationHandler.Labels))
	mux.Handle("/api/task-custom-fields", protectedWorkspaceIdempotent(taskCollaborationHandler.CustomFields))

	mux.Handle("/api/tasks", protectedWorkspaceIdempotent(taskHandler.Tasks))
	mux.Handle("/api/tasks/{id}/labels", protectedWorkspaceIdempotent(taskCollaborationHandler.TaskLabels))
	mux.Handle("/api/tasks/{id}/labels/{label_id}", protectedWorkspace(taskCollaborationHandler.TaskLabelByID))
	mux.Handle("/api/tasks/{id}/assignees", protectedWorkspaceIdempotent(taskCollaborationHandler.Assignees))
	mux.Handle("/api/tasks/{id}/assignees/{user_id}", protectedWorkspace(taskCollaborationHandler.AssigneeByID))
	mux.Handle("/api/tasks/{id}/watchers", protectedWorkspaceIdempotent(taskCollaborationHandler.Watchers))
	mux.Handle("/api/tasks/{id}/watchers/{user_id}", protectedWorkspace(taskCollaborationHandler.WatcherByID))
	mux.Handle("/api/tasks/{id}/comments", protectedWorkspaceIdempotent(taskCollaborationHandler.Comments))
	mux.Handle("/api/tasks/{id}/comments/{comment_id}", protectedWorkspace(taskCollaborationHandler.CommentByID))
	mux.Handle("/api/tasks/{id}/dependencies", protectedWorkspaceIdempotent(taskCollaborationHandler.Dependencies))
	mux.Handle("/api/tasks/{id}/dependencies/{depends_on_id}", protectedWorkspace(taskCollaborationHandler.DependencyByID))
	mux.Handle("/api/tasks/{id}/recurrence", protectedWorkspace(taskCollaborationHandler.Recurrence))
	mux.Handle("/api/tasks/{id}/activity", protectedWorkspace(taskCollaborationHandler.Activity))
	mux.Handle("/api/tasks/{id}/custom-fields", protectedWorkspace(taskCollaborationHandler.CustomFieldValues))
	mux.Handle("/api/tasks/{id}/custom-fields/{field_id}", protectedWorkspace(taskCollaborationHandler.CustomFieldValue))
	mux.Handle("/api/tasks/", protectedWorkspace(taskHandler.TaskByID))

	var root http.Handler = mux
	root = middleware.Recover(logger, metrics, root)
	root = middleware.AccessLog(logger, metrics, root)
	root = middleware.Trace(tracer, root)
	root = middleware.RequestID(root)
	root = middleware.APIVersion("v1", root)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("server_started", "address", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server_failed", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		logger.Info("shutdown_signal_received")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful_shutdown_failed", "error", err)
			_ = server.Close()
		}

		if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server_stop_error", "error", err)
		}
	}

	traceShutdownCtx, traceCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer traceCancel()
	if err := tracer.Shutdown(traceShutdownCtx); err != nil {
		logger.Warn("trace_shutdown_failed", "error", err)
	}

	logger.Info("server_stopped")
}
