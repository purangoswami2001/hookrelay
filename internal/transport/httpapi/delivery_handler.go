package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/purangoswami2001/hookrelay/internal/domain"
	"github.com/purangoswami2001/hookrelay/internal/ports"
)

type DeliveryHandler struct {
	repository ports.DeliveryRepository
	logger     *slog.Logger
}

func NewDeliveryHandler(
	repository ports.DeliveryRepository,
	logger *slog.Logger,
) *DeliveryHandler {
	return &DeliveryHandler{
		repository: repository,
		logger:     logger,
	}
}

// List handles GET /api/v1/deliveries (?event_id=...&limit=...&offset=...)
func (h *DeliveryHandler) List(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	eventID := strings.TrimSpace(query.Get("event_id"))

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

	h.logger.Debug("listing deliveries", "event_id", eventID, "limit", limit, "offset", offset)

	// status := query.Get("status")

	deliveries, err := h.repository.List(r.Context(), ports.DeliveryFilter{
		EventID: eventID,
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		if strings.Contains(err.Error(), "invalid input syntax for type uuid") {
			writeError(w, http.StatusBadRequest, "invalid event_id format")
			return
		}

		h.logger.Error("list deliveries failed", "error", err)
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

// GetByID handles GET /api/v1/deliveries/{id}
func (h *DeliveryHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "delivery ID is required")
		return
	}

	h.logger.Debug("getting delivery", "id", id)

	delivery, err := h.repository.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) ||
			strings.Contains(err.Error(), "not found") ||
			strings.Contains(err.Error(), "invalid input syntax for type uuid") {
			writeError(w, http.StatusNotFound, "delivery not found")
			return
		}

		h.logger.Error("failed to get delivery",
			"delivery_id", id,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "could not retrieve delivery")
		return
	}

	writeJSON(w, http.StatusOK, delivery)
}
