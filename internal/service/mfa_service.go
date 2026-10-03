package service

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrMFAInvalidCode = errors.New("invalid mfa code")
	ErrMFANotEnrolled = errors.New("mfa is not enrolled")
)

type WebAuthnCredentialChecker interface {
	HasCredentials(userID int64) (bool, error)
}

type MFAService struct {
	repo     repository.MFARepository
	users    repository.UserRepository
	cipher   *auth.SecretCipher
	issuer   string
	webauthn WebAuthnCredentialChecker
}

func NewMFAService(
	repo repository.MFARepository,
	users repository.UserRepository,
	cipher *auth.SecretCipher,
	issuer string,
) *MFAService {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		issuer = "Simple Task Manager"
	}
	return &MFAService{repo: repo, users: users, cipher: cipher, issuer: issuer}
}

func (s *MFAService) SetWebAuthnCredentialChecker(checker WebAuthnCredentialChecker) {
	s.webauthn = checker
}

func (s *MFAService) BeginTOTP(userID int64) (model.TOTPEnrollResult, error) {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return model.TOTPEnrollResult{}, err
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		return model.TOTPEnrollResult{}, err
	}
	encrypted, err := s.cipher.Encrypt(secret)
	if err != nil {
		return model.TOTPEnrollResult{}, err
	}
	now := time.Now()
	if err := s.repo.UpsertTOTP(model.TOTPCredential{
		UserID:           userID,
		SecretCiphertext: encrypted,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		return model.TOTPEnrollResult{}, err
	}

	label := url.QueryEscape(fmt.Sprintf("%s:%s", s.issuer, user.Email))
	issuer := url.QueryEscape(s.issuer)
	uri := fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30", label, secret, issuer)
	return model.TOTPEnrollResult{Secret: secret, OTPAuthURI: uri}, nil
}

func (s *MFAService) ConfirmTOTP(userID int64, code string) error {
	credential, secret, err := s.credentialSecret(userID)
	if err != nil {
		return err
	}
	if credential.ConfirmedAt != nil {
		return nil
	}
	if !auth.VerifyTOTP(secret, code, time.Now()) {
		return ErrMFAInvalidCode
	}
	return s.repo.ConfirmTOTP(userID, time.Now())
}

func (s *MFAService) Enabled(userID int64) (bool, error) {
	credential, err := s.repo.GetTOTP(userID)
	if err != nil && !errors.Is(err, repository.ErrTOTPCredentialNotFound) {
		return false, err
	}
	if err == nil && credential.ConfirmedAt != nil {
		return true, nil
	}
	if s.webauthn != nil {
		return s.webauthn.HasCredentials(userID)
	}
	return false, nil
}

func (s *MFAService) Verify(userID int64, code string) (bool, error) {
	credential, secret, err := s.credentialSecret(userID)
	if errors.Is(err, repository.ErrTOTPCredentialNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if credential.ConfirmedAt == nil {
		return false, nil
	}
	return auth.VerifyTOTP(secret, code, time.Now()), nil
}

func (s *MFAService) DisableTOTP(userID int64, code string) error {
	enabled, err := s.Enabled(userID)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrMFANotEnrolled
	}
	ok, err := s.Verify(userID, code)
	if err != nil {
		return err
	}
	if !ok {
		return ErrMFAInvalidCode
	}
	return s.repo.DeleteTOTP(userID)
}

func (s *MFAService) Status(userID int64) (model.MFAStatus, error) {
	totpEnabled := false
	credential, err := s.repo.GetTOTP(userID)
	if err != nil && !errors.Is(err, repository.ErrTOTPCredentialNotFound) {
		return model.MFAStatus{}, err
	}
	if err == nil {
		totpEnabled = credential.ConfirmedAt != nil
	}

	webAuthnAvailable := false
	if s.webauthn != nil {
		webAuthnAvailable, err = s.webauthn.HasCredentials(userID)
		if err != nil {
			return model.MFAStatus{}, err
		}
	}
	return model.MFAStatus{
		TOTPEnabled:       totpEnabled,
		WebAuthnAvailable: webAuthnAvailable,
	}, nil
}

func (s *MFAService) credentialSecret(userID int64) (model.TOTPCredential, string, error) {
	credential, err := s.repo.GetTOTP(userID)
	if err != nil {
		return model.TOTPCredential{}, "", err
	}
	secret, err := s.cipher.Decrypt(credential.SecretCiphertext)
	if err != nil {
		return model.TOTPCredential{}, "", err
	}
	return credential, secret, nil
}
