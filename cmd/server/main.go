// lulu-rpg 服务入口：装配配置、存储、LLM/图像 Provider 与 HTTP 服务。
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"lulu-rpg/internal/api"
	"lulu-rpg/internal/config"
	"lulu-rpg/internal/game"
	"lulu-rpg/internal/imggen"
	"lulu-rpg/internal/llm"
	"lulu-rpg/internal/store"
)

func main() {
	logger := log.New(os.Stdout, "[lulu-rpg] ", log.LstdFlags|log.Lmsgprefix)

	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("配置错误: %v", err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		logger.Fatalf("创建数据目录失败: %v", err)
	}

	st, err := store.Open(filepath.Join(cfg.DataDir, "lulu.db"))
	if err != nil {
		logger.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()

	// LLM Provider：openai 兼容端点或内置 Mock。
	var lp llm.Provider
	switch cfg.LLM.Provider {
	case config.ProviderMock:
		lp = llm.NewMock()
		logger.Printf("剧情写手：内置演示模式（Mock）。接入真实模型请设置 LLM_PROVIDER=openai LLM_BASE_URL=… LLM_MODEL=…")
	default:
		client := llm.NewOpenAICompat(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model, cfg.LLM.Timeout)
		client.ReasoningEffort = cfg.LLM.ReasoningEffort
		lp = client
		logger.Printf("剧情写手：%s @ %s", cfg.LLM.Model, cfg.LLM.BaseURL)
		if cfg.LLM.ReasoningEffort != "" {
			logger.Printf("思考模型推理力度：%s", cfg.LLM.ReasoningEffort)
		}
	}
	if cfg.HTTPProxy != "" {
		logger.Printf("出站代理（LLM/图像）：%s", cfg.HTTPProxy)
	}

	// 图像 Provider：IMG_MODEL=mock 强制演示画师；留空关闭配图；其余走 OpenAI 兼容。
	var ip imggen.Provider
	if cfg.ImageEnabled() {
		if cfg.Image.Model == "mock" {
			ip = imggen.NewMock()
			logger.Printf("回合配图：内置演示画师（Mock）")
		} else {
			ip = imggen.NewOpenAICompatWithProxy(cfg.Image.BaseURL, cfg.Image.APIKey, cfg.Image.Model, cfg.Image.Size, cfg.Image.Timeout, cfg.HTTPProxy)
			logger.Printf("回合配图：%s @ %s", cfg.Image.Model, cfg.Image.BaseURL)
		}
	} else {
		logger.Printf("回合配图：未启用（设置 IMG_MODEL 开启）")
	}

	// 演示模式下首次启动播种示例数据，让应用开箱即玩。
	if cfg.LLM.Provider == config.ProviderMock {
		if err := store.SeedDemoData(st); err != nil {
			logger.Printf("播种演示数据失败: %v", err)
		}
	}

	eng := game.New(st, lp, ip, cfg)
	// 思考档位：数据库保存值 > 环境变量（默认 medium）。
	if saved, err := st.GetSetting("reasoning_effort"); err == nil && saved != "" {
		eng.SetReasoningEffort(saved)
		logger.Printf("思考档位（数据库保存）：%s", saved)
	} else {
		eng.SetReasoningEffort(cfg.LLM.ReasoningEffort)
	}
	srv := api.New(st, eng, cfg)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		// 不设 WriteTimeout：SSE 长响应需要保持连接。
		IdleTimeout: 180 * time.Second,
	}

	go func() {
		logger.Printf("服务已启动：http://localhost%s", cfg.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("HTTP 服务异常退出: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	logger.Printf("正在优雅关闭…")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}
