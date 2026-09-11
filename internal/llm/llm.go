// Package llm 抽象剧情写手所用的大模型接入。所有 OpenAI 兼容服务
// （vLLM、llama.cpp server、Ollama、各类聚合网关）共用同一个客户端；
// 另内置一个无需外部依赖的 Mock 实现用于演示与测试。
package llm

import "context"

// Role 常量。
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message 是一次对话中的单条消息。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Mock 提示用到的辅助任务类型（Task 字段取值）。
const (
	TaskSummary     = "summary"     // 剧情滚动摘要
	TaskGreeting    = "greeting"    // 开场白草稿
	TaskDialogues   = "dialogues"   // 对话示例草稿
	TaskDetect      = "detect"      // 蒸馏前的人物识别
	TaskDistill     = "distill"     // 文本蒸馏角色卡
	TaskStyle       = "style"       // 文本蒸馏写作风格
	TaskInspiration = "inspiration" // 行动灵感建议
)

// MockHint 仅供 Mock 实现使用，真实 Provider 会忽略。
type MockHint struct {
	Opening     bool     // 是否为开场
	Task        string   // 辅助任务类型（见 Task* 常量）
	Target      string   // 蒸馏对象名
	Subject     string   // 蒸馏主体名
	Turn        int64    // 当前回合号（用于轮换示例文本）
	CharNames   []string // 本局出场角色名
	PersonaName string   // 玩家角色名
}

// Request 描述一次补全请求。
type Request struct {
	Messages    []Message
	Temperature float32
	MaxTokens   int
	// ReasoningEffort 覆盖客户端默认的思考档位（none/low/medium/high）。
	ReasoningEffort string
	Mock            MockHint
}

// Provider 是大模型接入的最小接口。
type Provider interface {
	// Stream 流式补全，逐段回调 onChunk；返回完整文本。
	Stream(ctx context.Context, req Request, onChunk func(chunk string) error) (string, error)
	// Complete 非流式补全（用于摘要等后台任务）。
	Complete(ctx context.Context, req Request) (string, error)
}
