package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOtherHTTPSIsOpaqueTunnel(t *testing.T) {
	body := []byte("x-anthropic-billing-header: fixture\nClaude Agent SDK")
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(got, body) {
			t.Error("tunnel changed the request body")
		}
		if r.Header.Get("Anthropic-Beta") != "claude-code-20250219" {
			t.Error("tunnel changed the beta header")
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(got)
	}))
	defer upstream.Close()
	proxy := httptest.NewServer(&gateway{rewrite: true})
	defer proxy.Close()
	p, _ := url.Parse(proxy.URL)
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	tr := &http.Transport{
		Proxy:           http.ProxyURL(p),
		TLSClientConfig: &tls.Config{RootCAs: roots},
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
	req, _ := http.NewRequest("POST", upstream.URL+"/v1/messages", bytes.NewReader(body))
	req.Header.Set("Anthropic-Beta", "claude-code-20250219")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("tunnel changed response")
	}
	if !bytes.Equal(resp.TLS.PeerCertificates[0].Raw, upstream.Certificate().Raw) {
		t.Fatal("proxy replaced the upstream TLS certificate")
	}
	t.Log("upstream certificate, beta header, request and response bytes preserved")
}

func TestTunnelBufferedBytesAndHalfClose(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := upstream.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		b, err := io.ReadAll(c)
		if err == nil {
			c.Write(append([]byte("echo:"), b...))
		}
	}()
	proxy := httptest.NewServer(&gateway{})
	defer proxy.Close()
	p, _ := url.Parse(proxy.URL)
	c, err := net.DialTimeout("tcp", p.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	payload := "buffered payload"
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n%s", upstream.Addr(), upstream.Addr(), payload)
	c.(*net.TCPConn).CloseWrite()
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: "CONNECT"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	got, err := io.ReadAll(br)
	if err != nil || string(got) != "echo:"+payload {
		t.Fatalf("got %q error %v", got, err)
	}
	<-done
}

func TestPlainHTTPForwarding(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" || r.Header.Get("X-Hop") != "" {
			t.Error("proxy credentials or hop-by-hop header leaked")
		}
		if r.Header.Get("Anthropic-Beta") != "claude-code-20250219" {
			t.Error("plain HTTP beta value changed")
		}
		io.Copy(w, r.Body)
	}))
	defer upstream.Close()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	defer tr.CloseIdleConnections()
	proxy := httptest.NewServer(&gateway{transport: tr, rewrite: true})
	defer proxy.Close()
	p, _ := url.Parse(proxy.URL)
	ct := &http.Transport{Proxy: http.ProxyURL(p)}
	defer ct.CloseIdleConnections()
	client := &http.Client{Transport: ct, Timeout: 3 * time.Second}
	body := "Claude Agent SDK"
	req, _ := http.NewRequest("POST", upstream.URL+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Proxy-Authorization", "Basic synthetic")
	req.Header.Set("Connection", "X-Hop")
	req.Header.Set("X-Hop", "remove")
	req.Header.Set("Anthropic-Beta", "claude-code-20250219")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if string(got) != body {
		t.Fatalf("got %q", got)
	}
}

func TestMalformedConnectAndMockHTTP(t *testing.T) {
	for _, target := range []string{"missing-port", ":443", "example.com:0", "example.com:65536"} {
		r := httptest.NewRequest("CONNECT", "http://example.com", nil)
		r.Host = target
		w := httptest.NewRecorder()
		(&gateway{}).ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("%s: %d", target, w.Code)
		}
	}
	w := httptest.NewRecorder()
	(&gateway{mock: true}).ServeHTTP(w, httptest.NewRequest("GET", "http://example.com/", nil))
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	(&gateway{}).ServeHTTP(w, httptest.NewRequest("GET", "http://api.anthropic.com/v1/messages", nil))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
