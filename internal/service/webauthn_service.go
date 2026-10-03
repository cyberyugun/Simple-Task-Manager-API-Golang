package service

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

const webAuthnCeremonyTTL = 5 * time.Minute

var (
	ErrWebAuthnUnavailable = errors.New("webauthn credential is not available")
	ErrWebAuthnCeremony    = errors.New("invalid or expired webauthn ceremony")
)

type webAuthnUser struct {
	user        model.User
	handle      []byte
	credentials []webauthn.Credential
}

func (u webAuthnUser) WebAuthnID() []byte {
	return append([]byte(nil), u.handle...)
}

func (u webAuthnUser) WebAuthnName() string {
	return u.user.Email
}

func (u webAuthnUser) WebAuthnDisplayName() string {
	return u.user.Name
}

func (u webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return append([]webauthn.Credential(nil), u.credentials...)
}

type WebAuthnService struct {
	repo     repository.WebAuthnRepository
	users    repository.UserRepository
	verifier *webauthn.WebAuthn
}

func NewWebAuthnService(
	repo repository.WebAuthnRepository,
	users repository.UserRepository,
	rpID string,
	rpOrigins []string,
	rpDisplayName string,
) (*WebAuthnService, error) {
	verifier, err := webauthn.New(&webauthn.Config{
		RPID:          strings.TrimSpace(rpID),
		RPOrigins:     append([]string(nil), rpOrigins...),
		RPDisplayName: strings.TrimSpace(rpDisplayName),
	})
	if err != nil {
		return nil, err
	}
	return &WebAuthnService{repo: repo, users: users, verifier: verifier}, nil
}

func (s *WebAuthnService) BeginRegistration(userID int64) (model.WebAuthnCeremonyResult, error) {
	user, err := s.loadUser(userID)
	if err != nil {
		return model.WebAuthnCeremonyResult{}, err
	}
	options, session, err := s.verifier.BeginRegistration(user)
	if err != nil {
		return model.WebAuthnCeremonyResult{}, err
	}
	sessionID, err := s.storeSession(userID, "registration", *session)
	if err != nil {
		return model.WebAuthnCeremonyResult{}, err
	}
	return model.WebAuthnCeremonyResult{SessionID: sessionID, Options: options}, nil
}

func (s *WebAuthnService) FinishRegistration(sessionID string, request *http.Request) (string, error) {
	userID, session, err := s.consumeSession(sessionID, "registration")
	if err != nil {
		return "", err
	}
	user, err := s.loadUser(userID)
	if err != nil {
		return "", err
	}
	credential, err := s.verifier.FinishRegistration(user, session, request)
	if err != nil {
		return "", err
	}
	if err := s.repo.SaveCredential(userID, *credential, time.Now()); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(credential.ID), nil
}

func (s *WebAuthnService) BeginLogin(userID int64) (model.WebAuthnCeremonyResult, error) {
	hasCredentials, err := s.repo.HasCredentials(userID)
	if err != nil {
		return model.WebAuthnCeremonyResult{}, err
	}
	if !hasCredentials {
		return model.WebAuthnCeremonyResult{}, ErrWebAuthnUnavailable
	}
	user, err := s.loadUser(userID)
	if err != nil {
		return model.WebAuthnCeremonyResult{}, err
	}
	options, session, err := s.verifier.BeginLogin(user)
	if err != nil {
		return model.WebAuthnCeremonyResult{}, err
	}
	sessionID, err := s.storeSession(userID, "authentication", *session)
	if err != nil {
		return model.WebAuthnCeremonyResult{}, err
	}
	return model.WebAuthnCeremonyResult{SessionID: sessionID, Options: options}, nil
}

func (s *WebAuthnService) FinishLogin(sessionID string, request *http.Request) (int64, error) {
	userID, session, err := s.consumeSession(sessionID, "authentication")
	if err != nil {
		return 0, err
	}
	user, err := s.loadUser(userID)
	if err != nil {
		return 0, err
	}
	credential, err := s.verifier.FinishLogin(user, session, request)
	if err != nil {
		return 0, err
	}
	if err := s.repo.SaveCredential(userID, *credential, time.Now()); err != nil {
		return 0, err
	}
	return userID, nil
}

func (s *WebAuthnService) HasCredentials(userID int64) (bool, error) {
	return s.repo.HasCredentials(userID)
}

func (s *WebAuthnService) loadUser(userID int64) (webAuthnUser, error) {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return webAuthnUser{}, err
	}
	handle, err := s.repo.GetOrCreateHandle(userID, time.Now())
	if err != nil {
		return webAuthnUser{}, err
	}
	credentials, err := s.repo.ListCredentials(userID)
	if err != nil {
		return webAuthnUser{}, err
	}
	return webAuthnUser{user: user, handle: handle, credentials: credentials}, nil
}

func (s *WebAuthnService) storeSession(userID int64, purpose string, session webauthn.SessionData) (string, error) {
	raw, hash, err := auth.GenerateOpaqueToken()
	if err != nil {
		return "", err
	}
	now := time.Now()
	if err := s.repo.SaveSession(hash, userID, purpose, session, now.Add(webAuthnCeremonyTTL), now); err != nil {
		return "", err
	}
	return raw, nil
}

func (s *WebAuthnService) consumeSession(raw, purpose string) (int64, webauthn.SessionData, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, webauthn.SessionData{}, ErrWebAuthnCeremony
	}
	userID, session, err := s.repo.ConsumeSession(auth.HashOpaqueToken(raw), purpose, time.Now())
	if errors.Is(err, repository.ErrWebAuthnSessionNotFound) {
		return 0, webauthn.SessionData{}, ErrWebAuthnCeremony
	}
	return userID, session, err
}
