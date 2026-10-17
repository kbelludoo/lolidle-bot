package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAI speaks the /chat/completions wire protocol used by OpenAI and every
// "OpenAI-compatible" endpoint (OpenRouter, DeepSeek, Groq, Ollama, vLLM, the
// opencode gateway, ...). Difference between them is only BaseURL + model.
type OpenAI struct {
	BaseURL string
	APIKey  string
	Model   string
	Headers map[string]string
	HTTP    *http.Client
}

func (c *OpenAI) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

type openaiTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

type openaiToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type openaiMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openaiRequest struct {
	Model         string          `json:"model"`
	Messages      []openaiMessage `json:"messages"`
	Tools         []openaiTool    `json:"tools,omitempty"`
	ToolChoice    string          `json:"tool_choice,omitempty"`
	Stream        bool            `json:"stream"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
}

func (c *OpenAI) buildRequest(req Request) openaiRequest {
	model := req.Model
	if model == "" {
		model = c.Model
	}

	msgs := make([]openaiMessage, 0, len(req.Messages)+1)
	if len(req.System) > 0 {
		msgs = append(msgs, openaiMessage{Role: "system", Content: strings.Join(req.System, "\n\n")})
	}
	for _, m := range req.Messages {
		om := openaiMessage{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			var wt openaiToolCall
			wt.ID = tc.ID
			wt.Type = "function"
			wt.Function.Name = tc.Name
			wt.Function.Arguments = tc.Arguments
			om.ToolCalls = append(om.ToolCalls, wt)
		}
		msgs = append(msgs, om)
	}

	out := openaiRequest{
		Model:       model,
		Messages:    msgs,
		Stream:      true,
		ToolChoice:  req.ToolChoice,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	out.StreamOptions = &struct {
		IncludeUsage bool `json:"include_usage"`
	}{IncludeUsage: true}

	for _, t := range req.Tools {
		var wt openaiTool
		wt.Type = "function"
		wt.Function.Name = t.Name
		wt.Function.Description = t.Description
		wt.Function.Parameters = t.Parameters
		out.Tools = append(out.Tools, wt)
	}
	return out
}

func (c *OpenAI) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	for k, v := range c.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.client().Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return nil, fmt.Errorf("openai: %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}

	ch := make(chan Event, 16)
	go c.pump(ctx, resp.Body, ch)
	return ch, nil
}

type openaiChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
}

func (c *OpenAI) pump(ctx context.Context, body io.ReadCloser, ch chan<- Event) {
	defer close(ch)
	defer body.Close()

	rd := newSSEReader(body)
	for {
		if err := ctx.Err(); err != nil {
			ch <- Event{Type: EventError, Err: err}
			return
		}
		ev, ok, err := rd.Next()
		if err != nil {
			ch <- Event{Type: EventError, Err: err}
			return
		}
		if !ok {
			return
		}

		data := strings.TrimSpace(ev.Data)
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			return
		}

		var chunk openaiChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			ch <- Event{Type: EventError, Err: fmt.Errorf("openai: bad chunk: %w", err)}
			return
		}

		if chunk.Usage != nil {
			ch <- Event{Type: EventUsage, Usage: chunk.Usage}
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.ReasoningContent != "" {
				ch <- Event{Type: EventReasoning, Text: choice.Delta.ReasoningContent}
			}
			if choice.Delta.Content != "" {
				ch <- Event{Type: EventText, Text: choice.Delta.Content}
			}
			for _, tc := range choice.Delta.ToolCalls {
				if tc.ID != "" || tc.Function.Name != "" {
					ch <- Event{Type: EventToolStart, Index: tc.Index, ID: tc.ID, Name: tc.Function.Name}
				}
				if tc.Function.Arguments != "" {
					ch <- Event{Type: EventToolDelta, Index: tc.Index, Args: tc.Function.Arguments}
				}
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				ch <- Event{Type: EventFinish, FinishReason: *choice.FinishReason}
			}
		}
	}
}
