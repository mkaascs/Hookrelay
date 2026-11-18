package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Prometheus struct {
	queueDepth                 prometheus.GaugeFunc
	deliveriesCompletionsTotal *prometheus.CounterVec
}

func New() *Prometheus {
	deliveriesTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "deliveries_completions_total",
		Help: "The total number of deliveries",
	}, []string{"result"})

	prometheus.MustRegister(deliveriesTotal)
	return &Prometheus{
		deliveriesCompletionsTotal: deliveriesTotal,
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

func (p *Prometheus) IncDelivered() {
	p.deliveriesCompletionsTotal.WithLabelValues("delivered").Inc()
}

func (p *Prometheus) IncFailed() {
	p.deliveriesCompletionsTotal.WithLabelValues("failed").Inc()
}
