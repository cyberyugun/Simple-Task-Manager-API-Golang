package repository

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrZeroTrustPolicyNotFound      = errors.New("zero trust policy not found")
	ErrWorkloadIdentityNotFound     = errors.New("workload identity not found")
	ErrWorkloadCertificateNotFound  = errors.New("workload certificate not found")
	ErrDeviceTrustNotFound          = errors.New("device trust record not found")
	ErrAuditCheckpointNotFound      = errors.New("audit checkpoint not found")
	ErrSIEMDestinationNotFound      = errors.New("siem destination not found")
)

type ZeroTrustRepository interface {
	GetPolicy(organizationID int64) (model.ZeroTrustPolicy, error)
	UpsertPolicy(item model.ZeroTrustPolicy) (model.ZeroTrustPolicy, error)

	CreateWorkload(item model.WorkloadIdentity) (model.WorkloadIdentity, error)
	GetWorkload(organizationID, workloadID int64) (model.WorkloadIdentity, error)
	ListWorkloads(organizationID int64) ([]model.WorkloadIdentity, error)
	RevokeWorkload(organizationID, workloadID int64, now time.Time) error
	TouchWorkloadAuthentication(organizationID, workloadID int64, now time.Time) error

	CreateCertificate(item model.WorkloadCertificate, replaceExisting bool) (model.WorkloadCertificate, error)
	ListCertificates(organizationID, workloadID int64) ([]model.WorkloadCertificate, error)
	ListOrganizationCertificates(organizationID int64) ([]model.WorkloadCertificate, error)
	FindActiveCertificateByFingerprint(fingerprint string, now time.Time) (model.WorkloadIdentity, model.WorkloadCertificate, error)

	UpsertDeviceTrust(item model.DeviceTrust) (model.DeviceTrust, error)
	GetActiveDeviceTrust(organizationID, userID int64, deviceHash string, now time.Time) (model.DeviceTrust, error)
	ListDeviceTrust(organizationID int64, now time.Time) ([]model.DeviceTrust, error)
	RevokeDeviceTrust(organizationID, deviceID int64, now time.Time) error

	CreateSecurityEvent(item model.SecurityEvent) (model.SecurityEvent, error)
	ListSecurityEvents(organizationID, afterID int64, limit int) ([]model.SecurityEvent, error)

	CreateCheckpoint(item model.AuditCheckpoint) (model.AuditCheckpoint, error)
	LatestCheckpoint(organizationID int64) (model.AuditCheckpoint, error)
	ListCheckpoints(organizationID int64, limit int) ([]model.AuditCheckpoint, error)

	CreateWORMExport(item model.WORMAuditExport) (model.WORMAuditExport, error)

	CreateSIEMDestination(item model.SIEMDestination) (model.SIEMDestination, error)
	ListSIEMDestinations(organizationID int64) ([]model.SIEMDestination, error)
}

type InMemoryZeroTrustRepository struct {
	mu sync.Mutex

	policies     map[int64]model.ZeroTrustPolicy
	workloads    map[int64]model.WorkloadIdentity
	certificates map[int64]model.WorkloadCertificate
	devices      map[int64]model.DeviceTrust
	events       map[int64]model.SecurityEvent
	checkpoints  map[int64]model.AuditCheckpoint
	exports      map[int64]model.WORMAuditExport
	siem         map[int64]model.SIEMDestination

	nextWorkload   int64
	nextCert       int64
	nextDevice     int64
	nextEvent      int64
	nextCheckpoint int64
	nextExport     int64
	nextSIEM       int64
}

func NewInMemoryZeroTrustRepository() *InMemoryZeroTrustRepository {
	return &InMemoryZeroTrustRepository{
		policies:       make(map[int64]model.ZeroTrustPolicy),
		workloads:      make(map[int64]model.WorkloadIdentity),
		certificates:   make(map[int64]model.WorkloadCertificate),
		devices:        make(map[int64]model.DeviceTrust),
		events:         make(map[int64]model.SecurityEvent),
		checkpoints:    make(map[int64]model.AuditCheckpoint),
		exports:        make(map[int64]model.WORMAuditExport),
		siem:           make(map[int64]model.SIEMDestination),
		nextWorkload:   1,
		nextCert:       1,
		nextDevice:     1,
		nextEvent:      1,
		nextCheckpoint: 1,
		nextExport:     1,
		nextSIEM:       1,
	}
}

func (r *InMemoryZeroTrustRepository) GetPolicy(organizationID int64) (model.ZeroTrustPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.policies[organizationID]
	if !ok {
		return model.ZeroTrustPolicy{}, ErrZeroTrustPolicyNotFound
	}
	return cloneZeroTrustPolicy(item), nil
}

func (r *InMemoryZeroTrustRepository) UpsertPolicy(item model.ZeroTrustPolicy) (model.ZeroTrustPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.policies[item.OrganizationID]; ok {
		item.CreatedAt = old.CreatedAt
	}
	item = cloneZeroTrustPolicy(item)
	r.policies[item.OrganizationID] = item
	return cloneZeroTrustPolicy(item), nil
}

func (r *InMemoryZeroTrustRepository) CreateWorkload(item model.WorkloadIdentity) (model.WorkloadIdentity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.workloads {
		if existing.OrganizationID == item.OrganizationID && existing.SPIFFEID == item.SPIFFEID && existing.RevokedAt == nil {
			return model.WorkloadIdentity{}, errors.New("workload spiffe id already exists")
		}
	}
	item.ID = r.nextWorkload
	r.nextWorkload++
	item.AllowedScopes = append([]string(nil), item.AllowedScopes...)
	r.workloads[item.ID] = item
	return cloneWorkload(item), nil
}

func (r *InMemoryZeroTrustRepository) GetWorkload(organizationID, workloadID int64) (model.WorkloadIdentity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.workloads[workloadID]
	if !ok || item.OrganizationID != organizationID {
		return model.WorkloadIdentity{}, ErrWorkloadIdentityNotFound
	}
	return cloneWorkload(item), nil
}

func (r *InMemoryZeroTrustRepository) ListWorkloads(organizationID int64) ([]model.WorkloadIdentity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.WorkloadIdentity, 0)
	for _, item := range r.workloads {
		if item.OrganizationID == organizationID {
			items = append(items, cloneWorkload(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryZeroTrustRepository) RevokeWorkload(organizationID, workloadID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.workloads[workloadID]
	if !ok || item.OrganizationID != organizationID {
		return ErrWorkloadIdentityNotFound
	}
	item.Status = model.WorkloadIdentityRevoked
	item.RevokedAt = &now
	item.UpdatedAt = now
	r.workloads[workloadID] = item
	for id, cert := range r.certificates {
		if cert.WorkloadID == workloadID && cert.RevokedAt == nil {
			revoked := now
			cert.RevokedAt = &revoked
			r.certificates[id] = cert
		}
	}
	return nil
}

func (r *InMemoryZeroTrustRepository) TouchWorkloadAuthentication(organizationID, workloadID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.workloads[workloadID]
	if !ok || item.OrganizationID != organizationID {
		return ErrWorkloadIdentityNotFound
	}
	item.LastAuthenticatedAt = &now
	item.UpdatedAt = now
	r.workloads[workloadID] = item
	return nil
}

func (r *InMemoryZeroTrustRepository) CreateCertificate(item model.WorkloadCertificate, replaceExisting bool) (model.WorkloadCertificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.certificates {
		if existing.SHA256Fingerprint == item.SHA256Fingerprint {
			return model.WorkloadCertificate{}, errors.New("certificate fingerprint already exists")
		}
	}
	item.ID = r.nextCert
	r.nextCert++
	if replaceExisting {
		for id, existing := range r.certificates {
			if existing.WorkloadID == item.WorkloadID && existing.RevokedAt == nil {
				revoked := item.CreatedAt
				existing.RevokedAt = &revoked
				replacement := item.ID
				existing.ReplacedByID = &replacement
				r.certificates[id] = existing
			}
		}
	}
	r.certificates[item.ID] = item
	return item, nil
}

func (r *InMemoryZeroTrustRepository) ListCertificates(organizationID, workloadID int64) ([]model.WorkloadCertificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if workload, ok := r.workloads[workloadID]; !ok || workload.OrganizationID != organizationID {
		return nil, ErrWorkloadIdentityNotFound
	}
	items := make([]model.WorkloadCertificate, 0)
	for _, item := range r.certificates {
		if item.OrganizationID == organizationID && item.WorkloadID == workloadID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return items, nil
}

func (r *InMemoryZeroTrustRepository) ListOrganizationCertificates(organizationID int64) ([]model.WorkloadCertificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.WorkloadCertificate, 0)
	for _, item := range r.certificates {
		if item.OrganizationID == organizationID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return items, nil
}

func (r *InMemoryZeroTrustRepository) FindActiveCertificateByFingerprint(fingerprint string, now time.Time) (model.WorkloadIdentity, model.WorkloadCertificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, cert := range r.certificates {
		if cert.SHA256Fingerprint != fingerprint || cert.RevokedAt != nil || now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
			continue
		}
		workload, ok := r.workloads[cert.WorkloadID]
		if !ok || workload.Status != model.WorkloadIdentityActive || workload.RevokedAt != nil {
			continue
		}
		return cloneWorkload(workload), cert, nil
	}
	return model.WorkloadIdentity{}, model.WorkloadCertificate{}, ErrWorkloadCertificateNotFound
}

func (r *InMemoryZeroTrustRepository) UpsertDeviceTrust(item model.DeviceTrust) (model.DeviceTrust, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, old := range r.devices {
		if old.OrganizationID == item.OrganizationID && old.UserID == item.UserID && old.DeviceHash == item.DeviceHash {
			item.ID = id
			item.CreatedAt = old.CreatedAt
			r.devices[id] = item
			return item, nil
		}
	}
	item.ID = r.nextDevice
	r.nextDevice++
	r.devices[item.ID] = item
	return item, nil
}

func (r *InMemoryZeroTrustRepository) GetActiveDeviceTrust(organizationID, userID int64, deviceHash string, now time.Time) (model.DeviceTrust, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.devices {
		if item.OrganizationID == organizationID && item.UserID == userID && item.DeviceHash == deviceHash &&
			item.RevokedAt == nil && item.ExpiresAt.After(now) {
			return item, nil
		}
	}
	return model.DeviceTrust{}, ErrDeviceTrustNotFound
}

func (r *InMemoryZeroTrustRepository) ListDeviceTrust(organizationID int64, now time.Time) ([]model.DeviceTrust, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.DeviceTrust, 0)
	for _, item := range r.devices {
		if item.OrganizationID == organizationID && item.RevokedAt == nil && item.ExpiresAt.After(now) {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return items, nil
}

func (r *InMemoryZeroTrustRepository) RevokeDeviceTrust(organizationID, deviceID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.devices[deviceID]
	if !ok || item.OrganizationID != organizationID {
		return ErrDeviceTrustNotFound
	}
	item.RevokedAt = &now
	r.devices[deviceID] = item
	return nil
}

func (r *InMemoryZeroTrustRepository) CreateSecurityEvent(item model.SecurityEvent) (model.SecurityEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextEvent
	r.nextEvent++
	item.Indicators = append([]string(nil), item.Indicators...)
	item.Metadata = cloneZeroTrustMap(item.Metadata)
	r.events[item.ID] = item
	return cloneSecurityEvent(item), nil
}

func (r *InMemoryZeroTrustRepository) ListSecurityEvents(organizationID, afterID int64, limit int) ([]model.SecurityEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.SecurityEvent, 0)
	for _, item := range r.events {
		if item.OrganizationID == organizationID && item.ID > afterID {
			items = append(items, cloneSecurityEvent(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryZeroTrustRepository) CreateCheckpoint(item model.AuditCheckpoint) (model.AuditCheckpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextCheckpoint
	r.nextCheckpoint++
	r.checkpoints[item.ID] = item
	return item, nil
}

func (r *InMemoryZeroTrustRepository) LatestCheckpoint(organizationID int64) (model.AuditCheckpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var latest model.AuditCheckpoint
	found := false
	for _, item := range r.checkpoints {
		if item.OrganizationID == organizationID && (!found || item.Sequence > latest.Sequence) {
			latest = item
			found = true
		}
	}
	if !found {
		return model.AuditCheckpoint{}, ErrAuditCheckpointNotFound
	}
	return latest, nil
}

func (r *InMemoryZeroTrustRepository) ListCheckpoints(organizationID int64, limit int) ([]model.AuditCheckpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.AuditCheckpoint, 0)
	for _, item := range r.checkpoints {
		if item.OrganizationID == organizationID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Sequence > items[j].Sequence })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryZeroTrustRepository) CreateWORMExport(item model.WORMAuditExport) (model.WORMAuditExport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextExport
	r.nextExport++
	if item.ObjectURI == "" {
		item.ObjectURI = "worm://security-audit/" + fmtInt64ZeroTrust(item.OrganizationID) + "/" + fmtInt64ZeroTrust(item.ID)
	}
	r.exports[item.ID] = item
	return item, nil
}

func (r *InMemoryZeroTrustRepository) CreateSIEMDestination(item model.SIEMDestination) (model.SIEMDestination, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextSIEM
	r.nextSIEM++
	item.EventTypes = append([]string(nil), item.EventTypes...)
	r.siem[item.ID] = item
	return cloneSIEM(item), nil
}

func (r *InMemoryZeroTrustRepository) ListSIEMDestinations(organizationID int64) ([]model.SIEMDestination, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.SIEMDestination, 0)
	for _, item := range r.siem {
		if item.OrganizationID == organizationID {
			items = append(items, cloneSIEM(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func cloneZeroTrustPolicy(item model.ZeroTrustPolicy) model.ZeroTrustPolicy {
	item.AllowedCIDRs = append([]string(nil), item.AllowedCIDRs...)
	item.DeniedCIDRs = append([]string(nil), item.DeniedCIDRs...)
	return item
}

func cloneWorkload(item model.WorkloadIdentity) model.WorkloadIdentity {
	item.AllowedScopes = append([]string(nil), item.AllowedScopes...)
	return item
}

func cloneSecurityEvent(item model.SecurityEvent) model.SecurityEvent {
	item.Indicators = append([]string(nil), item.Indicators...)
	item.Metadata = cloneZeroTrustMap(item.Metadata)
	return item
}

func cloneSIEM(item model.SIEMDestination) model.SIEMDestination {
	item.EventTypes = append([]string(nil), item.EventTypes...)
	return item
}

func cloneZeroTrustMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func fmtInt64ZeroTrust(v int64) string {
	if v == 0 {
		return "0"
	}
	const digits = "0123456789"
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = digits[v%10]
		v /= 10
	}
	return string(b[i:])
}

func normalizeZeroTrustHash(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
