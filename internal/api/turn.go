package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"lulu-rpg/internal/game"
	"lulu-rpg/internal/store"
)

// ---- SSE 回合接口 ----

// sseWriter 封装 text/event-stream 输出。
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newSSEWriter(w http.ResponseWriter) (*sseWriter, bool) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	h.Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpError(w, http.StatusInternalServerError, "当前服务器不支持流式响应")
		return nil, false
	}
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &sseWriter{w: w, flusher: flusher}, true
}

func (s *sseWriter) send(ev game.Event) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", b); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// postOpening 流式生成开场（幂等：已有消息时直接结束）。
func (s *Server) postOpening(w http.ResponseWriter, r *http.Request) {
	sess, err := s.store.GetSession(r.PathValue("id"))
	if notFoundOr(w, err, "get session") {
		return
	}
	mu := s.sessionLock(sess.ID)
	if !mu.TryLock() {
		httpError(w, http.StatusConflict, "该会话正在生成中，请稍候")
		return
	}
	defer mu.Unlock()

	sse, ok := newSSEWriter(w)
	if !ok {
		return
	}
	emit := sse.send
	if sess.TurnSeq > 0 {
		_ = emit(game.Event{Type: "done", Turn: sess.TurnSeq})
		return
	}
	if len(sess.Characters) == 0 {
		_ = emit(game.Event{Type: "error", Error: "会话没有出场角色"})
		return
	}
	if err := s.engine.RunOpening(r.Context(), sess, emit); err != nil {
		log.Printf("生成开场失败: %v", err)
	}
}

// postTurn 处理一轮玩家输入并流式返回剧情分段。
func (s *Server) postTurn(w http.ResponseWriter, r *http.Request) {
	sess, err := s.store.GetSession(r.PathValue("id"))
	if notFoundOr(w, err, "get session") {
		return
	}
	var body struct {
		Content       string `json:"content"`
		Mode          string `json:"mode"` // say | direct
		GenerateImage *bool  `json:"generate_image"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	content := trimContent(body.Content)
	if content == "" {
		httpError(w, http.StatusBadRequest, "输入不能为空")
		return
	}
	mode := body.Mode
	if mode == "" {
		mode = store.StyleSay
	}
	if mode != store.StyleSay && mode != store.StyleDirect {
		httpError(w, http.StatusBadRequest, "mode 仅支持 say / direct")
		return
	}
	if len(sess.Characters) == 0 {
		httpError(w, http.StatusBadRequest, "会话没有出场角色")
		return
	}
	mu := s.sessionLock(sess.ID)
	if !mu.TryLock() {
		httpError(w, http.StatusConflict, "上一轮还在生成中，请等它结束或点击停止")
		return
	}
	defer mu.Unlock()

	wantImage := sess.AutoImage
	if body.GenerateImage != nil {
		wantImage = *body.GenerateImage
	}

	sse, ok := newSSEWriter(w)
	if !ok {
		return
	}
	emit := sse.send
	if err := s.engine.RunTurn(r.Context(), sess, content, mode, wantImage, emit); err != nil {
		// 生成中途的模型错误已在事件流里报告过；这里只记录非预期失败。
		if !errors.Is(err, game.ErrStopGeneration) && r.Context().Err() == nil {
			log.Printf("回合生成失败: %v", err)
		}
	}
}

func trimContent(s string) string {
	// 限制单次输入长度，保护小上下文模型。
	runes := []rune(s)
	if len(runes) > 4000 {
		runes = runes[:4000]
	}
	return string(runes)
}
