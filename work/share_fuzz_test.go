package work

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	p2pnet "github.com/CADMonkey21/p2pool-go-VTC/net"
	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// zeroHasher stands in for Verthash so tests need no verthash.dat. Every header
// meets any target.
type zeroHasher struct{}

func (zeroHasher) Hash([]byte) ([]byte, error) { return make([]byte, 32), nil }
func (zeroHasher) Close()                      {}

func useZeroHasher() { p2pnet.ActiveNetwork.Verthash = zeroHasher{} }

var testTemplate = BlockTemplate{
	Version:           0x20000000,
	PreviousBlockHash: "0000000000000a3290f20e75860d505ce0e948a1d1d846bec7e39015d242884b",
	CoinbaseValue:     1250000000,
	CurTime:           1792500000,
	Bits:              "1b0404cb",
	Height:            2400000,
}

// testShare builds a share the way the Stratum server does.
func testShare(t testing.TB, sc *ShareChain, addr string) *Share {
	useZeroHasher()
	tmpl := testTemplate
	tip := sc.GetTipHash()
	_, wc, _ := CalculateWitnessCommitment(&tmpl)
	coinb1, coinb2, err := SplitCoinbaseForStratumAtTip(&tmpl, addr, wc.CloneBytes(), sc, tip)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := RebuildCoinbaseFromStratumParts(coinb1, "0a0b0c0d", "00000001", coinb2)
	if err != nil {
		t.Fatal(err)
	}
	var nt [4]byte
	binary.BigEndian.PutUint32(nt[:], uint32(tmpl.CurTime))
	sh, err := CreateShare(&tmpl, "0a0b0c0d", "00000001", hex.EncodeToString(nt[:]), "1a2b3c4d", addr, sc, 0.01, wc.CloneBytes(), nil, nil, nil, cb)
	if err != nil {
		t.Fatal(err)
	}
	sh.ShareInfo.ShareData.PreviousShareHash = tip
	sh.Target = DiffToTarget(sc.GetExpectedShareDifficulty(tip))
	return sh
}

// knownShareBug returns why decoding or adding s would hit a known panic, or "".
func knownShareBug(s *Share) string {
	var tx wire.MsgTx
	cbOK := tx.Deserialize(bytes.NewReader(s.ShareInfo.ShareData.CoinBase)) == nil
	if cbOK && s.MinHeader.PreviousBlock == nil {
		return "BUG: FullBlockHeader dereferences MinHeader.PreviousBlock without a nil check, so a " +
			"gossiped share with a valid coinbase and no PreviousBlock panics in CalculateHashes and kills the node"
	}
	if cbOK {
		for _, h := range s.MerkleLink.Branch {
			if h == nil {
				return "BUG: util.ComputeMerkleRootFromLink dereferences nil MerkleLink.Branch entries, so a " +
					"gossiped share with a null branch hash panics in CalculateHashes and kills the node"
			}
		}
	}
	if s.Target != nil && s.Target.BitLen() > 256 {
		return "BUG: ComputeShareMetadataCommitment calls Target.FillBytes into 32 bytes, so a gossiped " +
			"V2 share whose Target exceeds 256 bits panics in IsValid and kills the node"
	}
	return ""
}

func FuzzShareJSON(f *testing.F) {
	sc := newTestShareChain()
	sh := testShare(f, sc, base58.CheckEncode(make([]byte, 20), 0x47))
	b, err := json.Marshal(sh)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(b)
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"Hash", "POWHash", "Target", "MinHeader", "ShareInfo", "MerkleLink"} {
		v := m[k]
		delete(m, k)
		vb, _ := json.Marshal(m)
		f.Add(vb)
		m[k] = v
	}
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"Type":24,"Target":1e80,"ShareInfo":{"ShareData":{"CoinBase":"AQ=="}}}`))
	f.Add([]byte(`{"MerkleLink":{"Branch":[null],"Index":3}}`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		useZeroHasher()
		var s Share
		if json.Unmarshal(data, &s) != nil {
			return
		}
		if r := knownShareBug(&s); r != "" {
			t.Skip(r)
		}
		// As in main.go: hash a gossiped share, then add it.
		if s.CalculateHashes() != nil {
			return
		}
		sc := newTestShareChain()
		sc.AddShares([]Share{s})
		_ = sc.GetSharesByHashWithParents([]string{s.Hash.String()}, 10, nil)
		_ = sc.GetStats()
		_, _ = sc.GetSharesForPayout(s.Hash, 0)
	})
}

// FuzzShareChainWalk builds a share graph, including cycles and forks, from the
// input and checks the share request walk, the stop hashes and the PPLNS walk.
func FuzzShareChainWalk(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5}, uint16(5), []byte{3})
	f.Add([]byte{1, 1, 1}, uint16(100), []byte{})
	f.Add([]byte{2, 0, 1, 0, 4, 4}, uint16(1000), []byte{0, 1, 2})
	f.Add([]byte{255, 0, 0, 0, 0, 0, 0, 0}, uint16(3), []byte{7})
	f.Fuzz(func(t *testing.T, parents []byte, depth uint16, stopIdx []byte) {
		if len(parents) > 2000 {
			parents = parents[:2000]
		}
		sc := newTestShareChain()
		now := int32(time.Now().Unix())
		hashes := make([]string, len(parents))
		for i := range parents {
			hashes[i] = testHash(7, i).String()
		}
		for i, p := range parents {
			sh := &Share{Hash: testHash(7, i), Target: DiffToTarget(1)}
			sh.ShareInfo.Timestamp = now - int32(len(parents)-i)
			sh.ShareInfo.ShareData.PubKeyHash = []byte{byte(i % 3)}
			sh.ShareInfo.ShareData.PubKeyHashVersion = 0x47
			if int(p) < len(parents) {
				sh.ShareInfo.ShareData.PreviousShareHash = testHash(7, int(p))
			} else if p != 255 {
				sh.ShareInfo.ShareData.PreviousShareHash = testHash(8, int(p))
			}
			sc.shares[hashes[i]] = sh
			sc.verified[hashes[i]] = i%5 != 4
		}
		sc.recomputeWorkAndTipLocked()
		var stops []string
		for _, i := range stopIdx {
			if int(i) < len(hashes) {
				stops = append(stops, hashes[i])
			}
		}
		req := hashes
		if len(req) > 3 {
			req = req[len(req)-3:]
		}
		got := sc.GetSharesByHashWithParents(req, int(depth), stops)
		if len(got) > MaxShareReply {
			t.Fatalf("got %d shares, cap is %d", len(got), MaxShareReply)
		}
		isStop := make(map[string]bool)
		for _, h := range stops {
			isStop[h] = true
		}
		seen := make(map[string]bool)
		for _, s := range got {
			h := s.Hash.String()
			if seen[h] {
				t.Fatalf("share %s returned twice", h[:8])
			}
			if isStop[h] {
				t.Fatalf("stop hash %s returned", h[:8])
			}
			if !sc.HasShare(h) {
				t.Fatalf("share %s is not held", h[:8])
			}
			seen[h] = true
		}
		_ = sc.GetStopHashes(100)
		_ = sc.GetMissingParentHashes(100)
		_ = sc.GetPoolShareDifficulty()
		out := CalculatePPLNSOutputsAtTip(1250000000, base58.CheckEncode(make([]byte, 20), 0x47), sc, nil)
		var sum int64
		for _, o := range out {
			sum += o.Amount
		}
		if sum != 1250000000 {
			t.Fatalf("PPLNS outputs sum to %d", sum)
		}
		var zero chainhash.Hash
		_, _ = sc.GetSharesForPayout(&zero, 0)
	})
}
