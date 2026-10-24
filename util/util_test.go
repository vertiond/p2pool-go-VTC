package util_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/CADMonkey21/p2pool-go-VTC/util"
	"github.com/CADMonkey21/p2pool-go-VTC/work"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

func TestSha256d(t *testing.T) {
	got := hex.EncodeToString(util.Sha256d([]byte("hello")))
	if want := "9595c9df90075148eb06860365df33584b75bff782a510c6cd4883a419833d50"; got != want {
		t.Fatalf("Sha256d(hello) = %s, want %s", got, want)
	}
	if !bytes.Equal(util.Sha256d(nil), chainhash.DoubleHashB(nil)) {
		t.Fatal("Sha256d of empty input differs from chainhash")
	}
}

func testData(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + 7)
	}
	return b
}

func TestSha256MatchesStdlib(t *testing.T) {
	for _, n := range []int{0, 1, 55, 56, 63, 64, 65, 80, 119, 128, 1000} {
		data := testData(n)
		want := sha256.Sum256(data)
		if got := util.Sum256(data); got != want {
			t.Errorf("Sum256 of %d bytes = %x, want %x", n, got, want)
		}
		d := util.NewSha256()
		for i := 0; i < n; i += 7 {
			end := i + 7
			if end > n {
				end = n
			}
			_, _ = d.Write(data[i:end])
		}
		if got := d.Sum(nil); !bytes.Equal(got, want[:]) {
			t.Errorf("chunked Write of %d bytes = %x, want %x", n, got, want)
		}
		if got := d.Sum(nil); !bytes.Equal(got, want[:]) {
			t.Errorf("second Sum of %d bytes changed to %x", n, got)
		}
	}
}

func TestSha256MarshalBinary(t *testing.T) {
	data := testData(150)
	std := sha256.New()
	_, _ = std.Write(data[:70])
	state, _ := std.(interface{ MarshalBinary() ([]byte, error) }).MarshalBinary()

	d := util.NewSha256()
	if err := d.UnmarshalBinary(state); err != nil {
		t.Fatal(err)
	}
	_, _ = d.Write(data[70:])
	want := sha256.Sum256(data)
	if got := d.Sum(nil); !bytes.Equal(got, want[:]) {
		t.Fatalf("resumed from stdlib state: %x, want %x", got, want)
	}

	d = util.NewSha256()
	_, _ = d.Write(data[:70])
	mine, _ := d.MarshalBinary()
	if !bytes.Equal(mine, state) {
		t.Fatalf("MarshalBinary = %x, stdlib %x", mine, state)
	}
	if err := d.UnmarshalBinary(state[:10]); err == nil {
		t.Fatal("short state accepted")
	}
}

func TestCalcMidState(t *testing.T) {
	data := testData(80)
	std := sha256.New()
	_, _ = std.Write(data[:64])
	state, _ := std.(interface{ MarshalBinary() ([]byte, error) }).MarshalBinary()
	h := state[4 : 4+32]

	var d util.Sha256Digest
	got := d.CalcMidState(data[64:], h, nil, 64)
	if want := sha256.Sum256(data); got != want {
		t.Fatalf("CalcMidState = %x, want %x", got, want)
	}
}

func TestReverseBytes(t *testing.T) {
	in := []byte{1, 2, 3, 4, 5}
	got := util.ReverseBytes(in)
	if !bytes.Equal(got, []byte{5, 4, 3, 2, 1}) {
		t.Fatalf("ReverseBytes = %v", got)
	}
	if in[0] != 1 {
		t.Fatal("ReverseBytes modified its input")
	}
	if len(util.ReverseBytes(nil)) != 0 {
		t.Fatal("ReverseBytes(nil) not empty")
	}
	// chainhash strings are the byte-reversed hash.
	h := chainhash.DoubleHashH([]byte("x"))
	if hex.EncodeToString(util.ReverseBytes(h[:])) != h.String() {
		t.Fatal("reversed hash bytes do not match the hash string")
	}
	if !bytes.Equal(work.ReverseBytes(in), got) {
		t.Fatal("work.ReverseBytes differs")
	}
}

func merkleRoot(leaves []*chainhash.Hash) *chainhash.Hash {
	level := append([]*chainhash.Hash(nil), leaves...)
	for len(level) > 1 {
		if len(level)%2 == 1 {
			level = append(level, level[len(level)-1])
		}
		var next []*chainhash.Hash
		for i := 0; i < len(level); i += 2 {
			h := chainhash.DoubleHashH(append(level[i][:], level[i+1][:]...))
			next = append(next, &h)
		}
		level = next
	}
	return level[0]
}

// Every leaf's branch, from either work helper, must lead back to the tree's root.
func TestMerkleBranchAndRoot(t *testing.T) {
	for n := 1; n <= 9; n++ {
		leaves := make([]*chainhash.Hash, n)
		raw := make([][]byte, n)
		for i := range leaves {
			h := chainhash.DoubleHashH([]byte{byte(n), byte(i)})
			leaves[i] = &h
			raw[i] = h.CloneBytes()
		}
		root := merkleRoot(leaves)
		for i := 0; i < n; i++ {
			fromHashes := work.CalculateMerkleLinkFromHashes(append([]*chainhash.Hash(nil), leaves...), i)
			if got := util.ComputeMerkleRootFromLink(leaves[i], fromHashes, uint64(i)); !got.IsEqual(root) {
				t.Errorf("n=%d i=%d: root from CalculateMerkleLinkFromHashes = %s, want %s", n, i, got, root)
			}
			rawCopy := make([][]byte, n)
			for j := range raw {
				rawCopy[j] = append([]byte(nil), raw[j]...)
			}
			fromBytes := work.CalculateMerkleLink(rawCopy, i)
			if got := util.ComputeMerkleRootFromLink(leaves[i], fromBytes, uint64(i)); !got.IsEqual(root) {
				t.Errorf("n=%d i=%d: root from CalculateMerkleLink = %s, want %s", n, i, got, root)
			}
		}
	}
	leaf := chainhash.DoubleHashH([]byte("only"))
	if got := util.ComputeMerkleRootFromLink(&leaf, nil, 0); !got.IsEqual(&leaf) {
		t.Fatal("empty branch must return the leaf")
	}
}
