// Package imggen 抽象回合配图服务：OpenAI 兼容 images/generations 接口，
// 以及一个本地绘制占位图的 Mock 实现。
package imggen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"lulu-rpg/internal/llm"
)

// Provider 生成一张图片并返回其编码数据（当前为 PNG）。
type Provider interface {
	Generate(ctx context.Context, prompt string) ([]byte, error)
}

// OpenAICompat 调用 OpenAI 风格的 /images/generations。
type OpenAICompat struct {
	BaseURL string
	APIKey  string
	Model   string
	Size    string
	Timeout time.Duration
	Proxy   string // 为空时直连
	Client  *http.Client
}

// NewOpenAICompat 创建客户端。
func NewOpenAICompat(baseURL, apiKey, model, size string, timeout time.Duration) *OpenAICompat {
	return &OpenAICompat{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		Size:    size,
		Timeout: timeout,
		Client:  &http.Client{Transport: llm.TransportWithProxy("")},
	}
}

// NewOpenAICompatWithProxy 创建带出站代理的客户端。
func NewOpenAICompatWithProxy(baseURL, apiKey, model, size string, timeout time.Duration, proxy string) *OpenAICompat {
	return &OpenAICompat{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		Size:    size,
		Timeout: timeout,
		Client:  &http.Client{Transport: llm.TransportWithProxy(proxy)},
	}
}

type imagesRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	N      int    `json:"n"`
	Size   string `json:"size,omitempty"`
}

type imagesResponse struct {
	Data []struct {
		B64 string `json:"b64_json"`
		URL string `json:"url"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *OpenAICompat) Generate(ctx context.Context, prompt string) ([]byte, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	body, err := json.Marshal(imagesRequest{Model: c.Model, Prompt: prompt, N: 1, Size: c.Size})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/images/generations", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接图像服务失败: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode != http.StatusOK {
		var out imagesResponse
		if json.Unmarshal(data, &out) == nil && out.Error != nil {
			return nil, fmt.Errorf("图像服务返回 %d: %s", resp.StatusCode, out.Error.Message)
		}
		return nil, fmt.Errorf("图像服务返回 %d: %s", resp.StatusCode, truncate(data, 200))
	}
	var out imagesResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("解析图像服务响应失败: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("图像服务错误: %s", out.Error.Message)
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("图像服务未返回数据")
	}
	if out.Data[0].B64 != "" {
		return base64.StdEncoding.DecodeString(out.Data[0].B64)
	}
	if out.Data[0].URL != "" {
		return c.download(ctx, out.Data[0].URL)
	}
	return nil, fmt.Errorf("图像服务响应中没有图片")
}

func (c *OpenAICompat) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载生成图片失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载生成图片失败: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}
