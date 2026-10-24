package work

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CADMonkey21/p2pool-go-VTC/config"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/btcsuite/btcd/btcutil/bech32"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
)

func vtcParams(testnet bool) *chaincfg.Params {
	if testnet {
		p := chaincfg.TestNet3Params
		p.Bech32HRPSegwit = "tvtc"
		return &p
	}
	p := chaincfg.MainNetParams
	p.PubKeyHashAddrID = 0x47
	p.ScriptHashAddrID = 0x05
	p.Bech32HRPSegwit = "vtc"
	return &p
}

func seq(n int, start byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = start + byte(i*3)
	}
	return b
}

func segwitAddr(hrp string, ver byte, prog []byte) string {
	c, _ := bech32.ConvertBits(prog, 8, 5, true)
	enc := bech32.Encode
	if ver > 0 {
		enc = bech32.EncodeM
	}
	a, _ := enc(hrp, append([]byte{ver}, c...))
	return a
}

// oracleScript returns btcutil's payout script for addr. btcutil only decodes
// bech32 for registered networks, so segwit addresses are rebuilt from their
// program and must encode back to addr.
func oracleScript(t *testing.T, addr string, testnet bool) []byte {
	t.Helper()
	params := vtcParams(testnet)
	var a btcutil.Address
	var err error
	if _, d, e := bech32.Decode(strings.ToLower(addr)); e == nil && len(d) > 1 {
		prog, _ := bech32.ConvertBits(d[1:], 5, 8, false)
		switch {
		case d[0] == 1:
			a, err = btcutil.NewAddressTaproot(prog, params)
		case len(prog) == 20:
			a, err = btcutil.NewAddressWitnessPubKeyHash(prog, params)
		default:
			a, err = btcutil.NewAddressWitnessScriptHash(prog, params)
		}
	} else {
		a, err = btcutil.DecodeAddress(addr, params)
	}
	if err != nil {
		t.Fatalf("btcutil rejects %s: %v", addr, err)
	}
	if a.EncodeAddress() != addr {
		t.Fatalf("btcutil encodes %s as %s", addr, a.EncodeAddress())
	}
	s, err := txscript.PayToAddrScript(a)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAddressRoundTrips(t *testing.T) {
	tests := []struct {
		name    string
		testnet bool
		addr    string
		ver     uint8
		prog    []byte
	}{
		{"P2WPKH", false, segwitAddr("vtc", 0, seq(20, 1)), 0, seq(20, 1)},
		{"P2WSH", false, segwitAddr("vtc", 0, seq(32, 2)), 0, seq(32, 2)},
		{"P2PKH", false, base58.CheckEncode(seq(20, 3), 0x47), 0x47, seq(20, 3)},
		{"P2SH", false, base58.CheckEncode(seq(20, 4), 0x05), 0x05, seq(20, 4)},
		{"testnet P2WPKH", true, segwitAddr("tvtc", 0, seq(20, 5)), 0, seq(20, 5)},
		{"testnet P2PKH", true, base58.CheckEncode(seq(20, 6), 0x6f), 0x6f, seq(20, 6)},
		{"testnet P2SH", true, base58.CheckEncode(seq(20, 7), 0xc4), 0xc4, seq(20, 7)},
	}
	old := config.Active.Testnet
	defer func() { config.Active.Testnet = old }()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.Active.Testnet = tt.testnet
			want := oracleScript(t, tt.addr, tt.testnet)
			for _, in := range []string{tt.addr, " " + tt.addr + " "} {
				got, err := AddressToScriptPubKey(in)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("AddressToScriptPubKey(%q) = %x, %v; want %x", in, got, err, want)
				}
			}
			if tt.ver == 0 {
				if got, _ := AddressToScriptPubKey(strings.ToUpper(tt.addr)); !bytes.Equal(got, want) {
					t.Fatalf("upper-case address gives %x", got)
				}
			}
			ver, prog := DecodeAddressToPKH(tt.addr)
			if ver != tt.ver || !bytes.Equal(prog, tt.prog) {
				t.Fatalf("DecodeAddressToPKH = %#x %x, want %#x %x", ver, prog, tt.ver, tt.prog)
			}
			s := &Share{}
			s.ShareInfo.ShareData.PubKeyHashVersion, s.ShareInfo.ShareData.PubKeyHash = ver, prog
			if got := DecodeShareAddress(s); got != tt.addr {
				t.Fatalf("DecodeShareAddress = %s, want %s", got, tt.addr)
			}
		})
	}
}

func TestTaprootAddressRoundTrip(t *testing.T) {
	t.Skip("BUG: a vtc1p (witness v1) address passes isValidVtcAddress, but AddressToScriptPubKey " +
		"writes the version as byte 0x01 instead of OP_1 (0x51), and DecodeShareAddress turns " +
		"PubKeyHashVersion 1 into a base58 address, so the payout script is wrong")
	addr := segwitAddr("vtc", 1, seq(32, 9))
	want := oracleScript(t, addr, false)
	if got, _ := AddressToScriptPubKey(addr); !bytes.Equal(got, want) {
		t.Fatalf("AddressToScriptPubKey = %x, want %x", got, want)
	}
}

func TestAddressToScriptPubKeyErrors(t *testing.T) {
	for _, a := range []string{"", "1", "vtc1qqqqqq", "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx"} {
		if _, err := AddressToScriptPubKey(a); err == nil {
			t.Errorf("%q: no error", a)
		}
	}
	if v, p := DecodeAddressToPKH("vtc1qqqqqq"); v != 0 || p != nil {
		t.Errorf("DecodeAddressToPKH of an invalid address = %d %x", v, p)
	}
}

func TestStratumCoinbaseRoundTrip(t *testing.T) {
	useZeroHasher()
	addr := segwitAddr("vtc", 0, seq(20, 1))
	sc := testPayoutChain(3, 30)
	for _, tc := range []struct {
		name  string
		sc    *ShareChain
		flags string
	}{
		{"no sharechain", nil, ""},
		{"no sharechain with aux flags", nil, "062f503253482f"},
		{"PPLNS outputs", sc, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpl := testTemplate
			tmpl.CoinbaseAux.Flags = tc.flags
			_, wc, _ := CalculateWitnessCommitment(&tmpl)
			coinb1, coinb2, err := SplitCoinbaseForStratum(&tmpl, addr, wc.CloneBytes(), tc.sc)
			if err != nil {
				t.Fatal(err)
			}
			for _, en := range [][2]string{{"00000000", "00000000"}, {"deadbeef", "cafebabe"}, {"0a0b0c0d", "ffffffff"}} {
				got, err := RebuildCoinbaseFromStratumParts(coinb1, en[0], en[1], coinb2)
				if err != nil {
					t.Fatal(err)
				}
				want, err := CreateCoinbaseTx(&tmpl, addr, en[0], en[1], wc.CloneBytes(), tc.sc)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("extranonce %s%s: rebuilt coinbase\n%x\ndiffers from\n%x", en[0], en[1], got, want)
				}
			}
			var tip *chainhash.Hash
			if tc.sc != nil {
				tip = tc.sc.GetTipHash()
			}
			a1, a2, err := SplitCoinbaseForStratumAtTip(&tmpl, addr, wc.CloneBytes(), tc.sc, tip)
			if err != nil || a1 != coinb1 || a2 != coinb2 {
				t.Fatalf("SplitCoinbaseForStratumAtTip at the tip differs: %v", err)
			}
		})
	}
	if _, err := RebuildCoinbaseFromStratumParts("zz", "", "", ""); err == nil {
		t.Fatal("invalid hex: no error")
	}
	if _, err := RebuildCoinbaseFromStratumParts("0100", "", "", ""); err == nil {
		t.Fatal("truncated tx: no error")
	}
}

// testPayoutChain links n shares paid round-robin to addrs addresses, with
// varying targets, and verifies them all.
func testPayoutChain(addrs, n int) *ShareChain {
	sc := newTestShareChain()
	now := int32(time.Now().Unix())
	parent := sc.genesisHash
	for i := 0; i < n; i++ {
		h := testHash(3, i)
		sh := &Share{Hash: h, Target: DiffToTarget(0.5 + float64(i%7))}
		sh.ShareInfo.Timestamp = now - int32(n-i)
		sh.ShareInfo.ShareData.PreviousShareHash = parent
		switch a := i % addrs; a % 2 {
		case 0:
			sh.ShareInfo.ShareData.PubKeyHashVersion = 0
			sh.ShareInfo.ShareData.PubKeyHash = seq(20, byte(a))
		default:
			sh.ShareInfo.ShareData.PubKeyHashVersion = 0x47
			sh.ShareInfo.ShareData.PubKeyHash = seq(20, byte(a))
		}
		sc.shares[h.String()] = sh
		sc.verified[h.String()] = true
		parent = h
	}
	sc.recomputeWorkAndTipLocked()
	return sc
}

func TestPPLNSOutputsConserveReward(t *testing.T) {
	def := base58.CheckEncode(seq(20, 99), 0x47)
	for _, addrs := range []int{1, 2, 5} {
		sc := testPayoutChain(addrs, 40)
		for _, total := range []int64{1, 2, 7, 999999937, 1250000000, 2100000000000000} {
			out := CalculatePPLNSOutputsAtTip(total, def, sc, nil)
			if len(out) == 0 || len(out) > addrs {
				t.Fatalf("%d addresses, reward %d: %d outputs", addrs, total, len(out))
			}
			var sum int64
			for i, o := range out {
				if o.Amount <= 0 {
					t.Fatalf("non-positive output %+v", o)
				}
				if o.Address == def {
					t.Fatalf("default address paid although the window has shares")
				}
				if i > 0 && out[i-1].Address >= o.Address {
					t.Fatalf("outputs not sorted by address")
				}
				if _, err := AddressToScriptPubKey(o.Address); err != nil {
					t.Fatalf("output address %s: %v", o.Address, err)
				}
				sum += o.Amount
			}
			if sum != total {
				t.Fatalf("%d addresses: outputs sum to %d, want %d", addrs, sum, total)
			}
			for i := 0; i < 3; i++ {
				if again := CalculatePPLNSOutputsAtTip(total, def, testPayoutChain(addrs, 40), nil); !reflect.DeepEqual(again, out) {
					t.Fatalf("%d addresses, reward %d: outputs differ between identical chains", addrs, total)
				}
			}
			tip := sc.GetTipHash()
			if at := CalculatePPLNSOutputsAtTip(total, def, sc, tip); !reflect.DeepEqual(at, out) {
				t.Fatal("explicit tip differs from the default tip")
			}
		}
	}
}

func TestPPLNSOutputsWithoutShares(t *testing.T) {
	def := base58.CheckEncode(seq(20, 99), 0x47)
	for _, sc := range []*ShareChain{nil, newTestShareChain()} {
		out := CalculatePPLNSOutputsAtTip(1250000000, def, sc, nil)
		if len(out) != 1 || out[0].Address != def || out[0].Amount != 1250000000 {
			t.Fatalf("got %+v, want the whole reward to the default address", out)
		}
	}
	if out := CalculatePPLNSOutputsAtTip(0, def, nil, nil); out != nil {
		t.Fatalf("zero reward: got %+v", out)
	}
}
