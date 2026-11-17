package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Prometheus struct {
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

func (p *Prometheus) IncDelivered() {
	p.deliveriesCompletionsTotal.WithLabelValues("delivered").Inc()
}

func (p *Prometheus) IncFailed() {
	p.deliveriesCompletionsTotal.WithLabelValues("failed").Inc()
}