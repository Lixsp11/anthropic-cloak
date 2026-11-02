package main

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBodyRules(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=1;"},{"type":"text","text":"Claude Agent SDK","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"Claude Agent SDK"}]}`, `{"messages":[{"role":"user","content":"Claude Agent SDK"}],"system":[{"cache_control":{"type":"ephemeral"},"text":"C​laude Agent SDK","type":"text"}]}`},
		{`{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=1;"}]}`, `{}`},
		{`{"system":"x-anthropic-billing-header: a\nHello"}`, `{"system":"Hello"}`},
		{`{"system":"Hello","messages":[]}`, `{"system":"Hello","messages":[]}`},
	}
	for _, c := range cases {
		got, e := rewriteBody([]byte(c.in))
		if e != nil || string(got) != c.want {
			t.Fatalf("got %s error %v want %s", got, e, c.want)
		}
		again, e := rewriteBody(got)
		if e != nil || !bytes.Equal(got, again) {
			t.Fatal("not idempotent")
		}
	}
}
func TestBetaList(t *testing.T) {
	h := http.Header{"Anthropic-Beta": {"claude-code-20250219,keep", "other"}}
	filterBeta(h)
	if h.Get("Anthropic-Beta") != "keep,other" {
		t.Fatal(h)
	}
	h.Set("Anthropic-Beta", "claude-code-20250219")
	filterBeta(h)
	if _, ok := h["Anthropic-Beta"]; ok {
		t.Fatal("not removed")
	}
}
func TestCONNECTTransaction(t *testing.T) {
	dir := t.TempDir()
	cert, e := certificates(dir)
	if e != nil {
		t.Fatal(e)
	}
	ca, _ := os.ReadFile(dir + "/ca.pem")
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(ca)
	body := `{"model":"claude-opus-5-5","system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=1;"},{"type":"text","text":"Claude Agent SDK"}],"messages":[{"role":"user","content":"hi"}],"stream":true}`
	for _, c := range []struct {
		name    string
		rewrite bool
		status  int
	}{{"BASELINE", false, 400}, {"MODIFIED", true, 200}, {"ROLLBACK", false, 400}} {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewServer(&gateway{cert: cert, transport: mockTransport{}, rewrite: c.rewrite})
			defer server.Close()
			proxy, _ := url.Parse(server.URL)
			tr := &http.Transport{Proxy: http.ProxyURL(proxy), TLSClientConfig: &tls.Config{RootCAs: roots}}
			defer tr.CloseIdleConnections()
			client := &http.Client{Transport: tr}
			req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", strings.NewReader(body))
			req.Header.Set("Anthropic-Beta", "claude-code-20250219,keep")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			out, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != c.status {
				t.Fatalf("status %d", resp.StatusCode)
			}
			if c.rewrite && !bytes.Contains(out, []byte("LOCAL_OK")) {
				t.Fatal("missing SSE")
			}
			t.Logf("%s status=%d TLS verified; SSE=%t", c.name, resp.StatusCode, bytes.Contains(out, []byte("LOCAL_OK")))
		})
	}
}
func TestGzip(t *testing.T) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	gz.Write([]byte(`{"system":"Claude Agent SDK"}`))
	gz.Close()
	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", &b)
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Anthropic-Beta", "claude-code-20250219")
	g := gateway{rewrite: true, transport: mockTransport{}}
	resp, e := g.forward(req)
	if e != nil || resp.StatusCode != 200 {
		t.Fatal("gzip failed", e)
	}
	resp.Body.Close()
}
func TestDestinationRestriction(t *testing.T) {
	g := gateway{}
	r := httptest.NewRequest("CONNECT", "http://example.com:443", nil)
	r.Host = "example.com:443"
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestStreamingBeforeCompletion(t *testing.T) {
	dir := t.TempDir()
	cert, e := certificates(dir)
	if e != nil {
		t.Fatal(e)
	}
	ca, _ := os.ReadFile(dir + "/ca.pem")
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(ca)
	release := make(chan struct{})
	defer close(release)
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rd, wr := io.Pipe()
		go func() {
			defer wr.Close()
			io.WriteString(wr, "data: first\n\n")
			<-release
			io.WriteString(wr, "data: last\n\n")
		}()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, ContentLength: -1, Body: rd}, nil
	})
	server := httptest.NewServer(&gateway{cert: cert, transport: rt})
	defer server.Close()
	p, _ := url.Parse(server.URL)
	tr := &http.Transport{Proxy: http.ProxyURL(p), TLSClientConfig: &tls.Config{RootCAs: roots}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 2 * time.Second}
	resp, e := client.Get("https://api.anthropic.com/v1/messages")
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	line := make([]byte, len("data: first\n\n"))
	if _, e = io.ReadFull(resp.Body, line); e != nil {
		t.Fatal(e)
	}
	if string(line) != "data: first\n\n" {
		t.Fatal("missing first chunk")
	}
	t.Log("first SSE chunk received before upstream completion")
}
