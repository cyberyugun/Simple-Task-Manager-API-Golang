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
	ErrInvalidUser         = errors.New("name, valid email, and password with at least 8 characters are required")
	ErrInvalidCredentials  = errors.New("invalid email or password")
	ErrInvalidRefreshToken = errors.New("invalid or expired refresh token")
)

type AuthService struct {
	users      repository.UserRepository
	refreshes  repository.RefreshTokenRepository
	tokens     *auth.TokenManager
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewAuthService(
	users repository.UserRepository,
	refreshes repository.RefreshTokenRepository,
	tokens *auth.TokenManager,
	accessTTL time.Duration,
	refreshTTL time.Duration,
) *AuthService {
	return &AuthService{
		users:      users,
		refreshes:  refreshes,
		tokens:     tokens,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

func (s *AuthService) Register(req model.RegisterRequest) (model.AuthResult, error) {
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

	return s.issueSession(user)
}

func (s *AuthService) Login(req model.LoginRequest) (model.AuthResult, error) {
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

	return s.issueSession(user)
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

	return s.authResult(user, newRaw)
}

func (s *AuthService) Logout(req model.LogoutRequest) error {
	raw := strings.TrimSpace(req.RefreshToken)
	if raw == "" {
		return nil
	}
	return s.refreshes.Revoke(auth.HashRefreshToken(raw), time.Now())
}

func (s *AuthService) issueSession(user model.User) (model.AuthResult, error) {
	raw, hash, err := auth.GenerateRefreshToken()
	if err != nil {
		return model.AuthResult{}, err
	}

	now := time.Now()
	if err := s.refreshes.Create(model.RefreshSession{
		UserID:    user.ID,
		TokenHash: hash,
		ExpiresAt: now.Add(s.refreshTTL),
		CreatedAt: now,
	}); err != nil {
		return model.AuthResult{}, err
	}

	return s.authResult(user, raw)
}

func (s *AuthService) authResult(user model.User, refreshToken string) (model.AuthResult, error) {
	token, err := s.tokens.Generate(user.ID, user.Email)
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
