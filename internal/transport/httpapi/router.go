package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/purangoswami2001/hookrelay/internal/application/events"
	"github.com/purangoswami2001/hookrelay/internal/application/subscriptions"
	"github.com/purangoswami2001/hookrelay/internal/ports"
)

func NewRouter(
	eventService *events.Service,
	eventRepository ports.EventRepository,
	subscriptionService *subscriptions.Service,
	deliveryRepository ports.DeliveryRepository,
	logger *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	eventHandler := NewEventHandler(
		eventService, eventRepository, logger,
	)
	subscriptionHandler := NewSubscriptionHandler(
		subscriptionService, logger,
	)
	deliveryHandler := NewDeliveryHandler(
		deliveryRepository, logger,
	)

	// health
	mux.HandleFunc("GET /health", healthHandler)

	// events
	mux.HandleFunc("POST /api/v1/events", eventHandler.Create)
	mux.HandleFunc("GET /api/v1/events/{id}", eventHandler.GetByID)

	// subscriptions
	mux.HandleFunc("POST /api/v1/subscriptions", subscriptionHandler.Create)
	mux.HandleFunc("GET /api/v1/subscriptions", subscriptionHandler.List)
	mux.HandleFunc("GET /api/v1/subscriptions/{id}", subscriptionHandler.GetByID)
	mux.HandleFunc("PATCH /api/v1/subscriptions/{id}", subscriptionHandler.Update)
	mux.HandleFunc("DELETE /api/v1/subscriptions/{id}", subscriptionHandler.Delete)

	// deliveries
	mux.HandleFunc("GET /api/v1/deliveries", deliveryHandler.List)
	mux.HandleFunc("GET /api/v1/deliveries/{id}", deliveryHandler.GetByID)

	var handler http.Handler = mux
	// handler = corsMiddleware(handler)
	handler = RecoveryMiddleware(logger, handler)
	handler = LoggingMiddleware(logger, handler)

	return handler
}
