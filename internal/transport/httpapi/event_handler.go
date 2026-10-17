package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/purangoswami2001/hookrelay/internal/application/events"
	"github.com/purangoswami2001/hookrelay/internal/ports"
)

type EventHandler struct {
	service    *events.Service
	repository ports.EventRepository
	logger     *slog.Logger
}

func NewEventHandler(
	service *events.Service,
	repository ports.EventRepository,
	logger *slog.Logger,
) *EventHandler {
	return &EventHandler{
		service:    service,
		repository: repository,
		logger:     logger,
	}
}

type createEventRequest struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Create handles POST /api/v1/events
func (h *EventHandler) Create(w http.ResponseWriter, r *http.Request) {
	// 1MB limit
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	var req createEventRequest

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}

	// reject trailing content
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "request must contain only one JSON object")
		return
	}

	if strings.TrimSpace(req.Type) == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}

	if len(req.Data) == 0 || !json.Valid(req.Data) {
		writeError(w, http.StatusBadRequest, "data must be valid JSON")
		return
	}

	// must be an object
	trimmedData := bytes.TrimSpace(req.Data)
	if len(trimmedData) == 0 || trimmedData[0] != '{' {
		writeError(w, http.StatusBadRequest, "data must be a JSON object")
		return
	}

	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))

	h.logger.Debug("creating event", "type", req.Type, "idempotency_key", idempotencyKey)

	// traceParent := r.Header.Get("traceparent")

	event, err := h.service.Publish(
		r.Context(),
		req.Type,
		req.Data,
		idempotencyKey,
	)
	if err != nil {
		h.logger.Error("publish event failed",
			"event_type", req.Type,
			"error", err,
		)

		writeError(w, http.StatusInternalServerError, "could not publish event")
		return
	}

	writeJSON(w, http.StatusCreated, event)
}

// GetByID handles GET /api/v1/events/{id}
func (h *EventHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "event ID is required")
		return
	}

	h.logger.Debug("getting event", "id", id)

	event, err := h.repository.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) ||
			strings.Contains(err.Error(), "not found") ||
			strings.Contains(err.Error(), "invalid input syntax for type uuid") {
			writeError(w, http.StatusNotFound, "event not found")
			return
		}

		h.logger.Error("get event failed",
			"event_id", id,
			"error", err,
		)

		writeError(w, http.StatusInternalServerError, "could not retrieve event")
		return
	}

	writeJSON(w, http.StatusOK, event)
}
