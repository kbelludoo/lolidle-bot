package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"agent/provider"
)

func main() {
	p, err := newProvider()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}

	prompt := strings.Join(os.Args[1:], " ")
	if prompt == "" {
		prompt = "What time is it right now? Use the tool, then answer in one line."
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := provider.Request{
		System: []string{"You are a terse terminal agent. Prefer tools over guessing."},
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: prompt},
		},
		Tools: []provider.Tool{timeTool()},
	}

	for turn := 0; turn < 6; turn++ {
		msg, err := runTurn(ctx, p, &req)
		if err != nil {
			fmt.Fprintln(os.Stderr, "\nstream:", err)
			os.Exit(1)
		}
		fmt.Printf("\n[turn %d] finish=%q usage=%+v\n", turn, msg.finish, msg.usage)

		req.Messages = append(req.Messages, msg.message)
		if len(msg.message.ToolCalls) == 0 {
			return
		}

		for _, tc := range msg.message.ToolCalls {
			result := runTool(tc)
			fmt.Printf("  tool %s(%s) -> %s\n", tc.Name, tc.Arguments, result)
			req.Messages = append(req.Messages, provider.Message{
				Role:       provider.RoleTool,
				ToolCallID: tc.ID,
				Content:    result,
			})
		}
	}
}

type turnResult struct {
	message provider.Message
	finish  string
	usage   *provider.Usage
}

func runTurn(ctx context.Context, p provider.Provider, req *provider.Request) (turnResult, error) {
	stream, err := p.Stream(ctx, *req)
	if err != nil {
		return turnResult{}, err
	}

	asm := provider.NewAssembler()
	for ev := range stream {
		switch ev.Type {
		case provider.EventText:
			fmt.Print(ev.Text)
		case provider.EventReasoning:
			fmt.Fprintf(os.Stderr, "\x1b[2m%s\x1b[0m", ev.Text)
		case provider.EventToolStart:
			fmt.Fprintf(os.Stderr, "\n[tool %d: %s]\n", ev.Index, ev.Name)
		case provider.EventError:
			return turnResult{}, ev.Err
		}
		if err := asm.Add(ev); err != nil {
			return turnResult{}, err
		}
	}
	return turnResult{message: asm.Message(), finish: asm.Finish, usage: asm.Usage}, nil
}

func timeTool() provider.Tool {
	return provider.Tool{
		Name:        "get_time",
		Description: "Return the current local time in RFC3339 format.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}
}

func runTool(tc provider.ToolCall) string {
	if tc.Name == "get_time" {
		return time.Now().Format(time.RFC3339)
	}
	return fmt.Sprintf("error: unknown tool %q", tc.Name)
}

func newProvider() (provider.Provider, error) {
	kind := strings.ToLower(env("AGENT_PROVIDER", "openai"))
	model := os.Getenv("AGENT_MODEL")

	switch kind {
	case "anthropic":
		key := firstNonEmpty(os.Getenv("AGENT_API_KEY"), os.Getenv("ANTHROPIC_API_KEY"))
		if key == "" {
			return nil, fmt.Errorf("set AGENT_API_KEY or ANTHROPIC_API_KEY")
		}
		return &provider.Anthropic{
			BaseURL: env("AGENT_BASE_URL", "https://api.anthropic.com"),
			APIKey:  key,
			Model:   model,
		}, nil
	case "openai":
		key := firstNonEmpty(os.Getenv("AGENT_API_KEY"), os.Getenv("OPENAI_API_KEY"))
		if key == "" {
			return nil, fmt.Errorf("set AGENT_API_KEY or OPENAI_API_KEY")
		}
		return &provider.OpenAI{
			BaseURL: env("AGENT_BASE_URL", "https://api.openai.com/v1"),
			APIKey:  key,
			Model:   env("AGENT_MODEL", "gpt-4o-mini"),
		}, nil
	default:
		return nil, fmt.Errorf("unknown AGENT_PROVIDER %q (use openai or anthropic)", kind)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
