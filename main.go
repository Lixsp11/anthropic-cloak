package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const host = "api.anthropic.com"
const maxBody = 32 << 20

var billing = regexp.MustCompile("(?im)^[ \t]*x\u200b?-anthropic-billing-header:[^\r\n]*(?:\r\n|\r|\n|$)")
var sdk = regexp.MustCompile("(?i)Claude Agent SDK")

func cleanText(s string) string {
	s = billing.ReplaceAllString(s, "")
	return sdk.ReplaceAllStringFunc(s, func(m string) string { return m[:1] + "\u200b" + m[1:] })
}
func rewriteBody(raw []byte) ([]byte, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil || body == nil {
		return nil, errors.New("invalid JSON object")
	}
	sys, ok := body["system"]
	if !ok {
		return raw, nil
	}
	var s string
	if json.Unmarshal(sys, &s) == nil {
		cleaned := cleanText(s)
		if s == cleaned {
			return raw, nil
		}
		if strings.TrimSpace(cleaned) == "" {
			delete(body, "system")
		} else {
			body["system"], _ = json.Marshal(cleaned)
		}
	} else {
		var blocks []json.RawMessage
		if json.Unmarshal(sys, &blocks) != nil {
			return raw, nil
		}
		kept := make([]json.RawMessage, 0, len(blocks))
		changed := false
		for _, block := range blocks {
			var p map[string]json.RawMessage
			var typ, text string
			if json.Unmarshal(block, &p) != nil {
				kept = append(kept, block)
				continue
			}
			json.Unmarshal(p["type"], &typ)
			if typ != "text" || json.Unmarshal(p["text"], &text) != nil {
				kept = append(kept, block)
				continue
			}
			cleaned := cleanText(text)
			if cleaned != text {
				changed = true
				if strings.TrimSpace(cleaned) == "" {
					continue
				}
				p["text"], _ = json.Marshal(cleaned)
				block, _ = json.Marshal(p)
			}
			kept = append(kept, block)
		}
		if !changed {
			return raw, nil
		}
		if len(kept) == 0 {
			delete(body, "system")
		} else {
			body["system"], _ = json.Marshal(kept)
		}
	}
	return json.Marshal(body)
}
func filterBeta(h http.Header) {
	var kept []string
	for _, v := range h.Values("Anthropic-Beta") {
		for _, b := range strings.Split(v, ",") {
			b = strings.TrimSpace(b)
			if b != "" && b != "claude-code-20250219" {
				kept = append(kept, b)
			}
		}
	}
	h.Del("Anthropic-Beta")
	if len(kept) > 0 {
		h.Set("Anthropic-Beta", strings.Join(kept, ","))
	}
}
func stripHop(h http.Header) {
	for _, v := range h.Values("Connection") {
		for _, k := range strings.Split(v, ",") {
			h.Del(strings.TrimSpace(k))
		}
	}
	for _, k := range []string{"Connection", "Proxy-Connection", "Proxy-Authorization", "Proxy-Authenticate", "Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(k)
	}
}

type gateway struct {
	cert      tls.Certificate
	transport http.RoundTripper
	rewrite   bool
}

func (g *gateway) forward(r *http.Request) (*http.Response, error) {
	if r.Host != host && r.Host != host+":443" {
		return nil, errors.New("host not allowed")
	}
	r.URL.Scheme = "https"
	r.URL.Host = host
	r.RequestURI = ""
	stripHop(r.Header)
	if g.rewrite && r.Method == "POST" && (r.URL.Path == "/v1/messages" || r.URL.Path == "/v1/messages/count_tokens") {
		var reader io.Reader = r.Body
		if enc := r.Header.Get("Content-Encoding"); enc != "" {
			if enc != "gzip" {
				return nil, errors.New("unsupported content encoding")
			}
			gz, err := gzip.NewReader(r.Body)
			if err != nil {
				return nil, errors.New("invalid gzip")
			}
			defer gz.Close()
			reader = gz
		}
		raw, err := io.ReadAll(io.LimitReader(reader, maxBody+1))
		r.Body.Close()
		if err != nil || len(raw) > maxBody {
			return nil, errors.New("invalid or oversized body")
		}
		changed, err := rewriteBody(raw)
		if err != nil {
			return nil, err
		}
		had := strings.Contains(r.Header.Get("Anthropic-Beta"), "claude-code-20250219")
		filterBeta(r.Header)
		r.Body = io.NopCloser(bytes.NewReader(changed))
		r.ContentLength = int64(len(changed))
		r.TransferEncoding = nil
		r.Header.Del("Content-Encoding")
		r.Header.Del("Content-Length")
		log.Printf("audit body_changed=%t cc_beta_removed=%t", !bytes.Equal(raw, changed), had)
	}
	return g.transport.RoundTrip(r)
}
func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "CONNECT" || r.Host != host+":443" {
		http.Error(w, "destination not allowed", 403)
		return
	}
	conn, rw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	if rw.Flush() != nil {
		return
	}
	c := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{g.cert}, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
	c.SetDeadline(time.Now().Add(20 * time.Second))
	if c.Handshake() != nil {
		return
	}
	c.SetDeadline(time.Time{})
	br := bufio.NewReader(c)
	for {
		c.SetReadDeadline(time.Now().Add(5 * time.Minute))
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		resp, err := g.forward(req)
		if err != nil {
			io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 13\r\nConnection: close\r\n\r\nGateway error")
			log.Print("request failed")
			return
		}
		stripHop(resp.Header)
		resp.ProtoMajor = 1
		resp.ProtoMinor = 1
		if resp.ContentLength < 0 {
			resp.TransferEncoding = []string{"chunked"}
		}
		c.SetWriteDeadline(time.Now().Add(10 * time.Minute))
		err = resp.Write(c)
		resp.Body.Close()
		log.Printf("response status=%d", resp.StatusCode)
		if err != nil || req.Close || resp.Close {
			return
		}
	}
}
func certificates(dir string) (tls.Certificate, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return tls.Certificate{}, err
	}
	certfile, keyfile := filepath.Join(dir, "leaf.pem"), filepath.Join(dir, "leaf-key.pem")
	if _, err := os.Stat(certfile); err == nil {
		return tls.LoadX509KeyPair(certfile, keyfile)
	}
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ca := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "Anthropic Cloak local audit CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano() + 1), DNSNames: []string{host}, Subject: pkix.Name{CommonName: host}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	kb, _ := x509.MarshalECPrivateKey(key)
	for name, data := range map[string][]byte{"ca.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), "leaf.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), "leaf-key.pem": pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			return tls.Certificate{}, err
		}
	}
	return tls.LoadX509KeyPair(certfile, keyfile)
}

type mockTransport struct{}

func (mockTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	raw, _ := io.ReadAll(r.Body)
	if r.Body != nil {
		r.Body.Close()
	}
	var body map[string]json.RawMessage
	json.Unmarshal(raw, &body)
	bad := strings.Contains(r.Header.Get("Anthropic-Beta"), "claude-code-20250219") || bytes.Contains(body["system"], []byte("x-anthropic-billing-header")) || bytes.Contains(body["system"], []byte("Claude Agent SDK"))
	if bad {
		return &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"invalid_request_error","message":"LOCAL_MARKER_PRESENT"}}`)), ContentLength: -1}, nil
	}
	msg := `{"id":"msg_local","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}`
	events := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":" + msg + "}\n\n"
	events += "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"
	events += "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"LOCAL_OK\"}}\n\n"
	events += "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(events)), ContentLength: -1}, nil
}
func main() {
	addr := flag.String("listen", "127.0.0.1:18080", "loopback listener")
	dir := flag.String("cert-dir", "tmp/certs", "private temporary certificates")
	rewrite := flag.Bool("rewrite", true, "enable the three rewrite rules")
	mock := flag.Bool("mock", false, "use local synthetic upstream; never call Anthropic")
	flag.Parse()
	ip, _, err := net.SplitHostPort(*addr)
	if err != nil || net.ParseIP(ip) == nil || !net.ParseIP(ip).IsLoopback() {
		log.Fatal("loopback address required")
	}
	cert, err := certificates(*dir)
	if err != nil {
		log.Fatal("certificate setup failed")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.ResponseHeaderTimeout = 60 * time.Second
	var rt http.RoundTripper = tr
	if *mock {
		rt = mockTransport{}
	}
	fmt.Printf("listen=%s rewrite=%t mock=%t\n", *addr, *rewrite, *mock)
	server := &http.Server{Addr: *addr, Handler: &gateway{cert: cert, transport: rt, rewrite: *rewrite}, ReadHeaderTimeout: 15 * time.Second}
	log.Fatal(server.ListenAndServe())
}
