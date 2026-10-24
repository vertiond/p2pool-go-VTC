package stratum

import (
	"encoding/json"
	"math"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/CADMonkey21/p2pool-go-VTC/config"
	"github.com/CADMonkey21/p2pool-go-VTC/work"
	"github.com/btcsuite/btcd/btcutil/base58"
)

func TestFormatStratumPrevHash(t *testing.T) {
	// The RPC hash's 4-byte words, in reverse order, each word unchanged.
	in := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	want := "1c1d1e1f18191a1b14151617101112130c0d0e0f08090a0b0405060700010203"
	if got := formatStratumPrevHash(in); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	for _, bad := range []string{"zz", in + "00"} {
		if got := formatStratumPrevHash(bad); got != bad {
			t.Errorf("invalid %q: got %q, want it unchanged", bad, got)
		}
	}
}

func withTestnet(t *testing.T, on bool) {
	old := config.Active.Testnet
	config.Active.Testnet = on
	t.Cleanup(func() { config.Active.Testnet = old })
}

func TestIsValidVtcAddress(t *testing.T) {
	withTestnet(t, false)
	badSum := []byte(testP2PKH)
	badSum[len(badSum)-1] ^= 1
	for addr, want := range map[string]bool{
		testP2WPKH:                         true,
		"  " + testP2WPKH + " ":            true,
		testBech32("VTC", 0, testPKH):      true,
		testBech32("vtc", 0, testPKH[:16]): true,
		testP2PKH:                          true,
		testP2SH:                           true,
		testBech32("tvtc", 0, testPKH):     false,
		testBech32("bc", 0, testPKH):       false,
		base58.CheckEncode(testPKH, 0x00):  false,
		base58.CheckEncode(testPKH, 0x6f):  false,
		string(badSum):                     false,
		testP2WPKH[:len(testP2WPKH)-1]:     false,
		"":                                 false,
		"vtc1":                             false,
	} {
		if got := isValidVtcAddress(addr); got != want {
			t.Errorf("mainnet %q: got %v, want %v", addr, got, want)
		}
	}

	withTestnet(t, true)
	for addr, want := range map[string]bool{
		testBech32("tvtc", 0, testPKH):    true,
		base58.CheckEncode(testPKH, 0x6f): true,
		base58.CheckEncode(testPKH, 0xc4): true,
		testP2WPKH:                        false,
		testP2PKH:                         false,
	} {
		if got := isValidVtcAddress(addr); got != want {
			t.Errorf("testnet %q: got %v, want %v", addr, got, want)
		}
	}
}

func TestTargetToDiff(t *testing.T) {
	diff1, _ := new(big.Int).SetString("000000FFFF000000000000000000000000000000000000000000000000000000", 16)
	if got := TargetToDiff(diff1); got != 1 {
		t.Fatalf("diff 1 target: got %v", got)
	}
	if got := TargetToDiff(new(big.Int).Rsh(diff1, 4)); got != 16 {
		t.Fatalf("diff 1 target / 16: got %v", got)
	}
	for _, z := range []*big.Int{nil, big.NewInt(0), big.NewInt(-5)} {
		if got := TargetToDiff(z); got != 0 {
			t.Errorf("target %v: got %v, want 0", z, got)
		}
	}
	for _, d := range []float64{0.01, 0.5, 1, 3.7, 1e6} {
		got := TargetToDiff(work.DiffToTarget(d))
		if math.Abs(got-d)/d > 1e-9 {
			t.Errorf("DiffToTarget(%v) round-trips to %v", d, got)
		}
		if w := work.TargetToDiff(work.DiffToTarget(d)); w != got {
			t.Errorf("stratum and work TargetToDiff differ for %v: %v vs %v", d, got, w)
		}
	}
}

func TestRateMonitor(t *testing.T) {
	rm := NewRateMonitor(time.Minute)
	if d, span := rm.GetDatumsInLast(time.Minute); len(d) != 0 || span != time.Second {
		t.Fatalf("empty: %d datums over %v", len(d), span)
	}
	for i := 0; i < 5; i++ {
		rm.AddDatum(ShareDatum{Work: float64(i)})
	}
	d, span := rm.GetDatumsInLast(time.Hour)
	if len(d) != 5 || d[4].Work != 4 {
		t.Fatalf("got %d datums", len(d))
	}
	// Spans shorter than a second are reported as a second.
	if span != time.Second {
		t.Fatalf("span %v, want 1s", span)
	}

	rm = NewRateMonitor(50 * time.Millisecond)
	rm.AddDatum(ShareDatum{Work: 1})
	time.Sleep(80 * time.Millisecond)
	rm.AddDatum(ShareDatum{Work: 2})
	d, _ = rm.GetDatumsInLast(time.Hour)
	if len(d) != 1 || d[0].Work != 2 {
		t.Fatalf("after lookback: got %v, want only the newest datum", d)
	}
}

// vardiffStep runs vardiffLoop for one authorized client and returns the next
// difficulty the client is sent.
func vardiffStep(t *testing.T, cur float64, shares int) float64 {
	s := newTestServer()
	srv, cli := net.Pipe()
	t.Cleanup(func() { _ = cli.Close() })
	c := NewClient(srv)
	c.Authorized = true
	c.CurrentDifficulty = cur
	go s.vardiffLoop(c)
	t.Cleanup(func() { s.dropClient(c) })
	time.AfterFunc(400*time.Millisecond, func() {
		for i := 0; i < shares; i++ {
			c.LocalRateMonitor.AddDatum(ShareDatum{Work: cur})
		}
	})
	_ = cli.SetReadDeadline(time.Now().Add(5 * time.Second))
	var msg struct {
		Method string
		Params []float64
	}
	if err := json.NewDecoder(cli).Decode(&msg); err != nil {
		t.Fatalf("no difficulty update: %v", err)
	}
	if msg.Method != "mining.set_difficulty" || len(msg.Params) != 1 {
		t.Fatalf("got %+v", msg)
	}
	return msg.Params[0]
}

func TestVardiffBounds(t *testing.T) {
	old := config.Active.Vardiff
	config.Active.Vardiff = config.VardiffConfig{TargetTime: 5, RetargetTime: 1, MinDiff: 0.05}
	t.Cleanup(func() { config.Active.Vardiff = old })
	poolDiff := newTestServer().workManager.ShareChain.GetPoolShareDifficulty()

	tests := []struct {
		name   string
		cur    float64
		shares int
		want   float64
	}{
		{"no shares halves", 0.3, 0, 0.15},
		{"no shares stops at minDiff", 0.08, 0, 0.05},
		{"fast shares at most double", 0.1, 30, 0.2},
		{"fast shares stop at pool difficulty", 0.4, 30, poolDiff},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := vardiffStep(t, tt.cur, tt.shares); math.Abs(got-tt.want) > 1e-12 {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
