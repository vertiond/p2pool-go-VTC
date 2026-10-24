package metrics

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	RegisterStats(func() Stats {
		return Stats{Shares: 12, VerifiedShares: 10, ShareDifficulty: 0.5, PoolHashrate: 2e6, LocalHashrate: 1e6, Miners: 3, Peers: 4, UptimeSeconds: 60}
	})
	SharesAccepted.Inc()
	SharesRejected.WithLabelValues(RejectStale).Inc()
	SharechainAdded.WithLabelValues("p2p").Add(5)

	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Body)
	for _, want := range []string{
		"p2pool_sharechain_shares 12",
		"p2pool_sharechain_verified_shares 10",
		"p2pool_pool_share_difficulty 0.5",
		"p2pool_pool_hashrate 2e+06",
		"p2pool_local_hashrate 1e+06",
		"p2pool_miners_connected 3",
		"p2pool_peers_connected 4",
		"p2pool_uptime_seconds 60",
		"p2pool_stratum_shares_accepted_total 1",
		`p2pool_stratum_shares_rejected_total{reason="stale"} 1`,
		`p2pool_stratum_shares_rejected_total{reason="low_diff"} 0`,
		`p2pool_sharechain_shares_added_total{source="p2p"} 5`,
		`p2pool_blocks_found_total{source="local"} 0`,
		`p2pool_block_submissions_total{result="failed"} 0`,
		"go_goroutines",
	} {
		if !strings.Contains(string(body), want+"\n") && !strings.Contains(string(body), want+" ") {
			t.Errorf("missing %q", want)
		}
	}
}
