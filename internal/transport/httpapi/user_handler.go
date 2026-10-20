package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/purangoswami2001/hookrelay/internal/application/users"
	"github.com/purangoswami2001/hookrelay/internal/domain"
)

type UserHandler struct {
	service *users.Service
	logger  *slog.Logger
}

func NewUserHandler(
	service *users.Service,
	logger *slog.Logger,
) *UserHandler {
	return &UserHandler{
		service: service,
		logger:  logger,
	}
}

type registerUserRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

type registerUserResponse struct {
	User    *domain.User `json:"user"`
	APIKey  string       `json:"api_key"`
	Message string       `json:"message"`
}

type regenerateKeyResponse struct {
	Message      string `json:"message"`
	APIKey       string `json:"api_key"`
	APIKeyPrefix string `json:"api_key_prefix"`
}

// Register handles POST /api/v1/users/register
func (h *UserHandler) Register(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	var req registerUserRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}

	user, rawAPIKey, err := h.service.Register(r.Context(), req.Email, req.Name)
	if err != nil {
		if errors.Is(err, users.ErrInvalidUser) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, users.ErrUserExists) {
			writeError(w, http.StatusConflict, "user with this email already exists")
			return
		}

		h.logger.Error("register user failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not register user")
		return
	}

	writeJSON(w, http.StatusCreated, registerUserResponse{
		User:    user,
		APIKey:  rawAPIKey,
		Message: "Store your API key securely. It will never be shown again.",
	})
}

// Me handles GET /api/v1/users/me (authenticated)
func (h *UserHandler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user": user,
	})
}

// RegenerateKey handles POST /api/v1/users/me/regenerate-key (authenticated)
func (h *UserHandler) RegenerateKey(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	newKey, newPrefix, err := h.service.RegenerateAPIKey(r.Context(), user.ID)
	if err != nil {
		h.logger.Error("regenerate api key failed", "user_id", user.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not regenerate api key")
		return
	}

	writeJSON(w, http.StatusOK, regenerateKeyResponse{
		Message:      "API key regenerated successfully. Previous key has been terminated.",
		APIKey:       newKey,
		APIKeyPrefix: newPrefix,
	})
}
