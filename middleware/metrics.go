package middleware

import "github.com/prometheus/client_golang/prometheus"

var (
	apiRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "api_requests_total",
			Help: "Total API requests",
		},
		[]string{"path", "method", "status"},
	)
	apiDurations = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "api_request_duration_seconds",
			Help:    "API request duration distribution",
			Buckets: []float64{0.1, 0.5, 1, 2.5, 5},
		},
		[]string{"path", "method"},
	)
)

func init() {
	prometheus.MustRegister(apiRequests, apiDurations)
}
