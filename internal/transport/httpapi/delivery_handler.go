package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/purangoswami2001/hookrelay/internal/application/deliveries"
	"github.com/purangoswami2001/hookrelay/internal/domain"
	"github.com/purangoswami2001/hookrelay/internal/ports"
)

type DeliveryHandler struct {
	service    *deliveries.Service
	repository ports.DeliveryRepository
	logger     *slog.Logger
}

func NewDeliveryHandler(
	service *deliveries.Service,
	repository ports.DeliveryRepository,
	logger *slog.Logger,
) *DeliveryHandler {
	return &DeliveryHandler{
		service:    service,
		repository: repository,
		logger:     logger,
	}
}

// List handles GET /api/v1/deliveries (?event_id=...&subscription_id=...&limit=...&offset=...) scoped to current user
func (h *DeliveryHandler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	query := r.URL.Query()
	eventID := strings.TrimSpace(query.Get("event_id"))
	subscriptionID := strings.TrimSpace(query.Get("subscription_id"))

	// clamp limit between 1 and 100
	limit := 50
	if l := query.Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = min(val, 100)
		}
	}

	offset := 0
	if o := query.Get("offset"); o != "" {
		if val, err := strconv.Atoi(o); err == nil && val >= 0 {
			offset = val
		}
	}

	h.logger.Debug("listing deliveries", "user_id", user.ID, "event_id", eventID, "subscription_id", subscriptionID, "limit", limit, "offset", offset)

	deliveries, err := h.repository.List(r.Context(), ports.DeliveryFilter{
		UserID:         user.ID,
		EventID:        eventID,
		SubscriptionID: subscriptionID,
		Limit:          limit,
		Offset:         offset,
	})
	if err != nil {
		if strings.Contains(err.Error(), "invalid input syntax for type uuid") {
			writeError(w, http.StatusBadRequest, "invalid UUID format in query filter")
			return
		}

		h.logger.Error("list deliveries failed", "user_id", user.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not list deliveries")
		return
	}

	if deliveries == nil {
		deliveries = []domain.Delivery{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": deliveries,
	})
}

// GetByID handles GET /api/v1/deliveries/{id} scoped to current user
func (h *DeliveryHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "delivery ID is required")
		return
	}

	h.logger.Debug("getting delivery", "user_id", user.ID, "id", id)

	delivery, err := h.repository.GetByIDAndUser(r.Context(), id, user.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) ||
			strings.Contains(err.Error(), "not found") ||
			strings.Contains(err.Error(), "invalid input syntax for type uuid") {
			writeError(w, http.StatusNotFound, "delivery not found")
			return
		}

		h.logger.Error("failed to get delivery",
			"delivery_id", id,
			"user_id", user.ID,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "could not retrieve delivery")
		return
	}

	writeJSON(w, http.StatusOK, delivery)
}

// Retry handles POST /api/v1/deliveries/{id}/retry scoped to the authenticated user
func (h *DeliveryHandler) Retry(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "delivery ID is required")
		return
	}

	h.logger.Info("manual retry requested", "delivery_id", id, "user_id", user.ID)

	if err := h.service.Retry(r.Context(), id, user.ID); err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "not eligible") {
			writeError(w, http.StatusNotFound, "delivery not found or not eligible for retry")
			return
		}

		h.logger.Error("failed to retry delivery", "delivery_id", id, "user_id", user.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not retry delivery")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":     "delivery reset and queued for redelivery",
		"delivery_id": id,
		"status":      "pending",
	})
}

type ReplayRequest struct {
	EventID        string `json:"event_id"`
	SubscriptionID string `json:"subscription_id"`
	Limit          int    `json:"limit"`
}

// Replay handles POST /api/v1/deliveries/replay scoped to the authenticated user
func (h *DeliveryHandler) Replay(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req ReplayRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	if req.Limit <= 0 {
		req.Limit = 100
	} else if req.Limit > 500 {
		req.Limit = 500
	}

	h.logger.Info("batch replay requested", "user_id", user.ID, "subscription_id", req.SubscriptionID, "event_id", req.EventID, "limit", req.Limit)

	replayedIDs, err := h.service.Replay(r.Context(), ports.ReplayFilter{
		UserID:         user.ID,
		EventID:        strings.TrimSpace(req.EventID),
		SubscriptionID: strings.TrimSpace(req.SubscriptionID),
		Limit:          req.Limit,
	})
	if err != nil {
		h.logger.Error("batch replay failed", "user_id", user.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not replay deliveries")
		return
	}

	if replayedIDs == nil {
		replayedIDs = []string{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":      "dead-letter deliveries replayed",
		"count":        len(replayedIDs),
		"delivery_ids": replayedIDs,
	})
}

