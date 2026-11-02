package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestInteractiveIdentityRewrite(t *testing.T) {
	input := []byte(`{"system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude.","cache_control":{"type":"ephemeral","scope":"global","ttl":"1h"}}],"messages":[{"role":"user","content":"Claude Code"}]}`)
	got, err := rewriteBody(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("C\u200blaude Code")) ||
		!bytes.Contains(got, []byte(`"content":"Claude Code"`)) ||
		bytes.Contains(got, []byte(`"scope":"global"`)) ||
		!bytes.Contains(got, []byte(`"ttl":"1h"`)) {
		t.Fatalf("unexpected body: %s", got)
	}
	again, err := rewriteBody(got)
	if err != nil || !bytes.Equal(again, got) {
		t.Fatal("not idempotent")
	}
}

func TestCLIUserAgentFiltering(t *testing.T) {
	for _, c := range []struct {
		name, ua, path, method string
		rewrite, removed       bool
	}{
		{"interactive", "claude-cli/2.1.286 (external, cli)", "/v1/messages", "POST", true, true},
		{"sdk", "claude-cli/2.1.286 (external, sdk-cli)", "/v1/messages", "POST", true, true},
		{"token_count", "claude-cli/2.1.286", "/v1/messages/count_tokens", "POST", true, true},
		{"comparison", "claude-cli/2.1.286", "/v1/messages", "POST", false, false},
		{"other_agent", "example-client/1.0", "/v1/messages", "POST", true, false},
		{"other_path", "claude-cli/2.1.286", "/api/hello", "POST", true, false},
		{"get_request", "claude-cli/2.1.286", "/v1/messages", "GET", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, _ := http.NewRequest(c.method, "https://api.anthropic.com"+c.path, strings.NewReader(`{"system":"Claude Code"}`))
			r.Header.Set("User-Agent", c.ua)
			r.Header.Set("Authorization", "Bearer synthetic")
			r.Header.Set("X-Api-Key", "synthetic")
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if c.removed {
					if req.Header.Get("User-Agent") != "" {
						t.Error("CLI identity retained")
					}
				} else if req.Header.Get("User-Agent") != c.ua {
					t.Error("unrelated User-Agent changed")
				}
				if req.Header.Get("Authorization") != "Bearer synthetic" || req.Header.Get("X-Api-Key") != "synthetic" {
					t.Error("authentication changed")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("OK"))}, nil
			})
			g := gateway{rewrite: c.rewrite, transport: rt}
			resp, err := g.forward(r)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
		})
	}
}
