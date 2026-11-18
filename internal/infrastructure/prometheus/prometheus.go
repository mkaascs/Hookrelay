package prometheus

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Prometheus struct {
	queueDepth                 prometheus.GaugeFunc
	deliveriesAttemptDurations prometheus.Histogram
	deliveriesCompletionsTotal *prometheus.CounterVec
	deliveriesAttemptsTotal    *prometheus.CounterVec
}

func New() *Prometheus {
	completionsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "deliveries_completions_total",
		Help: "The total number of deliveries",
	}, []string{"result"})

	attemptsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "deliveries_attempts_total",
		Help: "The total number of attempts of deliveries",
	}, []string{"result"})

	attemptDuration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "deliveries_attempt_duration_seconds",
		Help: "The duration of deliveries attempts",
	})

	prometheus.MustRegister(completionsTotal, attemptsTotal, attemptDuration)
	return &Prometheus{
		deliveriesCompletionsTotal: completionsTotal,
		deliveriesAttemptsTotal:    attemptsTotal,
		deliveriesAttemptDurations: attemptDuration,
	}
}

func (p *Prometheus) RegisterQueueDepth(depth func() int) {
	p.queueDepth = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "delivery_worker_queue_depth",
		Help: "Number of deliveries waiting in the worker queue",
	}, func() float64 {
		return float64(depth())
	})

	prometheus.MustRegister(p.queueDepth)
}

func (p *Prometheus) IncCompletionDelivered() {
	p.deliveriesCompletionsTotal.WithLabelValues("delivered").Inc()
}

func (p *Prometheus) IncCompletionFailed() {
	p.deliveriesCompletionsTotal.WithLabelValues("failed").Inc()
}

func (p *Prometheus) IncAttemptSuccess() {
	p.deliveriesAttemptsTotal.WithLabelValues("success").Inc()
}

func (p *Prometheus) IncAttemptHttpError() {
	p.deliveriesAttemptsTotal.WithLabelValues("http_error").Inc()
}

func (p *Prometheus) IncAttemptNoResponse() {
	p.deliveriesAttemptsTotal.WithLabelValues("no_response").Inc()
}

func (p *Prometheus) ObserveAttemptDuration(elapsed time.Duration) {
	p.deliveriesAttemptDurations.Observe(elapsed.Seconds())
}
