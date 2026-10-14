package service

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
)

var ErrInvalidReportRetentionPolicy = errors.New("invalid report retention policy")

func DefaultReportRetentionPolicy() model.ReportRetentionPolicy {
	return model.ReportRetentionPolicy{
		Enabled:              false,
		PayloadRetentionDays: 30,
		DetailRetentionDays:  365,
	}
}

func NewReportRetentionPolicyFromEnv() (model.ReportRetentionPolicy, error) {
	policy := DefaultReportRetentionPolicy()
	if raw := strings.TrimSpace(os.Getenv("REPORT_RUN_RETENTION_ENABLED")); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return model.ReportRetentionPolicy{}, fmt.Errorf("%w: REPORT_RUN_RETENTION_ENABLED", ErrInvalidReportRetentionPolicy)
		}
		policy.Enabled = enabled
	}
	var err error
	policy.PayloadRetentionDays, err = reportRetentionDaysEnv("REPORT_RUN_PAYLOAD_RETENTION_DAYS", policy.PayloadRetentionDays)
	if err != nil {
		return model.ReportRetentionPolicy{}, err
	}
	policy.DetailRetentionDays, err = reportRetentionDaysEnv("REPORT_RUN_DETAIL_RETENTION_DAYS", policy.DetailRetentionDays)
	if err != nil {
		return model.ReportRetentionPolicy{}, err
	}
	if policy.DetailRetentionDays < policy.PayloadRetentionDays {
		return model.ReportRetentionPolicy{}, fmt.Errorf("%w: detail retention must be >= payload retention", ErrInvalidReportRetentionPolicy)
	}
	return policy, nil
}

func reportRetentionDaysEnv(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 3650 {
		return 0, fmt.Errorf("%w: %s must be between 1 and 3650", ErrInvalidReportRetentionPolicy, name)
	}
	return value, nil
}

func (s *SearchAnalyticsService) SetReportRetentionPolicy(policy model.ReportRetentionPolicy) error {
	if policy.PayloadRetentionDays < 1 || policy.PayloadRetentionDays > 3650 ||
		policy.DetailRetentionDays < policy.PayloadRetentionDays || policy.DetailRetentionDays > 3650 {
		return ErrInvalidReportRetentionPolicy
	}
	s.retention = policy
	return nil
}

func (s *SearchAnalyticsService) ProcessReportRetention(limit int, now time.Time) (model.ReportRetentionResult, error) {
	if !s.retention.Enabled {
		return model.ReportRetentionResult{}, nil
	}
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}
	now = now.UTC()
	payloadCutoff := now.AddDate(0, 0, -s.retention.PayloadRetentionDays)
	detailCutoff := now.AddDate(0, 0, -s.retention.DetailRetentionDays)

	purged, err := s.repo.PurgeReportRunPayloads(payloadCutoff, limit, now)
	if err != nil {
		return model.ReportRetentionResult{}, err
	}
	aggregated, err := s.repo.AggregateReportRuns(detailCutoff, limit, now)
	if err != nil {
		return model.ReportRetentionResult{PayloadsPurged: purged}, err
	}
	deleted, err := s.repo.DeleteAggregatedReportRuns(detailCutoff, limit)
	if err != nil {
		return model.ReportRetentionResult{PayloadsPurged: purged, RunsAggregated: aggregated}, err
	}
	return model.ReportRetentionResult{
		PayloadsPurged: purged,
		RunsAggregated: aggregated,
		RunsDeleted:    deleted,
	}, nil
}
