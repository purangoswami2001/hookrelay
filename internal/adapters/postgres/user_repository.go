package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/purangoswami2001/hookrelay/internal/domain"
)

type UserRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(
	ctx context.Context,
	user *domain.User,
) error {
	query := `
		INSERT INTO users (
			email,
			name,
			api_key_hash,
			api_key_prefix
		)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text, created_at, updated_at
	`

	err := r.db.QueryRow(
		ctx,
		query,
		user.Email,
		user.Name,
		user.APIKeyHash,
		user.APIKeyPrefix,
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	return nil
}

func (r *UserRepository) GetByID(
	ctx context.Context,
	id string,
) (*domain.User, error) {
	query := `
		SELECT
			id::text,
			email,
			name,
			api_key_prefix,
			api_key_hash,
			created_at,
			updated_at
		FROM users
		WHERE id = $1
	`

	var user domain.User
	err := r.db.QueryRow(ctx, query, id).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.APIKeyPrefix,
		&user.APIKeyHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: user %s not found", pgx.ErrNoRows, id)
	}
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}

	return &user, nil
}

func (r *UserRepository) GetByAPIKeyHash(
	ctx context.Context,
	apiKeyHash string,
) (*domain.User, error) {
	query := `
		SELECT
			id::text,
			email,
			name,
			api_key_prefix,
			api_key_hash,
			created_at,
			updated_at
		FROM users
		WHERE api_key_hash = $1
	`

	var user domain.User
	err := r.db.QueryRow(ctx, query, apiKeyHash).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.APIKeyPrefix,
		&user.APIKeyHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: user not found for api key", pgx.ErrNoRows)
	}
	if err != nil {
		return nil, fmt.Errorf("get user by api key hash: %w", err)
	}

	return &user, nil
}

func (r *UserRepository) GetByEmail(
	ctx context.Context,
	email string,
) (*domain.User, error) {
	query := `
		SELECT
			id::text,
			email,
			name,
			api_key_prefix,
			api_key_hash,
			created_at,
			updated_at
		FROM users
		WHERE email = $1
	`

	var user domain.User
	err := r.db.QueryRow(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.APIKeyPrefix,
		&user.APIKeyHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: user with email %s not found", pgx.ErrNoRows, email)
	}
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}

	return &user, nil
}

func (r *UserRepository) UpdateAPIKey(
	ctx context.Context,
	userID, newAPIKeyHash, newAPIKeyPrefix string,
) error {
	query := `
		UPDATE users
		SET
			api_key_hash = $2,
			api_key_prefix = $3,
			updated_at = NOW()
		WHERE id = $1
	`

	tag, err := r.db.Exec(ctx, query, userID, newAPIKeyHash, newAPIKeyPrefix)
	if err != nil {
		return fmt.Errorf("update user api key: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user %s not found", userID)
	}

	return nil
}
