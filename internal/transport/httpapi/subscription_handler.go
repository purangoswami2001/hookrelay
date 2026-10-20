package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/purangoswami2001/hookrelay/internal/application/subscriptions"
	"github.com/purangoswami2001/hookrelay/internal/domain"
)

type SubscriptionHandler struct {
	service *subscriptions.Service
	logger  *slog.Logger
}

func NewSubscriptionHandler(
	service *subscriptions.Service,
	logger *slog.Logger,
) *SubscriptionHandler {
	return &SubscriptionHandler{
		service: service,
		logger:  logger,
	}
}

type createSubscriptionRequest struct {
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types"`
}

type updateSubscriptionRequest struct {
	URL        *string  `json:"url"`
	EventTypes []string `json:"event_types"`
	Active     *bool    `json:"active"`
}

// subscriptionCreatedResponse returns secret only once at creation time
type subscriptionCreatedResponse struct {
	Subscription *domain.Subscription `json:"subscription"`
	Secret       string               `json:"secret"`
}

// Create handles POST /api/v1/subscriptions
func (h *SubscriptionHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	var req createSubscriptionRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}

	h.logger.Debug("creating subscription", "user_id", user.ID, "url", req.URL, "events", req.EventTypes)

	secret, err := generateSecret()
	if err != nil {
		h.logger.Error("generate subscription secret failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create subscription")
		return
	}

	subscription := &domain.Subscription{
		UserID:     user.ID,
		URL:        strings.TrimSpace(req.URL),
		EventTypes: req.EventTypes,
		Secret:     secret,
		Active:     true,
	}

	if err := h.service.Create(r.Context(), subscription); err != nil {
		if errors.Is(err, subscriptions.ErrInvalidSubscription) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		h.logger.Error("create subscription failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create subscription")
		return
	}

	writeJSON(w, http.StatusCreated, subscriptionCreatedResponse{
		Subscription: subscription,
		Secret:       secret,
	})
}

// List handles GET /api/v1/subscriptions (scoped to current user)
func (h *SubscriptionHandler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	h.logger.Debug("listing subscriptions", "user_id", user.ID)

	items, err := h.service.ListByUser(r.Context(), user.ID)
	if err != nil {
		h.logger.Error("list subscriptions failed", "user_id", user.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not list subscriptions")
		return
	}

	if items == nil {
		items = []domain.Subscription{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": items,
	})
}

// GetByID handles GET /api/v1/subscriptions/{id} (scoped to current user)
func (h *SubscriptionHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "subscription ID is required")
		return
	}

	h.logger.Debug("getting subscription", "user_id", user.ID, "id", id)

	item, err := h.service.GetByIDAndUser(r.Context(), id, user.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) ||
			strings.Contains(err.Error(), "not found") ||
			strings.Contains(err.Error(), "invalid input syntax for type uuid") {
			writeError(w, http.StatusNotFound, "subscription not found")
			return
		}

		h.logger.Error("get subscription failed",
			"subscription_id", id,
			"user_id", user.ID,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "could not retrieve subscription")
		return
	}

	writeJSON(w, http.StatusOK, item)
}

// Update handles PATCH /api/v1/subscriptions/{id} (scoped to current user)
func (h *SubscriptionHandler) Update(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "subscription ID is required")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	var req updateSubscriptionRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}

	h.logger.Debug("updating subscription", "user_id", user.ID, "id", id)

	current, err := h.service.GetByIDAndUser(r.Context(), id, user.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) ||
			strings.Contains(err.Error(), "not found") ||
			strings.Contains(err.Error(), "invalid input syntax for type uuid") {
			writeError(w, http.StatusNotFound, "subscription not found")
			return
		}

		h.logger.Error("load subscription for update failed",
			"subscription_id", id,
			"user_id", user.ID,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "could not update subscription")
		return
	}

	if req.URL != nil {
		current.URL = strings.TrimSpace(*req.URL)
	}
	if req.EventTypes != nil {
		current.EventTypes = req.EventTypes
	}
	if req.Active != nil {
		current.Active = *req.Active
	}

	if err := h.service.Update(r.Context(), current); err != nil {
		if errors.Is(err, subscriptions.ErrInvalidSubscription) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		h.logger.Error("update subscription failed",
			"subscription_id", id,
			"user_id", user.ID,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "could not update subscription")
		return
	}

	writeJSON(w, http.StatusOK, current)
}

// Delete handles DELETE /api/v1/subscriptions/{id} (scoped to current user)
func (h *SubscriptionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "subscription ID is required")
		return
	}

	h.logger.Debug("deleting subscription", "user_id", user.ID, "id", id)

	if err := h.service.Delete(r.Context(), id, user.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) ||
			strings.Contains(err.Error(), "not found") ||
			strings.Contains(err.Error(), "invalid input syntax for type uuid") {
			writeError(w, http.StatusNotFound, "subscription not found")
			return
		}

		h.logger.Error("delete subscription failed",
			"subscription_id", id,
			"user_id", user.ID,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "could not delete subscription")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func decodeJSON(reader io.Reader, destination any) error {
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(destination); err != nil {
		return err
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("request must contain only one JSON value")
	}

	return nil
}

func generateSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
