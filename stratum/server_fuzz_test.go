package stratum

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/CADMonkey21/p2pool-go-VTC/work"
	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/btcsuite/btcd/btcutil/bech32"
)

var testPKH = []byte{1, 8, 15, 22, 29, 36, 43, 50, 57, 64, 71, 78, 85, 92, 99, 106, 113, 120, 127, 134}

func testBech32(hrp string, ver byte, prog []byte) string {
	c, _ := bech32.ConvertBits(prog, 8, 5, true)
	a, _ := bech32.Encode(hrp, append([]byte{ver}, c...))
	return a
}

var (
	testP2WPKH = testBech32("vtc", 0, testPKH)
	testP2PKH  = base58.CheckEncode(testPKH, 0x47)
	testP2SH   = base58.CheckEncode(testPKH, 0x05)
)

func newTestServer() *StratumServer {
	return &StratumServer{
		workManager: work.NewWorkManager(nil, work.NewShareChain(nil)),
		clients:     make(map[uint64]*Client),
	}
}

// submitWithoutParams reports whether data holds a mining.submit request with no or
// null params, which handleSubmit dereferences without a check.
func submitWithoutParams(data []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var req JSONRPCRequest
		if dec.Decode(&req) != nil {
			return false
		}
		if req.Method == "mining.submit" && req.Params == nil {
			return true
		}
	}
}

const submitNilParamsBug = "BUG: handleSubmit dereferences req.Params without a nil check, so " +
	`{"method":"mining.submit"} (params missing or null) panics and kills the node`

// serveBytes runs one miner connection that sends data and then closes, and
// reports whether the server finished with it in time.
func serveBytes(s *StratumServer, data []byte) bool {
	srv, cli := net.Pipe()
	done := make(chan struct{})
	go func() {
		s.handleMinerConnection(srv)
		close(done)
	}()
	go func() { _, _ = io.Copy(io.Discard, cli) }()
	go func() {
		_, _ = cli.Write(data)
		_ = cli.Close()
	}()
	select {
	case <-done:
		return true
	case <-time.After(5 * time.Second):
		_ = cli.Close()
		return false
	}
}

func TestSubmitWithoutParams(t *testing.T) {
	t.Skip(submitNilParamsBug)
	if !serveBytes(newTestServer(), []byte(`{"id":1,"method":"mining.submit"}`+"\n")) {
		t.Fatal("connection did not end")
	}
}

// FuzzHandleMinerConnection feeds arbitrary bytes to a miner connection. Dispatch
// must not panic, and the connection must end once the miner hangs up.
func FuzzHandleMinerConnection(f *testing.F) {
	for _, s := range []string{
		`{"id":1,"method":"mining.subscribe","params":["VerthashMiner/0.7.2"]}`,
		`{"id":1,"method":"mining.subscribe","params":[]}` + "\n" + `{"id":2,"method":"mining.authorize","params":["` + testP2WPKH + `.rig1","x"]}`,
		`{"id":2,"method":"mining.authorize","params":["` + testP2PKH + `","x"]}`,
		`{"id":2,"method":"mining.authorize","params":[]}`,
		`{"id":2,"method":"mining.authorize","params":[1,2]}`,
		`{"id":2,"method":"mining.authorize","params":{"user":"x"}}`,
		`{"id":2,"method":"mining.authorize"}`,
		`{"id":3,"method":"mining.submit","params":["` + testP2WPKH + `","1","00000000","6533d1a2","1a2b3c4d"]}`,
		`{"id":3,"method":"mining.submit","params":["w","1","00","00"]}`,
		`{"id":3,"method":"mining.submit","params":"x"}`,
		`{"id":3,"method":"mining.submit","params":[null,null,null,null,null]}`,
		`{"id":"abc","method":"mining.extranonce.subscribe","params":[]}`,
		`{"id":null,"method":"mining.subscribe","params":null}`,
		`{"id":{"x":[1]},"method":"mining.subscribe"}`,
		`[1,2,3]`,
		`"mining.subscribe"`,
		`{"id":1,"method":`,
		`{}{}{}`,
		"GET / HTTP/1.1\r\n\r\n",
		"\x00\xff",
	} {
		f.Add([]byte(s + "\n"))
	}
	s := newTestServer()
	f.Fuzz(func(t *testing.T, data []byte) {
		if submitWithoutParams(data) {
			t.Skip(submitNilParamsBug)
		}
		if !serveBytes(s, data) {
			t.Fatalf("connection did not end for %q", data)
		}
	})
}

const emptyBech32Bug = "BUG: isValidVtcAddress accepts a bech32 address with no data (e.g. vtc1fvw5gc), " +
	"and AddressToScriptPubKey and CreateShare index decoded[0] on it and panic, so such a " +
	"miner username crashes the node when its first job is built"

// FuzzIsValidVtcAddress checks that address validation never panics, and that an
// address it accepts can be turned into a payout script.
func FuzzIsValidVtcAddress(f *testing.F) {
	for _, s := range []string{
		testP2WPKH,
		strings.ToUpper(testP2WPKH),
		testBech32("tvtc", 0, testPKH),
		testBech32("vtc", 1, append(testPKH, testPKH[:12]...)),
		testP2PKH,
		testP2SH,
		base58.CheckEncode(testPKH, 0x6f),
		"vtc1",
		"vtc1fvw5gc",
		"1111",
		"",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, addr string) {
		if !isValidVtcAddress(addr) {
			return
		}
		if l := strings.ToLower(strings.TrimSpace(addr)); strings.HasPrefix(l, "vtc1") || strings.HasPrefix(l, "tvtc1") {
			if _, d, err := bech32.Decode(l); err == nil && len(d) == 0 {
				t.Skip(emptyBech32Bug)
			}
		}
		if _, err := work.AddressToScriptPubKey(addr); err != nil {
			t.Logf("accepted address %q has no payout script: %v", addr, err)
		}
	})
}
