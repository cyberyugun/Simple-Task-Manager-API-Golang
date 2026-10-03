package service

import (
	"errors"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrInvalidUser            = errors.New("name, valid email, and password with at least 8 characters are required")
	ErrInvalidCredentials     = errors.New("invalid email or password")
	ErrInvalidRefreshToken    = errors.New("invalid or expired refresh token")
	ErrInvalidCurrentPassword = errors.New("current password is incorrect")
	ErrInvalidNewPassword     = errors.New("new password must be at least 8 characters")
	ErrInvalidActionToken     = errors.New("invalid or expired action token")
	ErrMFARequired            = errors.New("mfa code is required")
	ErrInvalidMFA             = errors.New("invalid mfa code")
)

type MFAVerifier interface {
	Enabled(userID int64) (bool, error)
	Verify(userID int64, code string) (bool, error)
}

type AuthService struct {
	users            repository.UserRepository
	refreshes        repository.RefreshTokenRepository
	actions          repository.AuthActionTokenRepository
	tokens           *auth.TokenManager
	accessTTL        time.Duration
	refreshTTL       time.Duration
	passwordResetTTL time.Duration
	emailVerifyTTL   time.Duration
	mfa              MFAVerifier
}

func NewAuthService(
	users repository.UserRepository,
	refreshes repository.RefreshTokenRepository,
	actions repository.AuthActionTokenRepository,
	tokens *auth.TokenManager,
	accessTTL time.Duration,
	refreshTTL time.Duration,
	passwordResetTTL time.Duration,
	emailVerifyTTL time.Duration,
) *AuthService {
	return &AuthService{
		users:            users,
		refreshes:        refreshes,
		actions:          actions,
		tokens:           tokens,
		accessTTL:        accessTTL,
		refreshTTL:       refreshTTL,
		passwordResetTTL: passwordResetTTL,
		emailVerifyTTL:   emailVerifyTTL,
	}
}

func (s *AuthService) SetMFAVerifier(verifier MFAVerifier) {
	s.mfa = verifier
}

func (s *AuthService) Register(req model.RegisterRequest, meta model.SessionMetadata) (model.AuthResult, error) {
	name := strings.TrimSpace(req.Name)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if name == "" || !validEmail(email) || len(req.Password) < 8 {
		return model.AuthResult{}, ErrInvalidUser
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return model.AuthResult{}, err
	}

	now := time.Now()
	user, err := s.users.Create(model.User{
		Name:         name,
		Email:        email,
		PasswordHash: string(hash),
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		return model.AuthResult{}, err
	}

	return s.issueSession(user, meta, false)
}

func (s *AuthService) Login(req model.LoginRequest, meta model.SessionMetadata) (model.AuthResult, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || req.Password == "" {
		return model.AuthResult{}, ErrInvalidCredentials
	}

	user, err := s.users.FindByEmail(email)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return model.AuthResult{}, ErrInvalidCredentials
		}
		return model.AuthResult{}, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return model.AuthResult{}, ErrInvalidCredentials
	}

	return s.issueSession(user, meta)
}

func (s *AuthService) Refresh(req model.RefreshRequest) (model.AuthResult, error) {
	raw := strings.TrimSpace(req.RefreshToken)
	if raw == "" {
		return model.AuthResult{}, ErrInvalidRefreshToken
	}

	newRaw, newHash, err := auth.GenerateRefreshToken()
	if err != nil {
		return model.AuthResult{}, err
	}

	now := time.Now()
	old, err := s.refreshes.Rotate(
		auth.HashRefreshToken(raw),
		newHash,
		now.Add(s.refreshTTL),
		now,
	)
	if err != nil {
		if errors.Is(err, repository.ErrInvalidRefreshToken) {
			return model.AuthResult{}, ErrInvalidRefreshToken
		}
		return model.AuthResult{}, err
	}

	user, err := s.users.FindByID(old.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return model.AuthResult{}, ErrInvalidRefreshToken
		}
		return model.AuthResult{}, err
	}

	return s.authResult(user, newRaw, old.MFAAuthenticated)
}

func (s *AuthService) Logout(req model.LogoutRequest) error {
	raw := strings.TrimSpace(req.RefreshToken)
	if raw == "" {
		return nil
	}
	return s.refreshes.Revoke(auth.HashRefreshToken(raw), time.Now())
}

func (s *AuthService) Sessions(userID int64) ([]model.RefreshSession, error) {
	return s.refreshes.ListActive(userID, time.Now())
}

func (s *AuthService) RevokeSession(userID, sessionID int64) error {
	err := s.refreshes.RevokeByID(userID, sessionID, time.Now())
	if errors.Is(err, repository.ErrInvalidRefreshToken) {
		return ErrInvalidRefreshToken
	}
	return err
}

func (s *AuthService) LogoutAll(userID int64) error {
	return s.refreshes.RevokeAll(userID, time.Now())
}

func (s *AuthService) ChangePassword(userID int64, req model.ChangePasswordRequest) error {
	if len(req.NewPassword) < 8 {
		return ErrInvalidNewPassword
	}

	user, err := s.users.FindByID(userID)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		return ErrInvalidCurrentPassword
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.NewPassword)); err == nil {
		return ErrInvalidNewPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	now := time.Now()
	if err := s.users.UpdatePassword(userID, string(hash), now); err != nil {
		return err
	}
	return s.refreshes.RevokeAll(userID, now)
}

func (s *AuthService) ForgotPassword(req model.ForgotPasswordRequest) (model.ActionTokenResult, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))
	result := model.ActionTokenResult{
		Message: "if the account exists, password reset instructions have been created",
	}
	if !validEmail(email) {
		return result, nil
	}

	user, err := s.users.FindByEmail(email)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return result, nil
		}
		return model.ActionTokenResult{}, err
	}

	token, err := s.issueActionToken(user.ID, model.ActionPasswordReset, s.passwordResetTTL)
	if err != nil {
		return model.ActionTokenResult{}, err
	}
	result.DevelopmentToken = token
	return result, nil
}

func (s *AuthService) ResetPassword(req model.ResetPasswordRequest) error {
	if len(req.NewPassword) < 8 {
		return ErrInvalidNewPassword
	}

	token := strings.TrimSpace(req.Token)
	if token == "" {
		return ErrInvalidActionToken
	}

	now := time.Now()
	action, err := s.actions.Consume(
		auth.HashOpaqueToken(token),
		model.ActionPasswordReset,
		now,
	)
	if err != nil {
		if errors.Is(err, repository.ErrInvalidActionToken) {
			return ErrInvalidActionToken
		}
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.users.UpdatePassword(action.UserID, string(hash), now); err != nil {
		return err
	}
	return s.refreshes.RevokeAll(action.UserID, now)
}

func (s *AuthService) RequestEmailVerification(userID int64) (model.ActionTokenResult, error) {
	result := model.ActionTokenResult{
		Message: "email verification instructions have been created",
	}

	user, err := s.users.FindByID(userID)
	if err != nil {
		return model.ActionTokenResult{}, err
	}
	if user.EmailVerifiedAt != nil {
		result.Message = "email is already verified"
		return result, nil
	}

	token, err := s.issueActionToken(user.ID, model.ActionEmailVerification, s.emailVerifyTTL)
	if err != nil {
		return model.ActionTokenResult{}, err
	}
	result.DevelopmentToken = token
	return result, nil
}

func (s *AuthService) VerifyEmail(req model.VerifyEmailRequest) error {
	token := strings.TrimSpace(req.Token)
	if token == "" {
		return ErrInvalidActionToken
	}

	now := time.Now()
	action, err := s.actions.Consume(
		auth.HashOpaqueToken(token),
		model.ActionEmailVerification,
		now,
	)
	if err != nil {
		if errors.Is(err, repository.ErrInvalidActionToken) {
			return ErrInvalidActionToken
		}
		return err
	}
	return s.users.MarkEmailVerified(action.UserID, now)
}

func (s *AuthService) issueActionToken(userID int64, purpose string, ttl time.Duration) (string, error) {
	raw, hash, err := auth.GenerateOpaqueToken()
	if err != nil {
		return "", err
	}

	now := time.Now()
	if err := s.actions.Create(model.AuthActionToken{
		UserID:    userID,
		TokenHash: hash,
		Purpose:   purpose,
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}); err != nil {
		return "", err
	}
	return raw, nil
}

func (s *AuthService) issueSession(user model.User, meta model.SessionMetadata, mfaAuthenticated bool) (model.AuthResult, error) {
	raw, hash, err := auth.GenerateRefreshToken()
	if err != nil {
		return model.AuthResult{}, err
	}

	now := time.Now()
	if err := s.refreshes.Create(model.RefreshSession{
		UserID:     user.ID,
		TokenHash:  hash,
		UserAgent:  strings.TrimSpace(meta.UserAgent),
		IPAddress:        strings.TrimSpace(meta.IPAddress),
		MFAAuthenticated: mfaAuthenticated,
		ExpiresAt:  now.Add(s.refreshTTL),
		LastUsedAt: now,
		CreatedAt:  now,
	}); err != nil {
		return model.AuthResult{}, err
	}

	return s.authResult(user, raw, mfaAuthenticated)
}

func (s *AuthService) authResult(user model.User, refreshToken string, mfaAuthenticated bool) (model.AuthResult, error) {
	token, err := s.tokens.GenerateUserWithMFA(user.ID, user.Email, nil, 0, mfaAuthenticated)
	if err != nil {
		return model.AuthResult{}, err
	}
	user.PasswordHash = ""
	return model.AuthResult{
		User:                  user,
		AccessToken:           token,
		RefreshToken:          refreshToken,
		TokenType:             "Bearer",
		AccessTokenExpiresIn:  int64(s.accessTTL.Seconds()),
		RefreshTokenExpiresIn: int64(s.refreshTTL.Seconds()),
	}, nil
}

func validEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	return err == nil && strings.EqualFold(address.Address, email)
}
