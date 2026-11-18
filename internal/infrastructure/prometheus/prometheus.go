package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Prometheus struct {
	deliveriesCompletionsTotal *prometheus.CounterVec
	queueSize                  prometheus.Gauge
}

func New() *Prometheus {
	deliveriesTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "deliveries_completions_total",
		Help: "The total number of deliveries",
	}, []string{"result"})

	queueSize := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "queue_size",
		Help: "Current size of deliveries queue",
	})

	prometheus.MustRegister(deliveriesTotal)
	return &Prometheus{
		deliveriesCompletionsTotal: deliveriesTotal,
		queueSize: queueSize,
	}
}

func (p *Prometheus) IncDelivered() {
	p.deliveriesCompletionsTotal.WithLabelValues("delivered").Inc()
}

func (p *Prometheus) IncFailed() {
	p.deliveriesCompletionsTotal.WithLabelValues("failed").Inc()
}

func (p *Prometheus) IncQueueSize() {
	p.queueSize.Inc()
}

func (p *Prometheus) DecQueueSize() {
	p.queueSize.Dec()
}