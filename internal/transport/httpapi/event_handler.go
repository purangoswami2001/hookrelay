package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/purangoswami2001/hookrelay/internal/application/events"
	"github.com/purangoswami2001/hookrelay/internal/domain"
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
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

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

	h.logger.Debug("creating event", "user_id", user.ID, "type", req.Type, "idempotency_key", idempotencyKey)

	event, err := h.service.Publish(
		r.Context(),
		user.ID,
		req.Type,
		req.Data,
		idempotencyKey,
	)
	if err != nil {
		h.logger.Error("publish event failed",
			"user_id", user.ID,
			"event_type", req.Type,
			"error", err,
		)

		writeError(w, http.StatusInternalServerError, "could not publish event")
		return
	}

	writeJSON(w, http.StatusCreated, event)
}

// List handles GET /api/v1/events (scoped to current user)
func (h *EventHandler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	query := r.URL.Query()
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

	h.logger.Debug("listing events", "user_id", user.ID, "limit", limit, "offset", offset)

	eventsList, err := h.repository.ListByUser(r.Context(), user.ID, limit, offset)
	if err != nil {
		h.logger.Error("list events failed", "user_id", user.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not list events")
		return
	}

	if eventsList == nil {
		eventsList = []domain.Event{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": eventsList,
	})
}

// GetByID handles GET /api/v1/events/{id} (scoped to current user)
func (h *EventHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "event ID is required")
		return
	}

	h.logger.Debug("getting event", "user_id", user.ID, "id", id)

	event, err := h.repository.GetByIDAndUser(r.Context(), id, user.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) ||
			strings.Contains(err.Error(), "not found") ||
			strings.Contains(err.Error(), "invalid input syntax for type uuid") {
			writeError(w, http.StatusNotFound, "event not found")
			return
		}

		h.logger.Error("get event failed",
			"event_id", id,
			"user_id", user.ID,
			"error", err,
		)

		writeError(w, http.StatusInternalServerError, "could not retrieve event")
		return
	}

	writeJSON(w, http.StatusOK, event)
}
