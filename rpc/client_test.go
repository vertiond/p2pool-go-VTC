package rpc

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/CADMonkey21/p2pool-go-VTC/config"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

type fakeNode struct {
	t       *testing.T
	user    string
	pass    string
	status  int
	replies map[string]string // method -> raw JSON-RPC response
	got     []RPCRequest
}

func (f *fakeNode) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if u, p, ok := r.BasicAuth(); !ok || u != f.user || p != f.pass {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
		f.t.Errorf("got %s with content type %q", r.Method, r.Header.Get("Content-Type"))
	}
	var req RPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Errorf("bad request body: %v", err)
	}
	f.got = append(f.got, req)
	if f.status != 0 {
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte("boom"))
		return
	}
	_, _ = w.Write([]byte(f.replies[req.Method]))
}

func newTestClient(t *testing.T, f *fakeNode, cfg config.Config) *Client {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	cfg.RPCHost = host
	cfg.RPCPort, _ = strconv.Atoi(port)
	return NewClient(cfg)
}

func TestCallSendsRequestAndAuth(t *testing.T) {
	f := &fakeNode{t: t, user: "u", pass: "p", replies: map[string]string{
		"getmininginfo": `{"result":{"networkhashps":1.5e12,"difficulty":24.8},"error":null,"id":1}`,
	}}
	c := newTestClient(t, f, config.Config{RPCUser: "u", RPCPass: "p"})
	info, err := c.GetMiningInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.NetworkHashPS != 1.5e12 || info.Difficulty != 24.8 {
		t.Fatalf("got %+v", info)
	}
	if len(f.got) != 1 || f.got[0].Method != "getmininginfo" || f.got[0].Jsonrpc != "2.0" {
		t.Fatalf("server got %+v", f.got)
	}
}

func TestCallErrors(t *testing.T) {
	f := &fakeNode{t: t, user: "u", pass: "p", replies: map[string]string{
		"getmininginfo": `{"result":null,"error":{"code":-10,"message":"Vertcoin is downloading blocks..."},"id":1}`,
		"getblock":      `not json`,
	}}
	c := newTestClient(t, f, config.Config{RPCUser: "u", RPCPass: "p"})

	_, err := c.GetMiningInfo()
	var rerr *RPCError
	if !errors.As(err, &rerr) || rerr.Code != -10 {
		t.Fatalf("RPC error: got %v", err)
	}
	if _, err := c.GetBlock(&chainhash.Hash{}); err == nil {
		t.Fatal("invalid JSON reply: no error")
	}
	f.status = http.StatusInternalServerError
	if _, err := c.Call("getmininginfo", nil); err == nil {
		t.Fatal("HTTP 500: no error")
	}

	bad := newTestClient(t, &fakeNode{t: t, user: "u", pass: "p"}, config.Config{RPCUser: "u", RPCPass: "wrong"})
	if _, err := bad.Call("getmininginfo", nil); err == nil {
		t.Fatal("HTTP 401: no error")
	}
}

func TestCookieAuth(t *testing.T) {
	cookie := filepath.Join(t.TempDir(), ".cookie")
	if err := os.WriteFile(cookie, []byte("__cookie__:s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fakeNode{t: t, user: "__cookie__", pass: "s3cret", replies: map[string]string{
		"getblockheader": `{"result":"00","error":null,"id":1}`,
	}}
	c := newTestClient(t, f, config.Config{RPCCookieFile: cookie})
	if got, err := c.GetRawBlockHeaderHex("ab"); err != nil || got != "00" {
		t.Fatalf("got %q, %v", got, err)
	}
	if p := f.got[0].Params; len(p) != 2 || p[0] != "ab" || p[1] != false {
		t.Fatalf("getblockheader params %v", p)
	}
}

func TestBlockCalls(t *testing.T) {
	f := &fakeNode{t: t, user: "u", pass: "p", replies: map[string]string{
		"getblocktemplate": `{"result":{"height":2400000,"bits":"1b0404cb"},"error":null,"id":1}`,
		"getblockheader":   `{"result":{"hash":"ab","confirmations":3,"height":7,"bits":"1b0404cb"},"error":null,"id":1}`,
		"getblock":         `{"result":{"hash":"ab","confirmations":101,"height":7},"error":null,"id":1}`,
		"submitblock":      `{"result":null,"error":null,"id":1}`,
	}}
	c := newTestClient(t, f, config.Config{RPCUser: "u", RPCPass: "p"})

	raw, err := c.GetBlockTemplate()
	if err != nil {
		t.Fatal(err)
	}
	var tmpl struct {
		Height int64
		Bits   string
	}
	if json.Unmarshal(raw, &tmpl) != nil || tmpl.Height != 2400000 {
		t.Fatalf("template %s", raw)
	}
	rules, _ := json.Marshal(f.got[0].Params)
	if string(rules) != `[{"rules":["segwit"]}]` {
		t.Fatalf("getblocktemplate params %s", rules)
	}

	if h, err := c.GetBlockHeader(&chainhash.Hash{}); err != nil || h.Confirmations != 3 || h.Height != 7 {
		t.Fatalf("header %+v, %v", h, err)
	}
	if b, err := c.GetBlock(&chainhash.Hash{}); err != nil || b.Confirmations != 101 {
		t.Fatalf("block %+v, %v", b, err)
	}

	blk := wire.NewMsgBlock(&wire.BlockHeader{Version: 1})
	if err := c.SubmitBlock(blk); err != nil {
		t.Fatalf("submitblock null result: %v", err)
	}
	f.replies["submitblock"] = `{"result":"high-hash","error":null,"id":1}`
	if err := c.SubmitBlock(blk); err == nil {
		t.Fatal("submitblock rejection: no error")
	}
}

func TestProposeBlock(t *testing.T) {
	f := &fakeNode{t: t, user: "u", pass: "p", replies: map[string]string{}}
	c := newTestClient(t, f, config.Config{RPCUser: "u", RPCPass: "p"})
	blk := wire.NewMsgBlock(&wire.BlockHeader{Version: 1})
	for reply, want := range map[string]string{
		`{"result":null,"error":null,"id":1}`:              "accepted",
		`{"result":"","error":null,"id":1}`:                "accepted",
		`{"result":"bad-cb-amount","error":null,"id":1}`:   "bad-cb-amount",
		`{"result":{"weird":true},"error":null,"id":1}`:    `{"weird":true}`,
		`{"result":"bad-txnmrklroot","error":null,"id":1}`: "bad-txnmrklroot",
	} {
		f.replies["getblocktemplate"] = reply
		if got, err := c.ProposeBlock(blk); err != nil || got != want {
			t.Errorf("reply %s: got %q, %v; want %q", reply, got, err, want)
		}
	}
	var p struct {
		Mode  string
		Data  string
		Rules []string
	}
	b, _ := json.Marshal(f.got[0].Params[0])
	if json.Unmarshal(b, &p) != nil || p.Mode != "proposal" || p.Data == "" || len(p.Rules) != 1 {
		t.Fatalf("proposal params %s", b)
	}
}
