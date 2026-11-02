package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCacheScopeCompatibility(t *testing.T) {
	input := []byte(`{
		"cache_control":{"type":"ephemeral","scope":"global","ttl":"1h"},
		"system":[
			{"type":"text","text":"x-anthropic-billing-header: fixture"},
			{"type":"text","text":"Claude Agent SDK","cache_control":{"type":"ephemeral","scope":"global","ttl":"1h"}}
		],
		"tools":[{"name":"fixture","input_schema":{"type":"object","properties":{"cache_control":{"scope":"global"}}},"cache_control":{"type":"ephemeral","scope":"global"}}],
		"messages":[{"role":"assistant","content":[
			{"type":"text","text":"User text is unchanged","cache_control":{"type":"ephemeral","scope":"global"}},
			{"type":"tool_use","id":"fixture","name":"fixture","input":{"cache_control":{"scope":"global"}}}
		]}],
		"large_integer":9007199254740993
	}`)
	got, err := rewriteBody(input)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		CacheControl map[string]string `json:"cache_control"`
		System       []struct {
			Text         string
			CacheControl map[string]string `json:"cache_control"`
		}
		Tools []struct {
			InputSchema  json.RawMessage   `json:"input_schema"`
			CacheControl map[string]string `json:"cache_control"`
		}
		Messages []struct {
			Content []struct {
				Text         string
				Input        json.RawMessage
				CacheControl map[string]string `json:"cache_control"`
			}
		}
		LargeInteger json.RawMessage `json:"large_integer"`
	}
	if err := json.Unmarshal(got, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.System) != 1 || body.System[0].Text != "C\u200blaude Agent SDK" {
		t.Fatal("original rewrite rules lost")
	}
	for _, cc := range []map[string]string{
		body.CacheControl, body.System[0].CacheControl,
		body.Tools[0].CacheControl, body.Messages[0].Content[0].CacheControl,
	} {
		if _, exists := cc["scope"]; exists {
			t.Fatal("global scope retained")
		}
		if cc["type"] != "ephemeral" {
			t.Fatal("cache boundary removed")
		}
	}
	if body.System[0].CacheControl["ttl"] != "1h" || body.CacheControl["ttl"] != "1h" {
		t.Fatal("TTL changed")
	}
	if !bytes.Contains(body.Tools[0].InputSchema, []byte(`"scope":"global"`)) ||
		!bytes.Contains(body.Messages[0].Content[1].Input, []byte(`"scope":"global"`)) {
		t.Fatal("application payload changed")
	}
	if body.Messages[0].Content[0].Text != "User text is unchanged" ||
		string(body.LargeInteger) != "9007199254740993" {
		t.Fatal("content changed")
	}
	again, err := rewriteBody(got)
	if err != nil || !bytes.Equal(got, again) {
		t.Fatal("not idempotent")
	}
}

func TestBillingOnlyPreservesScope(t *testing.T) {
	got, err := rewriteBody([]byte(`{"system":[{"type":"text","text":"x-anthropic-billing-header: fixture"},{"type":"text","text":"Other system text","cache_control":{"type":"ephemeral","scope":"global"}}]}`))
	if err != nil || !bytes.Contains(got, []byte(`"scope":"global"`)) {
		t.Fatal("scope changed without SDK rewrite")
	}
}

func TestOtherCacheScopePreserved(t *testing.T) {
	got, err := rewriteBody([]byte(`{"system":[{"type":"text","text":"Claude Agent SDK","cache_control":{"type":"ephemeral","scope":"workspace"}}]}`))
	if err != nil || !bytes.Contains(got, []byte(`"scope":"workspace"`)) {
		t.Fatal("non-global scope changed")
	}
}
