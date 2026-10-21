package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// EventsPublishedTotal tracks ingested events categorized by event type
	EventsPublishedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "hookrelay",
			Name:      "events_published_total",
			Help:      "Total number of events ingested and dispatched to subscriptions",
		},
		[]string{"event_type"},
	)

	// DeliveriesTotal tracks terminal outcomes of webhook deliveries
	DeliveriesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "hookrelay",
			Name:      "deliveries_total",
			Help:      "Total number of webhook deliveries resolved by outcome status",
		},
		[]string{"status"}, // delivered, retry_waiting, dead_letter
	)

	// DeliveryDurationSeconds tracks latency of outbound HTTP calls
	DeliveryDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "hookrelay",
			Name:      "delivery_duration_seconds",
			Help:      "Duration of outbound webhook HTTP requests in seconds",
			Buckets:   []float64{0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"status"}, // success, failure
	)

	// DeliveryAttemptsTotal tracks individual HTTP attempts and status codes
	DeliveryAttemptsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "hookrelay",
			Name:      "delivery_attempts_total",
			Help:      "Total number of individual HTTP webhook delivery attempts",
		},
		[]string{"status_code"},
	)

	// DeliveriesReplayedTotal tracks manual DLQ retries and bulk replays
	DeliveriesReplayedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "hookrelay",
			Name:      "deliveries_replayed_total",
			Help:      "Total count of dead-letter deliveries manually retried or replayed",
		},
	)

	// ScheduledPromotionsTotal tracks delayed retries promoted from ZSET to active list
	ScheduledPromotionsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "hookrelay",
			Name:      "scheduled_promotions_total",
			Help:      "Total count of delayed delivery retries promoted from Redis ZSET to active queue",
		},
	)
)

// MetricsHandler returns standard Prometheus HTTP handler for scraping
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
