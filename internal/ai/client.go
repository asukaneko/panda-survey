package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client OpenAI 兼容 Chat Completions 客户端。
type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

func NewClient(cfg Config) *Client {
	// 不设 http.Client 总超时：由每次调用的 ctx 控制时长，
	// 连接测试用短超时，生成整卷等长任务由 handler 放宽
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		http:    &http.Client{},
	}
}

// ---- 协议结构 ----

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Function FunctionCall  `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolDef struct {
	Type     string       `json:"type"`
	Function FunctionDef  `json:"function"`
}

type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream"` // 显式声明非流式，部分网关默认开启流式
}

type chatChoice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ErrToolsUnsupported 端点不支持 function calling，调用方可降级重试。
var ErrToolsUnsupported = errors.New("上游不支持 function calling")

func (c *Client) Chat(ctx context.Context, req chatRequest) (*chatResponse, error) {
	req.Model = c.model
	if req.Temperature == 0 {
		req.Temperature = 0.3
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("AI 服务请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusBadRequest && len(req.Tools) > 0 {
			low := strings.ToLower(string(raw))
			if strings.Contains(low, "tool") || strings.Contains(low, "function") {
				return nil, ErrToolsUnsupported
			}
		}
		msg := string(raw)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("AI 服务返回 %d: %s", resp.StatusCode, msg)
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		// 部分网关忽略 stream=false，按 SSE 流式返回；聚合 chunks 兜底
		if agg, ok := parseSSEChunks(raw); ok {
			return &agg, nil
		}
		preview := string(raw)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return nil, fmt.Errorf("AI 服务响应格式错误（Content-Type: %s）: %v；响应开头: %s",
			resp.Header.Get("Content-Type"), err, preview)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("AI 服务错误: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("AI 服务返回空结果")
	}
	return &out, nil
}

// chunkDelta 流式增量块的 delta 结构。
type chunkDelta struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls []struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

type chunkResp struct {
	Choices []struct {
		Delta        chunkDelta `json:"delta"`
		FinishReason string     `json:"finish_reason"`
	} `json:"choices"`
}

// parseSSEChunks 解析 SSE 流式响应并聚合为一条消息；非 SSE 内容返回 false。
func parseSSEChunks(raw []byte) (chatResponse, bool) {
	var out chatResponse
	msg := Message{Role: "assistant"}
	finish := ""
	found := false
	toolByIndex := map[int]*ToolCall{}
	var toolOrder []*ToolCall
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(payload) == 0 || string(payload) == "[DONE]" {
			continue
		}
		// 单块即完整响应（部分网关整个响应包成一个 data 行）
		var full chatResponse
		if json.Unmarshal(payload, &full) == nil && len(full.Choices) > 0 &&
			(full.Choices[0].Message.Content != "" || len(full.Choices[0].Message.ToolCalls) > 0) {
			return full, true
		}
		var ck chunkResp
		if err := json.Unmarshal(payload, &ck); err != nil || len(ck.Choices) == 0 {
			continue
		}
		found = true
		d := ck.Choices[0].Delta
		if d.Role != "" {
			msg.Role = d.Role
		}
		msg.Content += d.Content
		for _, tc := range d.ToolCalls {
			existing, ok := toolByIndex[tc.Index]
			if !ok {
				existing = &ToolCall{ID: tc.ID, Type: "function"}
				toolByIndex[tc.Index] = existing
				toolOrder = append(toolOrder, existing)
			}
			if tc.ID != "" {
				existing.ID = tc.ID
			}
			if tc.Function.Name != "" {
				existing.Function.Name += tc.Function.Name
			}
			existing.Function.Arguments += tc.Function.Arguments
		}
		if ck.Choices[0].FinishReason != "" {
			finish = ck.Choices[0].FinishReason
		}
	}
	if !found {
		return out, false
	}
	if len(toolOrder) > 0 {
		msg.ToolCalls = make([]ToolCall, 0, len(toolOrder))
		for _, tc := range toolOrder {
			msg.ToolCalls = append(msg.ToolCalls, *tc)
		}
	}
	out.Choices = []chatChoice{{Message: msg, FinishReason: finish}}
	return out, true
}

// Ping 连接测试：短超时快速反馈。
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := c.Chat(ctx, chatRequest{
		Messages:  []Message{{Role: "user", Content: "ping"}},
		MaxTokens: 8,
	})
	return err
}
