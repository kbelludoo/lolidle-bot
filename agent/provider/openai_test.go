package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIStreamAssemblesTextAndToolCalls(t *testing.T) {
	chunks := []string{
		`{"choices":[{"delta":{"role":"assistant","content":"Let me "}}]}`,
		`{"choices":[{"delta":{"content":"check."}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_time"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"tz\":"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"UTC\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth header = %q", got)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := &OpenAI{BaseURL: srv.URL + "/v1", APIKey: "test-key", Model: "test"}
	stream, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	asm := NewAssembler()
	for ev := range stream {
		if err := asm.Add(ev); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	msg := asm.Message()

	if msg.Content != "Let me check." {
		t.Errorf("content = %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Name != "get_time" || msg.ToolCalls[0].ID != "call_1" {
		t.Errorf("tool call = %+v", msg.ToolCalls[0])
	}
	if msg.ToolCalls[0].Arguments != `{"tz":"UTC"}` {
		t.Errorf("args = %q", msg.ToolCalls[0].Arguments)
	}
	if asm.Finish != "tool_calls" {
		t.Errorf("finish = %q", asm.Finish)
	}
	if asm.Usage == nil || asm.Usage.TotalTokens != 19 {
		t.Errorf("usage = %+v", asm.Usage)
	}
}
