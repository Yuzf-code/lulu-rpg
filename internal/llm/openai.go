package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAICompat 适配任意 OpenAI 兼容的 chat/completions 接口。
type OpenAICompat struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
	Proxy   string // 为空时直连（忽略环境的 http_proxy，避免局域网请求被代理劫持）
	// 思考型模型的推理力度："none" 关闭思考（Ollama 实测有效），
	// 空 = 不发送该字段。非 qwen/o 系后端通常会忽略未知参数，无副作用。
	ReasoningEffort string
	Client          *http.Client
}

// NewOpenAICompat 创建客户端。
func NewOpenAICompat(baseURL, apiKey, model string, timeout time.Duration) *OpenAICompat {
	return newOpenAICompat(baseURL, apiKey, model, timeout, "")
}

// newOpenAICompat 内部构造，允许指定出站代理。
func newOpenAICompat(baseURL, apiKey, model string, timeout time.Duration, proxy string) *OpenAICompat {
	c := &OpenAICompat{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		Timeout: timeout,
		Proxy:   proxy,
		Client:  &http.Client{Transport: TransportWithProxy(proxy)}, // 超时由 ctx 控制，便于流式长连接
	}
	return c
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float32   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	// 思考型模型的推理力度（如 Ollama: "none" 关闭思考）。空则不发送。
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	Stream          bool   `json:"stream"`
}

type chatChoice struct {
	Delta   *chatDelta   `json:"delta,omitempty"`
	Message *chatMessage `json:"message,omitempty"`
}

type chatDelta struct {
	Content string `json:"content"`
}

type chatMessage struct {
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Error   *apiError    `json:"error,omitempty"`
}

type apiError struct {
	Message string `json:"message"`
}

func (c *OpenAICompat) do(ctx context.Context, req Request, stream bool, onChunk func(string) error) (string, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	body, err := json.Marshal(chatRequest{
		Model:           c.Model,
		Messages:        req.Messages,
		Temperature:     req.Temperature,
		MaxTokens:       req.MaxTokens,
		ReasoningEffort: reasoningEffortOf(req, c),
		Stream:          stream,
	})
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.Client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("连接大模型失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg := readErrorBody(resp.Body)
		return "", fmt.Errorf("大模型返回 %d: %s", resp.StatusCode, msg)
	}

	// 部分网关不支持流式，会直接返回完整 JSON；按内容类型自动降级。
	ct := resp.Header.Get("Content-Type")
	if stream && !strings.Contains(ct, "text/event-stream") {
		return c.consumeFull(resp.Body, onChunk)
	}
	if !stream {
		return c.consumeFull(resp.Body, func(chunk string) error { return onChunk(chunk) })
	}
	return c.consumeStream(ctx, resp.Body, onChunk)
}

func (c *OpenAICompat) consumeFull(r io.Reader, onChunk func(string) error) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	var out chatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("解析大模型响应失败: %w", err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("大模型错误: %s", out.Error.Message)
	}
	text := ""
	if len(out.Choices) > 0 && out.Choices[0].Message != nil {
		text = out.Choices[0].Message.Content
	}
	if text != "" && onChunk != nil {
		if err := onChunk(text); err != nil {
			return "", err
		}
	}
	return text, nil
}

// consumeStream 解析 OpenAI 风格的 SSE：每行 “data: {json}”，以 “data: [DONE]” 结束。
// ctx 被取消（用户点击停止）时视为正常结束，返回已收到的文本。
func (c *OpenAICompat) consumeStream(ctx context.Context, r io.Reader, onChunk func(string) error) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var full strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		payload, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		payload = strings.TrimSpace(payload)
		if payload == "[DONE]" {
			break
		}
		var out chatResponse
		if err := json.Unmarshal([]byte(payload), &out); err != nil {
			continue // 跳过无法解析的心跳/注释帧
		}
		if out.Error != nil {
			return full.String(), fmt.Errorf("大模型错误: %s", out.Error.Message)
		}
		if len(out.Choices) == 0 {
			continue
		}
		chunk := ""
		if out.Choices[0].Delta != nil {
			chunk = out.Choices[0].Delta.Content
		} else if out.Choices[0].Message != nil {
			chunk = out.Choices[0].Message.Content
		}
		if chunk == "" {
			continue
		}
		full.WriteString(chunk)
		if onChunk != nil {
			if err := onChunk(chunk); err != nil {
				return full.String(), err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return full.String(), nil
		}
		return full.String(), fmt.Errorf("读取大模型流失败: %w", err)
	}
	return full.String(), nil
}

func (c *OpenAICompat) Stream(ctx context.Context, req Request, onChunk func(string) error) (string, error) {
	return c.do(ctx, req, true, onChunk)
}

func (c *OpenAICompat) Complete(ctx context.Context, req Request) (string, error) {
	var full strings.Builder
	_, err := c.do(ctx, req, false, func(chunk string) error {
		full.WriteString(chunk)
		return nil
	})
	return full.String(), err
}

// reasoningEffortOf 请求级设置优先，回退到客户端默认。
func reasoningEffortOf(req Request, c *OpenAICompat) string {
	if req.ReasoningEffort != "" {
		return req.ReasoningEffort
	}
	return c.ReasoningEffort
}

func readErrorBody(r io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(r, 4*1024))
	var out struct {
		Error *apiError `json:"error"`
	}
	if json.Unmarshal(data, &out) == nil && out.Error != nil {
		return out.Error.Message
	}
	return strings.TrimSpace(string(data))
}
