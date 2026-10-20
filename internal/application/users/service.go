package users

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"

	"github.com/purangoswami2001/hookrelay/internal/domain"
	"github.com/purangoswami2001/hookrelay/internal/ports"
)

var (
	ErrInvalidUser = errors.New("invalid user")
	ErrUserExists  = errors.New("user already exists with this email")
)

type Service struct {
	repository ports.UserRepository
	logger     *slog.Logger
}

func NewService(
	repository ports.UserRepository,
	logger *slog.Logger,
) *Service {
	return &Service{
		repository: repository,
		logger:     logger,
	}
}

// Register creates a new user and returns the user object along with the unhashed raw API key.
func (s *Service) Register(
	ctx context.Context,
	email string,
	name string,
) (*domain.User, string, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	name = strings.TrimSpace(name)

	if email == "" {
		return nil, "", fmt.Errorf("%w: email is required", ErrInvalidUser)
	}

	if _, err := mail.ParseAddress(email); err != nil {
		return nil, "", fmt.Errorf("%w: invalid email address format", ErrInvalidUser)
	}

	if name == "" {
		return nil, "", fmt.Errorf("%w: name is required", ErrInvalidUser)
	}

	// generate high-entropy API key: hr_live_<64 hex chars>
	rawAPIKey, keyHash, keyPrefix, err := GenerateAPIKey()
	if err != nil {
		return nil, "", fmt.Errorf("generate api key: %w", err)
	}

	user := &domain.User{
		Email:        email,
		Name:         name,
		APIKeyHash:   keyHash,
		APIKeyPrefix: keyPrefix,
	}

	if err := s.repository.Create(ctx, user); err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "users_email_key") {
			return nil, "", fmt.Errorf("%w", ErrUserExists)
		}
		s.logger.Error("create user failed", "email", email, "error", err)
		return nil, "", fmt.Errorf("create user: %w", err)
	}

	s.logger.Info("user registered", "user_id", user.ID, "email", user.Email)

	return user, rawAPIKey, nil
}

func (s *Service) GetByID(ctx context.Context, id string) (*domain.User, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: ID is required", ErrInvalidUser)
	}
	return s.repository.GetByID(ctx, id)
}

func (s *Service) AuthenticateByAPIKey(ctx context.Context, rawAPIKey string) (*domain.User, error) {
	rawAPIKey = strings.TrimSpace(rawAPIKey)
	if rawAPIKey == "" {
		return nil, errors.New("empty api key")
	}

	keyHash := HashAPIKey(rawAPIKey)
	return s.repository.GetByAPIKeyHash(ctx, keyHash)
}

// RegenerateAPIKey terminates the current API key and issues a new one.
func (s *Service) RegenerateAPIKey(ctx context.Context, userID string) (string, string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", "", fmt.Errorf("%w: user ID is required", ErrInvalidUser)
	}

	rawAPIKey, keyHash, keyPrefix, err := GenerateAPIKey()
	if err != nil {
		return "", "", fmt.Errorf("generate new api key: %w", err)
	}

	if err := s.repository.UpdateAPIKey(ctx, userID, keyHash, keyPrefix); err != nil {
		s.logger.Error("regenerate api key failed", "user_id", userID, "error", err)
		return "", "", fmt.Errorf("update api key: %w", err)
	}

	s.logger.Info("user api key regenerated", "user_id", userID, "prefix", keyPrefix)

	return rawAPIKey, keyPrefix, nil
}

// GenerateAPIKey generates a cryptographically secure random API key, its SHA-256 hash, and a safe prefix.
func GenerateAPIKey() (rawKey string, hash string, prefix string, err error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", "", err
	}

	token := hex.EncodeToString(randomBytes)
	rawKey = "hr_live_" + token
	hash = HashAPIKey(rawKey)
	prefix = "hr_live_" + token[:8] + "..."

	return rawKey, hash, prefix, nil
}

// HashAPIKey computes the SHA-256 hex digest of the raw API key.
func HashAPIKey(rawAPIKey string) string {
	sum := sha256.Sum256([]byte(rawAPIKey))
	return hex.EncodeToString(sum[:])
}
