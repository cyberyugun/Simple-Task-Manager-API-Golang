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
	ErrInvalidUser        = errors.New("name, valid email, and password with at least 8 characters are required")
	ErrInvalidCredentials = errors.New("invalid email or password")
)

type AuthService struct {
	users  repository.UserRepository
	tokens *auth.TokenManager
}

func NewAuthService(users repository.UserRepository, tokens *auth.TokenManager) *AuthService {
	return &AuthService{users: users, tokens: tokens}
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

	return s.authResult(user)
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

	return s.authResult(user)
}

func (s *AuthService) authResult(user model.User) (model.AuthResult, error) {
	token, err := s.tokens.Generate(user.ID, user.Email)
	if err != nil {
		return model.AuthResult{}, err
	}
	user.PasswordHash = ""
	return model.AuthResult{User: user, AccessToken: token, TokenType: "Bearer"}, nil
}

func validEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	return err == nil && strings.EqualFold(address.Address, email)
}
