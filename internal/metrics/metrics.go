package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry      *prometheus.Registry
	sent          *prometheus.CounterVec
	duration      *prometheus.HistogramVec
	devices       *prometheus.GaugeVec
	batches       *prometheus.CounterVec
	batchSize     *prometheus.HistogramVec
	batchDuration *prometheus.HistogramVec
	deliveries    *prometheus.CounterVec
}

func New() *Metrics {
	m := &Metrics{
		registry:      prometheus.NewRegistry(),
		sent:          prometheus.NewCounterVec(prometheus.CounterOpts{Name: "virtualsms_messages_total", Help: "SMS submissions by outcome."}, []string{"device", "provider", "outcome"}),
		duration:      prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "virtualsms_provider_duration_seconds", Help: "AT submission handling time; includes batch wait when ack_mode is sent.", Buckets: prometheus.DefBuckets}, []string{"device", "provider"}),
		devices:       prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "virtualsms_device_up", Help: "Virtual device availability."}, []string{"device", "provider"}),
		batches:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "virtualsms_batches_total", Help: "Submitted batches by outcome."}, []string{"provider", "outcome"}),
		batchSize:     prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "virtualsms_batch_size", Help: "Recipient count per batch.", Buckets: []float64{1, 2, 3, 5, 10, 25, 50, 100}}, []string{"provider"}),
		batchDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "virtualsms_batch_provider_duration_seconds", Help: "Provider batch submission duration.", Buckets: prometheus.DefBuckets}, []string{"provider"}),
		deliveries:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "virtualsms_delivery_results_total", Help: "Provider results per recipient."}, []string{"provider", "outcome"}),
	}
	m.registry.MustRegister(m.sent, m.duration, m.devices, m.batches, m.batchSize, m.batchDuration, m.deliveries)
	return m
}

func (m *Metrics) RecordBatch(provider string, size, failed int, elapsed time.Duration) {
	outcome := "success"
	if failed == size {
		outcome = "error"
	} else if failed > 0 {
		outcome = "partial"
	}
	m.batches.WithLabelValues(provider, outcome).Inc()
	m.batchSize.WithLabelValues(provider).Observe(float64(size))
	m.batchDuration.WithLabelValues(provider).Observe(elapsed.Seconds())
}

func (m *Metrics) Record(device, provider string, queued bool, err error, elapsed time.Duration) {
	outcome := "success"
	if err != nil {
		outcome = "error"
	} else if queued {
		outcome = "queued"
	}
	m.sent.WithLabelValues(device, provider, outcome).Inc()
	m.duration.WithLabelValues(device, provider).Observe(elapsed.Seconds())
}

func (m *Metrics) RecordDelivery(provider string, err error) {
	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	m.deliveries.WithLabelValues(provider, outcome).Inc()
}

func (m *Metrics) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK); w.Write([]byte("ok\n")) })
	return mux
}

func (m *Metrics) DeviceUp(device, provider string, up bool) {
	value := float64(0)
	if up {
		value = 1
	}
	m.devices.WithLabelValues(device, provider).Set(value)
}
