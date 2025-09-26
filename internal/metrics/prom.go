package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	TotalPackets = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "distributor_total_packets_total",
			Help: "Total packets ingested",
		},
	)
	TotalMessages = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "distributor_total_messages_total",
			Help: "Total messages ingested",
		},
	)
	PacketsByAnalyzer = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "distributor_packets_by_analyzer_total",
			Help: "Packets routed to each analyzer",
		},
		[]string{"analyzer"},
	)
	MessagesByAnalyzer = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "distributor_messages_by_analyzer_total",
			Help: "Messages routed to each analyzer",
		},
		[]string{"analyzer"},
	)
	QueueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "distributor_queue_depth",
			Help: "Per-analyzer queue depth",
		},
		[]string{"analyzer"},
	)
	HealthyAnalyzers = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "distributor_healthy_analyzers",
			Help: "Number of healthy analyzers",
		},
	)
	TotalAnalyzers = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "distributor_total_analyzers",
			Help: "Total number of configured analyzers",
		},
	)
)

func Register() {
	prometheus.MustRegister(
		TotalPackets,
		TotalMessages,
		PacketsByAnalyzer,
		MessagesByAnalyzer,
		QueueDepth,
		HealthyAnalyzers,
		TotalAnalyzers,
	)
}
