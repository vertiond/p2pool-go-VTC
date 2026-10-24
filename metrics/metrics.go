// Package metrics exposes the node's Prometheus metrics.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "p2pool"

// Share rejection reasons.
const (
	RejectStale     = "stale"
	RejectDuplicate = "duplicate"
	RejectLowDiff   = "low_diff"
	RejectMalformed = "malformed"
	RejectError     = "error"
)

var (
	SharesAccepted = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "stratum_shares_accepted_total",
		Help:      "Stratum shares accepted from local miners.",
	})
	SharesRejected = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "stratum_shares_rejected_total",
		Help:      "Stratum shares rejected from local miners, by reason.",
	}, []string{"reason"})
	SharechainAdded = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "sharechain_shares_added_total",
		Help:      "Shares verified into the sharechain, by source (local or p2p).",
	}, []string{"source"})
	BlocksFound = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "blocks_found_total",
		Help:      "Shares that met the network target, by source (local or p2p).",
	}, []string{"source"})
	BlockSubmissions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "block_submissions_total",
		Help:      "Local block submissions, by result (accepted, relayed or failed).",
	}, []string{"result"})
)

// Stats is a snapshot of the node's gauges, read on every scrape.
type Stats struct {
	Shares          int
	VerifiedShares  int
	ShareDifficulty float64
	PoolHashrate    float64
	LocalHashrate   float64
	Miners          int
	Peers           int
	UptimeSeconds   float64
}

var (
	sharesDesc    = desc("sharechain_shares", "Shares held in the sharechain.")
	verifiedDesc  = desc("sharechain_verified_shares", "Verified shares held in the sharechain.")
	shareDiffDesc = desc("pool_share_difficulty", "Current pool share difficulty.")
	poolHRDesc    = desc("pool_hashrate", "Estimated pool hashrate in H/s.")
	localHRDesc   = desc("local_hashrate", "Estimated hashrate of local miners in H/s.")
	minersDesc    = desc("miners_connected", "Connected stratum miners.")
	peersDesc     = desc("peers_connected", "Connected P2P peers.")
	uptimeDesc    = desc("uptime_seconds", "Node uptime in seconds.")

	// The node's own metrics live in reg; the default registry holds the Go runtime,
	// process and libp2p metrics.
	reg      = prometheus.NewRegistry()
	gatherer = prometheus.Gatherers{prometheus.DefaultGatherer, reg}
)

func desc(name, help string) *prometheus.Desc {
	return prometheus.NewDesc(prometheus.BuildFQName(namespace, "", name), help, nil, nil)
}

func init() {
	reg.MustRegister(SharesAccepted, SharesRejected, SharechainAdded, BlocksFound, BlockSubmissions)
	for _, r := range []string{RejectStale, RejectDuplicate, RejectLowDiff, RejectMalformed, RejectError} {
		SharesRejected.WithLabelValues(r)
	}
	for _, s := range []string{"local", "p2p"} {
		SharechainAdded.WithLabelValues(s)
		BlocksFound.WithLabelValues(s)
	}
	for _, r := range []string{"accepted", "relayed", "failed"} {
		BlockSubmissions.WithLabelValues(r)
	}
}

type collector func() Stats

func (c collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{sharesDesc, verifiedDesc, shareDiffDesc, poolHRDesc, localHRDesc, minersDesc, peersDesc, uptimeDesc} {
		ch <- d
	}
}

func (c collector) Collect(ch chan<- prometheus.Metric) {
	s := c()
	g := func(d *prometheus.Desc, v float64) { ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v) }
	g(sharesDesc, float64(s.Shares))
	g(verifiedDesc, float64(s.VerifiedShares))
	g(shareDiffDesc, s.ShareDifficulty)
	g(poolHRDesc, s.PoolHashrate)
	g(localHRDesc, s.LocalHashrate)
	g(minersDesc, float64(s.Miners))
	g(peersDesc, float64(s.Peers))
	g(uptimeDesc, s.UptimeSeconds)
}

// RegisterStats makes every scrape report the gauges returned by f. Call it once.
func RegisterStats(f func() Stats) {
	reg.MustRegister(collector(f))
}

// Handler serves the node's metrics.
func Handler() http.Handler {
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{})
}
