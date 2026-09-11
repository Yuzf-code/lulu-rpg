// Package config 负责从环境变量加载应用配置。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Provider 表示某个能力的后端实现方式。
type Provider string

const (
	// ProviderOpenAI 表示任意 OpenAI 兼容 API（vLLM / llama.cpp server / Ollama / 硅基流动等）。
	ProviderOpenAI Provider = "openai"
	// ProviderMock 为内置演示实现，无需外部服务即可跑通全流程。
	ProviderMock Provider = "mock"
)

// Config 汇总所有可调参数，均来自环境变量。
type Config struct {
	Addr    string // HTTP 监听地址
	DataDir string // SQLite 与图片等持久化目录
	WebDir  string // 前端静态文件目录

	LLM       LLMConfig
	Image     ImageConfig
	Context   ContextConfig
	DevBanner bool // 在界面上提示当前处于演示模式
}

// LLMConfig 描述剧情写手所用的大模型接入方式。
type LLMConfig struct {
	Provider    Provider
	BaseURL     string // 例如 http://ollama:11434/v1
	APIKey      string
	Model       string
	Temperature float32
	MaxTokens   int
	Timeout     time.Duration
}

// ImageConfig 描述回合配图所用服务的接入方式；Model 为空表示关闭生图。
type ImageConfig struct {
	BaseURL     string
	APIKey      string
	Model       string
	Size        string
	StyleSuffix string // 追加到每条生图提示词后的风格词
	Timeout     time.Duration
}

// ContextConfig 控制上下文窗口与记忆压缩策略。
type ContextConfig struct {
	RecentMessages   int // 始终保留的最近消息条数
	CompactThreshold int // 未摘要消息超出该数量时触发滚动摘要
	MaxPromptTokens  int // 组装提示词的粗略 token 预算
	MaxFieldChars    int // 角色卡等字段进入提示词时的截断长度
}

// Load 从环境变量读取配置，未设置时使用内置默认值（演示模式）。
func Load() (*Config, error) {
	c := &Config{
		Addr:    env("HTTP_ADDR", ":8080"),
		DataDir: env("APP_DATA_DIR", "./data"),
		WebDir:  env("APP_WEB_DIR", "./web"),

		LLM: LLMConfig{
			Provider:    Provider(strings.ToLower(env("LLM_PROVIDER", string(ProviderMock)))),
			BaseURL:     strings.TrimRight(env("LLM_BASE_URL", ""), "/"),
			APIKey:      env("LLM_API_KEY", ""),
			Model:       env("LLM_MODEL", ""),
			Temperature: float32(envFloat("LLM_TEMPERATURE", 0.9)),
			MaxTokens:   envInt("LLM_MAX_TOKENS", 1024),
			Timeout:     time.Duration(envInt("LLM_TIMEOUT_SECONDS", 300)) * time.Second,
		},
		Image: ImageConfig{
			BaseURL:     strings.TrimRight(env("IMG_BASE_URL", ""), "/"),
			APIKey:      env("IMG_API_KEY", ""),
			Model:       env("IMG_MODEL", ""),
			Size:        env("IMG_SIZE", "1024x1024"),
			StyleSuffix: env("IMG_STYLE_SUFFIX", "幻想RPG概念艺术，数字绘画，电影感光影，丰富的细节"),
			Timeout:     time.Duration(envInt("IMG_TIMEOUT_SECONDS", 180)) * time.Second,
		},
		Context: ContextConfig{
			RecentMessages:   envInt("CONTEXT_RECENT_MESSAGES", 20),
			CompactThreshold: envInt("CONTEXT_COMPACT_THRESHOLD", 12),
			MaxPromptTokens:  envInt("CONTEXT_MAX_TOKENS", 6000),
			MaxFieldChars:    envInt("CONTEXT_MAX_FIELD_CHARS", 700),
		},
	}

	if c.LLM.Provider != ProviderMock && c.LLM.Provider != ProviderOpenAI {
		return nil, fmt.Errorf("未知 LLM_PROVIDER: %s（可选 mock / openai）", c.LLM.Provider)
	}
	if c.LLM.Provider == ProviderOpenAI && (c.LLM.BaseURL == "" || c.LLM.Model == "") {
		return nil, fmt.Errorf("LLM_PROVIDER=openai 时必须设置 LLM_BASE_URL 与 LLM_MODEL")
	}
	// 图像模型未单独配置时，回退到 LLM 的接入点（很多聚合网关两者都有）。
	if c.Image.BaseURL == "" {
		c.Image.BaseURL = c.LLM.BaseURL
	}
	if c.Image.APIKey == "" {
		c.Image.APIKey = c.LLM.APIKey
	}
	return c, nil
}

// ImageEnabled 表示回合配图功能是否可用。
func (c *Config) ImageEnabled() bool { return c.Image.Model != "" }

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return def
}
