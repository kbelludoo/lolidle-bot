package provider

import (
	"context"
	"encoding/json"
	"strings"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role
	Content    string
	Reasoning  string
	ToolCalls  []ToolCall
	ToolCallID string
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Request struct {
	Model       string
	System      []string
	Messages    []Message
	Tools       []Tool
	Temperature *float64
	MaxTokens   *int
	ToolChoice  string
}

type EventType int

const (
	EventText EventType = iota
	EventReasoning
	EventToolStart
	EventToolDelta
	EventUsage
	EventFinish
	EventError
)

type Event struct {
	Type         EventType
	Text         string
	Index        int
	ID           string
	Name         string
	Args         string
	FinishReason string
	Usage        *Usage
	Err          error
}

type Provider interface {
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}

type Assembler struct {
	Text      strings.Builder
	Reasoning strings.Builder
	Finish    string
	Usage     *Usage
	calls     map[int]*ToolCall
	order     []int
}

func NewAssembler() *Assembler {
	return &Assembler{calls: make(map[int]*ToolCall)}
}

func (a *Assembler) Add(ev Event) error {
	switch ev.Type {
	case EventText:
		a.Text.WriteString(ev.Text)
	case EventReasoning:
		a.Reasoning.WriteString(ev.Text)
	case EventToolStart:
		tc := a.call(ev.Index)
		if ev.ID != "" {
			tc.ID = ev.ID
		}
		if ev.Name != "" {
			tc.Name = ev.Name
		}
	case EventToolDelta:
		a.call(ev.Index).Arguments += ev.Args
	case EventUsage:
		a.Usage = ev.Usage
	case EventFinish:
		a.Finish = ev.FinishReason
	case EventError:
		return ev.Err
	}
	return nil
}

func (a *Assembler) call(idx int) *ToolCall {
	tc, ok := a.calls[idx]
	if !ok {
		tc = &ToolCall{}
		a.calls[idx] = tc
		a.order = append(a.order, idx)
	}
	return tc
}

func (a *Assembler) Message() Message {
	m := Message{
		Role:      RoleAssistant,
		Content:   a.Text.String(),
		Reasoning: a.Reasoning.String(),
	}
	for _, idx := range a.order {
		m.ToolCalls = append(m.ToolCalls, *a.calls[idx])
	}
	return m
}
