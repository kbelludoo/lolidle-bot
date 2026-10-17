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

// Anthropic speaks the /v1/messages wire protocol. Same normalized Event
// stream out the other side as OpenAI, so the agent loop is provider-agnostic.
type Anthropic struct {
	BaseURL string
	APIKey  string
	Model   string
	Version string
	Headers map[string]string
	HTTP    *http.Client
}

func (c *Anthropic) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Anthropic) version() string {
	if c.Version != "" {
		return c.Version
	}
	return "2023-06-01"
}

type anthropicBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      []anthropicBlock   `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
	Stream      bool               `json:"stream"`
}

func (c *Anthropic) buildRequest(req Request) anthropicRequest {
	model := req.Model
	if model == "" {
		model = c.Model
	}
	max := 4096
	if req.MaxTokens != nil {
		max = *req.MaxTokens
	}

	out := anthropicRequest{
		Model:       model,
		MaxTokens:   max,
		Messages:    toAnthropicMessages(req.Messages),
		Temperature: req.Temperature,
		Stream:      true,
	}
	for _, s := range req.System {
		out.System = append(out.System, anthropicBlock{Type: "text", Text: s})
	}
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}
	return out
}

// toAnthropicMessages flattens our neutral messages into Anthropic's
// role/content-block shape, folding consecutive tool results into one user turn.
func toAnthropicMessages(in []Message) []anthropicMessage {
	var out []anthropicMessage
	var pendingToolResults []anthropicBlock

	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		out = append(out, anthropicMessage{Role: "user", Content: pendingToolResults})
		pendingToolResults = nil
	}

	for _, m := range in {
		if m.Role == RoleTool {
			pendingToolResults = append(pendingToolResults, anthropicBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			})
			continue
		}
		flushToolResults()

		var blocks []anthropicBlock
		if m.Role == RoleAssistant {
			if m.Content != "" {
				blocks = append(blocks, anthropicBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				input := json.RawMessage(tc.Arguments)
				if len(input) == 0 {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, anthropicBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: input,
				})
			}
		} else {
			blocks = append(blocks, anthropicBlock{Type: "text", Text: m.Content})
		}
		out = append(out, anthropicMessage{Role: string(m.Role), Content: blocks})
	}
	flushToolResults()
	return out
}

func (c *Anthropic) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(c.BaseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		httpReq.Header.Set("x-api-key", c.APIKey)
	}
	httpReq.Header.Set("anthropic-version", c.version())
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
		return nil, fmt.Errorf("anthropic: %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}

	ch := make(chan Event, 16)
	go c.pump(ctx, resp.Body, ch)
	return ch, nil
}

func (c *Anthropic) pump(ctx context.Context, body io.ReadCloser, ch chan<- Event) {
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
		if !c.dispatch(ev.Event, data, ch) {
			return
		}
	}
}

func (c *Anthropic) dispatch(name, data string, ch chan<- Event) bool {
	switch name {
	case "message_start":
		var m struct {
			Message struct {
				Usage struct {
					InputTokens int `json:"input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(data), &m) == nil && m.Message.Usage.InputTokens > 0 {
			ch <- Event{Type: EventUsage, Usage: &Usage{PromptTokens: m.Message.Usage.InputTokens}}
		}
	case "content_block_start":
		var m struct {
			Index        int `json:"index"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
		}
		if json.Unmarshal([]byte(data), &m) == nil && m.ContentBlock.Type == "tool_use" {
			ch <- Event{Type: EventToolStart, Index: m.Index, ID: m.ContentBlock.ID, Name: m.ContentBlock.Name}
		}
	case "content_block_delta":
		var m struct {
			Index int `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				Thinking    string `json:"thinking"`
			} `json:"delta"`
		}
		if json.Unmarshal([]byte(data), &m) != nil {
			return true
		}
		switch m.Delta.Type {
		case "text_delta":
			ch <- Event{Type: EventText, Text: m.Delta.Text}
		case "thinking_delta":
			ch <- Event{Type: EventReasoning, Text: m.Delta.Thinking}
		case "input_json_delta":
			ch <- Event{Type: EventToolDelta, Index: m.Index, Args: m.Delta.PartialJSON}
		}
	case "message_delta":
		var m struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		json.Unmarshal([]byte(data), &m)
		if m.Usage.OutputTokens > 0 {
			ch <- Event{Type: EventUsage, Usage: &Usage{CompletionTokens: m.Usage.OutputTokens}}
		}
		if m.Delta.StopReason != "" {
			ch <- Event{Type: EventFinish, FinishReason: m.Delta.StopReason}
		}
	case "error":
		var m struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		json.Unmarshal([]byte(data), &m)
		ch <- Event{Type: EventError, Err: fmt.Errorf("anthropic: %s: %s", m.Error.Type, m.Error.Message)}
		return false
	case "message_stop":
		return false
	}
	return true
}
