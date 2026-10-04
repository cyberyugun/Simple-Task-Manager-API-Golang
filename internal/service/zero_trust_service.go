package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrZeroTrustForbidden      = errors.New("zero trust administration requires organization owner or admin")
	ErrZeroTrustMemberRequired = errors.New("zero trust risk evaluation requires organization membership")
	ErrInvalidZeroTrustPolicy  = errors.New("invalid zero trust policy")
	ErrInvalidWorkloadIdentity = errors.New("invalid workload identity")
	ErrInvalidWorkloadCert     = errors.New("invalid workload certificate")
	ErrInvalidDeviceTrust      = errors.New("invalid device trust record")
	ErrInvalidRiskEvaluation   = errors.New("invalid risk evaluation")
	ErrNoSecurityEvents        = errors.New("no new security events to checkpoint")
	ErrInvalidSIEMDestination  = errors.New("invalid siem destination")
	ErrSIEMFederationDisabled  = errors.New("siem federation is disabled by zero trust policy")
)

type ZeroTrustService struct {
	repo       repository.ZeroTrustRepository
	orgs       repository.OrganizationRepository
	refreshes  repository.RefreshTokenRepository
	tokens     *auth.TokenManager
	signingKey []byte
}

func NewZeroTrustService(
	repo repository.ZeroTrustRepository,
	orgs repository.OrganizationRepository,
	refreshes repository.RefreshTokenRepository,
	tokens *auth.TokenManager,
	signingSecret string,
) *ZeroTrustService {
	return &ZeroTrustService{
		repo: repo, orgs: orgs, refreshes: refreshes, tokens: tokens,
		signingKey: []byte(signingSecret),
	}
}

func defaultZeroTrustPolicy(organizationID int64) model.ZeroTrustPolicy {
	return model.ZeroTrustPolicy{
		OrganizationID:          organizationID,
		Enabled:                 true,
		RequireWorkloadMTLS:     true,
		RequireBoundTokens:      true,
		StepUpRiskScore:         40,
		RevokeRiskScore:         75,
		ImpossibleTravelKPH:     900,
		WAFBlockScore:           80,
		AllowedCIDRs:            []string{},
		DeniedCIDRs:             []string{},
		AutoRevokeHighRisk:      true,
		RequireTrustedDevice:    false,
		SIEMFederationEnabled:   false,
		AuditCheckpointInterval: 100,
	}
}

func (s *ZeroTrustService) Policy(actorUserID, organizationID int64) (model.ZeroTrustPolicy, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.ZeroTrustPolicy{}, err
	}
	return s.policyOrDefault(organizationID)
}

func (s *ZeroTrustService) UpdatePolicy(actorUserID, organizationID int64, req model.UpdateZeroTrustPolicyRequest) (model.ZeroTrustPolicy, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.ZeroTrustPolicy{}, err
	}
	allowed, err := normalizeCIDRs(req.AllowedCIDRs)
	if err != nil {
		return model.ZeroTrustPolicy{}, ErrInvalidZeroTrustPolicy
	}
	denied, err := normalizeCIDRs(req.DeniedCIDRs)
	if err != nil {
		return model.ZeroTrustPolicy{}, ErrInvalidZeroTrustPolicy
	}
	if req.StepUpRiskScore < 1 || req.StepUpRiskScore > 100 ||
		req.RevokeRiskScore < 1 || req.RevokeRiskScore > 100 ||
		req.StepUpRiskScore >= req.RevokeRiskScore ||
		req.ImpossibleTravelKPH < 100 || req.ImpossibleTravelKPH > 5000 ||
		req.WAFBlockScore < 1 || req.WAFBlockScore > 100 ||
		req.AuditCheckpointInterval < 1 || req.AuditCheckpointInterval > 10000 {
		return model.ZeroTrustPolicy{}, ErrInvalidZeroTrustPolicy
	}
	now := time.Now().UTC()
	old, err := s.repo.GetPolicy(organizationID)
	createdAt := now
	if err == nil {
		createdAt = old.CreatedAt
	} else if !errors.Is(err, repository.ErrZeroTrustPolicyNotFound) {
		return model.ZeroTrustPolicy{}, err
	}
	item, err := s.repo.UpsertPolicy(model.ZeroTrustPolicy{
		OrganizationID:          organizationID,
		Enabled:                 req.Enabled,
		RequireWorkloadMTLS:     req.RequireWorkloadMTLS,
		RequireBoundTokens:      req.RequireBoundTokens,
		StepUpRiskScore:         req.StepUpRiskScore,
		RevokeRiskScore:         req.RevokeRiskScore,
		ImpossibleTravelKPH:     req.ImpossibleTravelKPH,
		WAFBlockScore:           req.WAFBlockScore,
		AllowedCIDRs:            allowed,
		DeniedCIDRs:             denied,
		AutoRevokeHighRisk:      req.AutoRevokeHighRisk,
		RequireTrustedDevice:    req.RequireTrustedDevice,
		SIEMFederationEnabled:   req.SIEMFederationEnabled,
		AuditCheckpointInterval: req.AuditCheckpointInterval,
		UpdatedByUserID:         actorUserID,
		CreatedAt:               createdAt,
		UpdatedAt:               now,
	})
	if err == nil {
		s.audit(organizationID, actorUserID, "security.zero_trust_policy.updated", "zero_trust_policy", fmt.Sprint(organizationID), map[string]any{
			"step_up_risk_score": req.StepUpRiskScore,
			"revoke_risk_score":  req.RevokeRiskScore,
			"allowed_cidrs":      allowed,
			"denied_cidrs":       denied,
		})
	}
	return item, err
}

func (s *ZeroTrustService) Workloads(actorUserID, organizationID int64) ([]model.WorkloadIdentity, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListWorkloads(organizationID)
}

func (s *ZeroTrustService) CreateWorkload(actorUserID, organizationID int64, req model.CreateWorkloadIdentityRequest) (model.WorkloadIdentity, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.WorkloadIdentity{}, err
	}
	name := strings.TrimSpace(req.Name)
	spiffeID := strings.TrimSpace(req.SPIFFEID)
	scopes := normalizeSecurityScopes(req.AllowedScopes)
	if name == "" || len(name) > 200 || req.WorkspaceID <= 0 || !validSPIFFEID(spiffeID) || len(scopes) == 0 {
		return model.WorkloadIdentity{}, ErrInvalidWorkloadIdentity
	}
	attached, err := s.workspaceAttached(organizationID, req.WorkspaceID)
	if err != nil {
		return model.WorkloadIdentity{}, err
	}
	if !attached {
		return model.WorkloadIdentity{}, ErrInvalidWorkloadIdentity
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateWorkload(model.WorkloadIdentity{
		OrganizationID:  organizationID,
		WorkspaceID:     req.WorkspaceID,
		Name:            name,
		SPIFFEID:        spiffeID,
		Status:          model.WorkloadIdentityActive,
		AllowedScopes:   scopes,
		MTLSRequired:    req.MTLSRequired,
		CreatedByUserID: actorUserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err == nil {
		s.audit(organizationID, actorUserID, "security.workload.created", "workload_identity", fmt.Sprint(item.ID), map[string]any{
			"workspace_id": item.WorkspaceID,
			"spiffe_id":    item.SPIFFEID,
			"scopes":       item.AllowedScopes,
		})
	}
	return item, err
}

func (s *ZeroTrustService) RevokeWorkload(actorUserID, organizationID, workloadID int64) error {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return err
	}
	if err := s.repo.RevokeWorkload(organizationID, workloadID, time.Now().UTC()); err != nil {
		return err
	}
	s.audit(organizationID, actorUserID, "security.workload.revoked", "workload_identity", fmt.Sprint(workloadID), nil)
	return nil
}

func (s *ZeroTrustService) Certificates(actorUserID, organizationID, workloadID int64) ([]model.WorkloadCertificate, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListCertificates(organizationID, workloadID)
}

func (s *ZeroTrustService) RegisterCertificate(actorUserID, organizationID, workloadID int64, req model.RegisterWorkloadCertificateRequest) (model.WorkloadCertificate, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.WorkloadCertificate{}, err
	}
	workload, err := s.repo.GetWorkload(organizationID, workloadID)
	if err != nil {
		return model.WorkloadCertificate{}, err
	}
	cert, err := parseWorkloadCertificate(req.CertificatePEM, workload.SPIFFEID)
	if err != nil {
		return model.WorkloadCertificate{}, ErrInvalidWorkloadCert
	}
	now := time.Now().UTC()
	if now.Before(cert.NotBefore.Add(-5*time.Minute)) || !now.Before(cert.NotAfter) {
		return model.WorkloadCertificate{}, ErrInvalidWorkloadCert
	}
	sum := sha256.Sum256(cert.Raw)
	item, err := s.repo.CreateCertificate(model.WorkloadCertificate{
		OrganizationID:    organizationID,
		WorkloadID:        workloadID,
		SerialNumber:      cert.SerialNumber.String(),
		SHA256Fingerprint: hex.EncodeToString(sum[:]),
		Subject:           cert.Subject.String(),
		NotBefore:         cert.NotBefore.UTC(),
		NotAfter:          cert.NotAfter.UTC(),
		CreatedByUserID:   actorUserID,
		CreatedAt:         now,
	}, req.ReplaceExisting)
	if err == nil {
		s.audit(organizationID, actorUserID, "security.workload_certificate.rotated", "workload_certificate", fmt.Sprint(item.ID), map[string]any{
			"workload_id": item.WorkloadID,
			"fingerprint": item.SHA256Fingerprint,
			"not_after":   item.NotAfter,
			"replaced":    req.ReplaceExisting,
		})
		_, _ = s.repo.CreateSecurityEvent(model.SecurityEvent{
			OrganizationID: organizationID,
			WorkloadID:     &workloadID,
			Type:           model.SecurityEventCertificate,
			Severity:       model.RiskLevelLow,
			RiskScore:      0,
			Action:         model.ZeroTrustActionAllow,
			Indicators:     []string{"certificate_registered"},
			Metadata:       map[string]any{"fingerprint": item.SHA256Fingerprint, "not_after": item.NotAfter},
			OccurredAt:     now,
		})
	}
	return item, err
}

func (s *ZeroTrustService) ExchangeWorkloadToken(certificateFingerprint string) (model.WorkloadTokenResult, error) {
	fingerprint := strings.ToLower(strings.TrimSpace(certificateFingerprint))
	if len(fingerprint) != 64 {
		return model.WorkloadTokenResult{}, ErrInvalidWorkloadCert
	}
	now := time.Now().UTC()
	workload, cert, err := s.repo.FindActiveCertificateByFingerprint(fingerprint, now)
	if err != nil {
		return model.WorkloadTokenResult{}, err
	}
	policy, err := s.policyOrDefault(workload.OrganizationID)
	if err != nil {
		return model.WorkloadTokenResult{}, err
	}
	if policy.RequireWorkloadMTLS && !workload.MTLSRequired {
		return model.WorkloadTokenResult{}, ErrInvalidWorkloadIdentity
	}
	binding := ""
	if policy.RequireBoundTokens || workload.MTLSRequired {
		binding = cert.SHA256Fingerprint
	}
	token, err := s.tokens.GenerateServiceBound(
		fmt.Sprintf("workload:%d", workload.ID),
		workload.WorkspaceID,
		workload.AllowedScopes,
		workload.CreatedByUserID,
		binding,
	)
	if err != nil {
		return model.WorkloadTokenResult{}, err
	}
	if err := s.repo.TouchWorkloadAuthentication(workload.OrganizationID, workload.ID, now); err != nil {
		return model.WorkloadTokenResult{}, err
	}
	_, _ = s.repo.CreateSecurityEvent(model.SecurityEvent{
		OrganizationID: workload.OrganizationID,
		WorkloadID:     &workload.ID,
		Type:           model.SecurityEventWorkloadAuth,
		Severity:       model.RiskLevelLow,
		RiskScore:      0,
		Action:         model.ZeroTrustActionAllow,
		Indicators:     []string{"mtls_certificate_verified", "certificate_bound_token"},
		Metadata:       map[string]any{"workspace_id": workload.WorkspaceID, "spiffe_id": workload.SPIFFEID},
		OccurredAt:     now,
	})
	return model.WorkloadTokenResult{
		AccessToken:                 token,
		TokenType:                   "Bearer",
		ExpiresIn:                   int64(s.tokens.TTL().Seconds()),
		Scopes:                      append([]string(nil), workload.AllowedScopes...),
		WorkloadID:                  workload.ID,
		BoundCertificateFingerprint: binding,
	}, nil
}

func (s *ZeroTrustService) Devices(actorUserID, organizationID int64) ([]model.DeviceTrust, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListDeviceTrust(organizationID, time.Now().UTC())
}

func (s *ZeroTrustService) TrustDevice(actorUserID, organizationID int64, req model.TrustDeviceRequest) (model.DeviceTrust, error) {
	if _, err := s.requireMember(actorUserID, organizationID); err != nil {
		return model.DeviceTrust{}, err
	}
	deviceHash := strings.ToLower(strings.TrimSpace(req.DeviceHash))
	trustLevel := strings.ToLower(strings.TrimSpace(req.TrustLevel))
	if !isSHA256Hex(deviceHash) || (trustLevel != "trusted" && trustLevel != "managed") ||
		req.ExpiresInHours < 1 || req.ExpiresInHours > 24*365 || len(strings.TrimSpace(req.Label)) > 200 {
		return model.DeviceTrust{}, ErrInvalidDeviceTrust
	}
	now := time.Now().UTC()
	item, err := s.repo.UpsertDeviceTrust(model.DeviceTrust{
		OrganizationID: organizationID,
		UserID:         actorUserID,
		DeviceHash:     deviceHash,
		Label:          strings.TrimSpace(req.Label),
		TrustLevel:     trustLevel,
		LastSeenAt:     now,
		ExpiresAt:      now.Add(time.Duration(req.ExpiresInHours) * time.Hour),
		CreatedAt:      now,
	})
	if err == nil {
		s.audit(organizationID, actorUserID, "security.device.trusted", "device_trust", fmt.Sprint(item.ID), map[string]any{
			"trust_level": item.TrustLevel,
			"expires_at":  item.ExpiresAt,
		})
	}
	return item, err
}

func (s *ZeroTrustService) RevokeDevice(actorUserID, organizationID, deviceID int64) error {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return err
	}
	if err := s.repo.RevokeDeviceTrust(organizationID, deviceID, time.Now().UTC()); err != nil {
		return err
	}
	s.audit(organizationID, actorUserID, "security.device.revoked", "device_trust", fmt.Sprint(deviceID), nil)
	return nil
}

func (s *ZeroTrustService) EvaluateRisk(
	actorUserID, organizationID int64,
	mfaAuthenticated bool,
	req model.RiskEvaluationRequest,
) (model.RiskEvaluation, error) {
	if _, err := s.requireMember(actorUserID, organizationID); err != nil {
		return model.RiskEvaluation{}, err
	}
	if req.AnomalyScore < 0 || req.AnomalyScore > 100 || req.WAFScore < 0 || req.WAFScore > 100 ||
		!validCoordinates(req.PreviousLatitude, req.PreviousLongitude) ||
		!validCoordinates(req.CurrentLatitude, req.CurrentLongitude) {
		return model.RiskEvaluation{}, ErrInvalidRiskEvaluation
	}
	policy, err := s.policyOrDefault(organizationID)
	if err != nil {
		return model.RiskEvaluation{}, err
	}
	now := time.Now().UTC()
	if !policy.Enabled {
		return model.RiskEvaluation{
			OrganizationID: organizationID, UserID: actorUserID, SessionID: req.SessionID,
			RiskScore: 0, RiskLevel: model.RiskLevelLow, Action: model.ZeroTrustActionAllow,
			Indicators: []string{"zero_trust_policy_disabled"}, MFAAuthenticated: mfaAuthenticated,
			EvaluatedAt: now,
		}, nil
	}

	score := 0
	indicators := make([]string, 0, 10)
	severity := model.RiskLevelLow

	if req.SourceIP != "" {
		ip, parseErr := netip.ParseAddr(strings.TrimSpace(req.SourceIP))
		if parseErr != nil {
			return model.RiskEvaluation{}, ErrInvalidRiskEvaluation
		}
		if matchesCIDRs(ip, policy.DeniedCIDRs) {
			score += 100
			indicators = append(indicators, "source_ip_denied")
		} else if len(policy.AllowedCIDRs) > 0 && !matchesCIDRs(ip, policy.AllowedCIDRs) {
			score += 55
			indicators = append(indicators, "source_ip_outside_allowlist")
		}
	}

	if req.DeviceHash != "" {
		deviceHash := strings.ToLower(strings.TrimSpace(req.DeviceHash))
		if !isSHA256Hex(deviceHash) {
			return model.RiskEvaluation{}, ErrInvalidRiskEvaluation
		}
		if _, err := s.repo.GetActiveDeviceTrust(organizationID, actorUserID, deviceHash, now); err != nil {
			if !errors.Is(err, repository.ErrDeviceTrustNotFound) {
				return model.RiskEvaluation{}, err
			}
			score += 25
			indicators = append(indicators, "device_untrusted")
		}
	} else if policy.RequireTrustedDevice {
		score += 30
		indicators = append(indicators, "trusted_device_missing")
	}

	if req.PreviousLatitude != nil && req.PreviousLongitude != nil &&
		req.CurrentLatitude != nil && req.CurrentLongitude != nil && req.PreviousSeenAt != nil {
		elapsed := now.Sub(req.PreviousSeenAt.UTC())
		if elapsed > 0 && elapsed <= 48*time.Hour {
			distanceKM := haversineKM(*req.PreviousLatitude, *req.PreviousLongitude, *req.CurrentLatitude, *req.CurrentLongitude)
			speed := distanceKM / elapsed.Hours()
			if speed > float64(policy.ImpossibleTravelKPH) {
				score += 45
				indicators = append(indicators, "impossible_travel")
			}
		}
	}

	if req.AnomalyScore > 0 {
		score += req.AnomalyScore / 2
		if req.AnomalyScore >= 60 {
			indicators = append(indicators, "behavior_anomaly")
		}
	}
	if req.WAFScore >= policy.WAFBlockScore {
		score += 60
		indicators = append(indicators, "waf_high_risk")
	} else if req.WAFScore >= policy.WAFBlockScore/2 && req.WAFScore > 0 {
		score += 20
		indicators = append(indicators, "waf_elevated_risk")
	}
	if req.TokenBindingValid != nil && !*req.TokenBindingValid {
		score += 80
		indicators = append(indicators, "token_binding_invalid")
	}
	if score > 100 {
		score = 100
	}

	action := model.ZeroTrustActionAllow
	if score >= policy.RevokeRiskScore {
		action = model.ZeroTrustActionRevoke
		severity = model.RiskLevelCritical
	} else if score >= policy.StepUpRiskScore {
		severity = model.RiskLevelHigh
		if mfaAuthenticated {
			indicators = append(indicators, "step_up_satisfied_by_mfa")
		} else {
			action = model.ZeroTrustActionStepUp
		}
	} else if score >= policy.StepUpRiskScore/2 {
		severity = model.RiskLevelMedium
	}

	sessionRevoked := false
	if action == model.ZeroTrustActionRevoke && policy.AutoRevokeHighRisk && req.SessionID > 0 {
		if err := s.refreshes.RevokeByID(actorUserID, req.SessionID, now); err == nil {
			sessionRevoked = true
			indicators = append(indicators, "session_revoked")
		} else if !errors.Is(err, repository.ErrInvalidRefreshToken) {
			return model.RiskEvaluation{}, err
		}
	}

	eventType := model.SecurityEventRiskAssessment
	if containsSecurityIndicator(indicators, "impossible_travel") {
		eventType = model.SecurityEventImpossibleTravel
	} else if containsSecurityIndicator(indicators, "source_ip_denied") || containsSecurityIndicator(indicators, "source_ip_outside_allowlist") {
		eventType = model.SecurityEventNetworkViolation
	} else if containsSecurityIndicator(indicators, "token_binding_invalid") {
		eventType = model.SecurityEventDPoPViolation
	} else if containsSecurityIndicator(indicators, "waf_high_risk") {
		eventType = model.SecurityEventWAFAnomaly
	}
	userID := actorUserID
	var sessionID *int64
	if req.SessionID > 0 {
		sessionID = &req.SessionID
	}
	event, err := s.repo.CreateSecurityEvent(model.SecurityEvent{
		OrganizationID: organizationID,
		UserID:         &userID,
		SessionID:      sessionID,
		Type:           eventType,
		Severity:       severity,
		RiskScore:      score,
		Action:         action,
		SourceIP:       strings.TrimSpace(req.SourceIP),
		Indicators:     indicators,
		Metadata: map[string]any{
			"anomaly_score": req.AnomalyScore,
			"waf_score":     req.WAFScore,
			"mfa":           mfaAuthenticated,
		},
		OccurredAt: now,
	})
	if err != nil {
		return model.RiskEvaluation{}, err
	}
	if sessionRevoked {
		_, _ = s.repo.CreateSecurityEvent(model.SecurityEvent{
			OrganizationID: organizationID,
			UserID:         &userID,
			SessionID:      sessionID,
			Type:           model.SecurityEventSessionRevoked,
			Severity:       model.RiskLevelCritical,
			RiskScore:      score,
			Action:         model.ZeroTrustActionRevoke,
			SourceIP:       strings.TrimSpace(req.SourceIP),
			Indicators:     []string{"automatic_high_risk_revocation"},
			Metadata:       map[string]any{"trigger_event_id": event.ID},
			OccurredAt:     now,
		})
	}
	s.audit(organizationID, actorUserID, "security.risk.evaluated", "security_event", fmt.Sprint(event.ID), map[string]any{
		"risk_score": score, "action": action, "session_revoked": sessionRevoked,
	})
	return model.RiskEvaluation{
		OrganizationID:   organizationID,
		UserID:           actorUserID,
		SessionID:        req.SessionID,
		RiskScore:        score,
		RiskLevel:        severity,
		Action:           action,
		Indicators:       indicators,
		MFAAuthenticated: mfaAuthenticated,
		SessionRevoked:   sessionRevoked,
		EvaluatedAt:      now,
	}, nil
}

func (s *ZeroTrustService) Events(actorUserID, organizationID, afterID, limit int64) ([]model.SecurityEvent, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	if afterID < 0 || limit < 1 || limit > 500 {
		return nil, ErrInvalidRiskEvaluation
	}
	return s.repo.ListSecurityEvents(organizationID, afterID, int(limit))
}

func (s *ZeroTrustService) Checkpoints(actorUserID, organizationID int64) ([]model.AuditCheckpoint, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListCheckpoints(organizationID, 100)
}

func (s *ZeroTrustService) CreateCheckpoint(actorUserID, organizationID int64) (model.AuditCheckpoint, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.AuditCheckpoint{}, err
	}
	policy, err := s.policyOrDefault(organizationID)
	if err != nil {
		return model.AuditCheckpoint{}, err
	}
	previousHash := strings.Repeat("0", 64)
	sequence := int64(1)
	afterID := int64(0)
	latest, err := s.repo.LatestCheckpoint(organizationID)
	if err == nil {
		previousHash = latest.CheckpointHash
		sequence = latest.Sequence + 1
		afterID = latest.LastEventID
	} else if !errors.Is(err, repository.ErrAuditCheckpointNotFound) {
		return model.AuditCheckpoint{}, err
	}
	events, err := s.repo.ListSecurityEvents(organizationID, afterID, policy.AuditCheckpointInterval)
	if err != nil {
		return model.AuditCheckpoint{}, err
	}
	if len(events) == 0 {
		return model.AuditCheckpoint{}, ErrNoSecurityEvents
	}
	payload, err := json.Marshal(events)
	if err != nil {
		return model.AuditCheckpoint{}, err
	}
	payloadHash := sha256.Sum256(payload)
	chainMaterial := fmt.Sprintf("%d:%d:%s:%s", organizationID, sequence, previousHash, hex.EncodeToString(payloadHash[:]))
	checkpointHash := sha256.Sum256([]byte(chainMaterial))
	checkpointHex := hex.EncodeToString(checkpointHash[:])
	signature := s.signCheckpoint(checkpointHex)
	now := time.Now().UTC()
	item, err := s.repo.CreateCheckpoint(model.AuditCheckpoint{
		OrganizationID:  organizationID,
		Sequence:        sequence,
		FirstEventID:    events[0].ID,
		LastEventID:     events[len(events)-1].ID,
		EventCount:      len(events),
		PreviousHash:    previousHash,
		PayloadHash:     hex.EncodeToString(payloadHash[:]),
		CheckpointHash:  checkpointHex,
		Signature:       signature,
		CreatedByUserID: actorUserID,
		CreatedAt:       now,
	})
	if err == nil {
		s.audit(organizationID, actorUserID, "security.audit_checkpoint.created", "audit_checkpoint", fmt.Sprint(item.ID), map[string]any{
			"sequence": item.Sequence, "event_count": item.EventCount, "checkpoint_hash": item.CheckpointHash,
		})
	}
	return item, err
}

func (s *ZeroTrustService) CreateWORMExport(actorUserID, organizationID int64) (model.WORMAuditExport, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.WORMAuditExport{}, err
	}
	checkpoint, err := s.repo.LatestCheckpoint(organizationID)
	if err != nil {
		return model.WORMAuditExport{}, err
	}
	events, err := s.repo.ListSecurityEvents(organizationID, checkpoint.FirstEventID-1, checkpoint.EventCount)
	if err != nil {
		return model.WORMAuditExport{}, err
	}
	if len(events) != checkpoint.EventCount || events[len(events)-1].ID != checkpoint.LastEventID {
		return model.WORMAuditExport{}, ErrNoSecurityEvents
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateWORMExport(model.WORMAuditExport{
		OrganizationID:  organizationID,
		FromEventID:     checkpoint.FirstEventID,
		ToEventID:       checkpoint.LastEventID,
		EventCount:      checkpoint.EventCount,
		RootHash:        checkpoint.PayloadHash,
		CheckpointHash:  checkpoint.CheckpointHash,
		ObjectURI:       fmt.Sprintf("worm://organization/%d/security-audit/%d", organizationID, checkpoint.Sequence),
		CreatedByUserID: actorUserID,
		CreatedAt:       now,
	})
	if err == nil {
		s.audit(organizationID, actorUserID, "security.worm_export.created", "worm_audit_export", fmt.Sprint(item.ID), map[string]any{
			"object_uri": item.ObjectURI, "checkpoint_hash": item.CheckpointHash,
		})
	}
	return item, err
}

func (s *ZeroTrustService) SIEMDestinations(actorUserID, organizationID int64) ([]model.SIEMDestination, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListSIEMDestinations(organizationID)
}

func (s *ZeroTrustService) CreateSIEMDestination(actorUserID, organizationID int64, req model.CreateSIEMDestinationRequest) (model.SIEMDestination, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.SIEMDestination{}, err
	}
	name := strings.TrimSpace(req.Name)
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	endpoint := strings.TrimSpace(req.EndpointURL)
	u, err := url.Parse(endpoint)
	if name == "" || provider == "" || err != nil || u.Scheme != "https" || u.Host == "" ||
		len(name) > 200 || len(provider) > 80 || len(req.SecretRef) > 500 {
		return model.SIEMDestination{}, ErrInvalidSIEMDestination
	}
	eventTypes := normalizeSecurityStrings(req.EventTypes)
	now := time.Now().UTC()
	item, err := s.repo.CreateSIEMDestination(model.SIEMDestination{
		OrganizationID:  organizationID,
		Name:            name,
		Provider:        provider,
		EndpointURL:     endpoint,
		EventTypes:      eventTypes,
		Enabled:         req.Enabled,
		SecretRef:       strings.TrimSpace(req.SecretRef),
		CreatedByUserID: actorUserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err == nil {
		s.audit(organizationID, actorUserID, "security.siem_destination.created", "siem_destination", fmt.Sprint(item.ID), map[string]any{
			"provider": provider, "endpoint_url": endpoint, "event_types": eventTypes,
		})
	}
	return item, err
}

func (s *ZeroTrustService) SIEMFeed(actorUserID, organizationID, afterID int64) ([]model.SecurityEvent, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	policy, err := s.policyOrDefault(organizationID)
	if err != nil {
		return nil, err
	}
	if !policy.SIEMFederationEnabled {
		return nil, ErrSIEMFederationDisabled
	}
	if afterID < 0 {
		return nil, ErrInvalidRiskEvaluation
	}
	return s.repo.ListSecurityEvents(organizationID, afterID, 500)
}

func (s *ZeroTrustService) Dashboard(actorUserID, organizationID int64) (model.SecurityDashboard, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.SecurityDashboard{}, err
	}
	policy, err := s.policyOrDefault(organizationID)
	if err != nil {
		return model.SecurityDashboard{}, err
	}
	now := time.Now().UTC()
	workloads, err := s.repo.ListWorkloads(organizationID)
	if err != nil {
		return model.SecurityDashboard{}, err
	}
	certs, err := s.repo.ListOrganizationCertificates(organizationID)
	if err != nil {
		return model.SecurityDashboard{}, err
	}
	devices, err := s.repo.ListDeviceTrust(organizationID, now)
	if err != nil {
		return model.SecurityDashboard{}, err
	}
	events, err := s.repo.ListSecurityEvents(organizationID, 0, 500)
	if err != nil {
		return model.SecurityDashboard{}, err
	}
	activeWorkloads := 0
	for _, workload := range workloads {
		if workload.Status == model.WorkloadIdentityActive && workload.RevokedAt == nil {
			activeWorkloads++
		}
	}
	expiring := 0
	for _, cert := range certs {
		if cert.RevokedAt == nil && cert.NotAfter.After(now) && cert.NotAfter.Before(now.Add(30*24*time.Hour)) {
			expiring++
		}
	}
	highRisk := 0
	revokedSessions := 0
	for _, event := range events {
		if event.RiskScore >= policy.StepUpRiskScore {
			highRisk++
		}
		if event.Type == model.SecurityEventSessionRevoked {
			revokedSessions++
		}
	}
	recent := make([]model.SecurityEvent, 0, 20)
	for i := len(events) - 1; i >= 0 && len(recent) < 20; i-- {
		recent = append(recent, events[i])
	}
	var latestPtr *model.AuditCheckpoint
	latest, err := s.repo.LatestCheckpoint(organizationID)
	if err == nil {
		latestPtr = &latest
	} else if !errors.Is(err, repository.ErrAuditCheckpointNotFound) {
		return model.SecurityDashboard{}, err
	}
	return model.SecurityDashboard{
		OrganizationID:       organizationID,
		Policy:               policy,
		ActiveWorkloads:      activeWorkloads,
		CertificatesExpiring: expiring,
		TrustedDevices:       len(devices),
		HighRiskEvents:       highRisk,
		RevokedSessions:      revokedSessions,
		LatestCheckpoint:     latestPtr,
		RecentEvents:         recent,
		GeneratedAt:          now,
	}, nil
}

func (s *ZeroTrustService) policyOrDefault(organizationID int64) (model.ZeroTrustPolicy, error) {
	item, err := s.repo.GetPolicy(organizationID)
	if errors.Is(err, repository.ErrZeroTrustPolicyNotFound) {
		return defaultZeroTrustPolicy(organizationID), nil
	}
	return item, err
}

func (s *ZeroTrustService) workspaceAttached(organizationID, workspaceID int64) (bool, error) {
	items, err := s.orgs.ListWorkspaces(organizationID)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if item.WorkspaceID == workspaceID {
			return true, nil
		}
	}
	return false, nil
}

func (s *ZeroTrustService) requireAdmin(userID, organizationID int64) (model.Organization, error) {
	org, err := s.orgs.GetOrganization(organizationID)
	if err != nil {
		return model.Organization{}, err
	}
	member, err := s.orgs.GetMember(organizationID, userID)
	if err != nil {
		return model.Organization{}, ErrZeroTrustForbidden
	}
	if org.Status != model.OrganizationStatusActive {
		return model.Organization{}, ErrZeroTrustForbidden
	}
	if member.Role != model.OrganizationRoleOwner && member.Role != model.OrganizationRoleAdmin &&
		member.Role != model.OrganizationRoleDelegatedAdmin {
		return model.Organization{}, ErrZeroTrustForbidden
	}
	return org, nil
}

func (s *ZeroTrustService) requireMember(userID, organizationID int64) (model.Organization, error) {
	org, err := s.orgs.GetOrganization(organizationID)
	if err != nil {
		return model.Organization{}, err
	}
	if org.Status != model.OrganizationStatusActive {
		return model.Organization{}, ErrZeroTrustMemberRequired
	}
	if _, err := s.orgs.GetMember(organizationID, userID); err != nil {
		return model.Organization{}, ErrZeroTrustMemberRequired
	}
	return org, nil
}

func (s *ZeroTrustService) audit(organizationID, actorUserID int64, action, resourceType, resourceID string, metadata map[string]any) {
	uid := actorUserID
	_ = s.orgs.RecordAudit(model.OrganizationAuditEvent{
		OrganizationID: organizationID,
		ActorUserID:    &uid,
		Action:         action,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Metadata:       metadata,
		CreatedAt:      time.Now().UTC(),
	})
}

func (s *ZeroTrustService) signCheckpoint(checkpointHash string) string {
	mac := hmac.New(sha256.New, s.signingKey)
	_, _ = mac.Write([]byte(checkpointHash))
	return hex.EncodeToString(mac.Sum(nil))
}

func normalizeCIDRs(values []string) ([]string, error) {
	seen := make(map[string]bool)
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, err
		}
		value = prefix.Masked().String()
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out, nil
}

func matchesCIDRs(ip netip.Addr, cidrs []string) bool {
	for _, raw := range cidrs {
		prefix, err := netip.ParsePrefix(raw)
		if err == nil && prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func validSPIFFEID(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "spiffe" && u.Host != "" && strings.HasPrefix(u.Path, "/") && u.RawQuery == "" && u.Fragment == ""
}

func parseWorkloadCertificate(rawPEM, expectedSPIFFEID string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(rawPEM)))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, ErrInvalidWorkloadCert
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	found := false
	for _, uri := range cert.URIs {
		if uri.String() == expectedSPIFFEID {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrInvalidWorkloadCert
	}
	if len(cert.ExtKeyUsage) > 0 {
		clientAuth := false
		for _, usage := range cert.ExtKeyUsage {
			if usage == x509.ExtKeyUsageClientAuth || usage == x509.ExtKeyUsageAny {
				clientAuth = true
				break
			}
		}
		if !clientAuth {
			return nil, ErrInvalidWorkloadCert
		}
	}
	return cert, nil
}

func normalizeSecurityScopes(values []string) []string {
	allowed := map[string]bool{
		model.ScopeTasksRead: true, model.ScopeTasksWrite: true, model.ScopeWorkspaceRead: true,
		model.ScopeWorkspaceAdmin: true, model.ScopeAuditRead: true, model.ScopeIdentityAdmin: true,
	}
	seen := make(map[string]bool)
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if allowed[value] && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func isSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validCoordinates(lat, lon *float64) bool {
	if lat == nil && lon == nil {
		return true
	}
	return lat != nil && lon != nil && *lat >= -90 && *lat <= 90 && *lon >= -180 && *lon <= 180
}

func haversineKM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKM = 6371.0
	toRad := func(v float64) float64 { return v * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusKM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func containsSecurityIndicator(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func normalizeSecurityStrings(values []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
