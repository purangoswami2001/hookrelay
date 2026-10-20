package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/purangoswami2001/hookrelay/internal/application/events"
	"github.com/purangoswami2001/hookrelay/internal/application/subscriptions"
	"github.com/purangoswami2001/hookrelay/internal/application/users"
	"github.com/purangoswami2001/hookrelay/internal/ports"
)

func NewRouter(
	userService *users.Service,
	eventService *events.Service,
	eventRepository ports.EventRepository,
	subscriptionService *subscriptions.Service,
	deliveryRepository ports.DeliveryRepository,
	logger *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	userHandler := NewUserHandler(userService, logger)
	eventHandler := NewEventHandler(eventService, eventRepository, logger)
	subscriptionHandler := NewSubscriptionHandler(subscriptionService, logger)
	deliveryHandler := NewDeliveryHandler(deliveryRepository, logger)

	auth := RequireAuth(userService, logger)

	// 1. Public endpoints
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("POST /api/v1/users/register", userHandler.Register)

	// 2. Authenticated user management
	mux.Handle("GET /api/v1/users/me", auth(http.HandlerFunc(userHandler.Me)))
	mux.Handle("POST /api/v1/users/me/regenerate-key", auth(http.HandlerFunc(userHandler.RegenerateKey)))

	// 3. Authenticated events
	mux.Handle("POST /api/v1/events", auth(http.HandlerFunc(eventHandler.Create)))
	mux.Handle("GET /api/v1/events", auth(http.HandlerFunc(eventHandler.List)))
	mux.Handle("GET /api/v1/events/{id}", auth(http.HandlerFunc(eventHandler.GetByID)))

	// 4. Authenticated subscriptions
	mux.Handle("POST /api/v1/subscriptions", auth(http.HandlerFunc(subscriptionHandler.Create)))
	mux.Handle("GET /api/v1/subscriptions", auth(http.HandlerFunc(subscriptionHandler.List)))
	mux.Handle("GET /api/v1/subscriptions/{id}", auth(http.HandlerFunc(subscriptionHandler.GetByID)))
	mux.Handle("PATCH /api/v1/subscriptions/{id}", auth(http.HandlerFunc(subscriptionHandler.Update)))
	mux.Handle("DELETE /api/v1/subscriptions/{id}", auth(http.HandlerFunc(subscriptionHandler.Delete)))

	// 5. Authenticated deliveries
	mux.Handle("GET /api/v1/deliveries", auth(http.HandlerFunc(deliveryHandler.List)))
	mux.Handle("GET /api/v1/deliveries/{id}", auth(http.HandlerFunc(deliveryHandler.GetByID)))

	var handler http.Handler = mux
	handler = RecoveryMiddleware(logger, handler)
	handler = LoggingMiddleware(logger, handler)

	return handler
}
