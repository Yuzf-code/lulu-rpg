// Package api 提供全部 HTTP 接口与静态资源服务。
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"lulu-rpg/internal/config"
	"lulu-rpg/internal/game"
	"lulu-rpg/internal/store"
)

// Server 汇聚依赖并注册路由。
type Server struct {
	store  *store.Store
	engine *game.Engine
	cfg    *config.Config

	mux    *http.ServeMux
	turnMu sync.Map // sessionID → *sync.Mutex，保证同一会话同时只有一轮在生成
}

// New 创建 Server 并注册全部路由。
func New(st *store.Store, eng *game.Engine, cfg *config.Config) *Server {
	s := &Server{store: st, engine: eng, cfg: cfg, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	m.HandleFunc("GET /api/config", s.getConfig)

	// 角色卡
	m.HandleFunc("GET /api/characters", s.listCharacters)
	m.HandleFunc("POST /api/characters", s.createCharacter)
	m.HandleFunc("GET /api/characters/{id}", s.getCharacter)
	m.HandleFunc("PUT /api/characters/{id}", s.updateCharacter)
	m.HandleFunc("DELETE /api/characters/{id}", s.deleteCharacter)
	m.HandleFunc("POST /api/characters/{id}/avatar", s.generateCharacterAvatar)
	m.HandleFunc("POST /api/characters/distill", s.distillCharacters)
	m.HandleFunc("POST /api/characters/distill-detect", s.distillDetect)
	m.HandleFunc("POST /api/characters/distill-style", s.distillStyle)
	m.HandleFunc("POST /api/characters/{id}/distill", s.distillIntoCharacter)
	m.HandleFunc("POST /api/characters/generate-draft", s.generateCardDraft)

	// 角色档案（玩家本人）
	m.HandleFunc("GET /api/personas", s.listPersonas)
	m.HandleFunc("POST /api/personas", s.createPersona)
	m.HandleFunc("GET /api/personas/{id}", s.getPersona)
	m.HandleFunc("PUT /api/personas/{id}", s.updatePersona)
	m.HandleFunc("DELETE /api/personas/{id}", s.deletePersona)
	m.HandleFunc("POST /api/personas/{id}/default", s.setDefaultPersona)

	// 写作风格
	m.HandleFunc("GET /api/styles", s.listStyles)
	m.HandleFunc("POST /api/styles", s.createStyle)
	m.HandleFunc("PUT /api/styles/{id}", s.updateStyle)
	m.HandleFunc("DELETE /api/styles/{id}", s.deleteStyle)

	// 会话（游戏实例）
	m.HandleFunc("GET /api/sessions", s.listSessions)
	m.HandleFunc("POST /api/sessions", s.createSession)
	m.HandleFunc("GET /api/sessions/{id}", s.getSession)
	m.HandleFunc("PATCH /api/sessions/{id}", s.updateSession)
	m.HandleFunc("DELETE /api/sessions/{id}", s.deleteSession)
	m.HandleFunc("GET /api/sessions/{id}/messages", s.listMessages)
	m.HandleFunc("POST /api/sessions/{id}/opening", s.postOpening) // SSE
	m.HandleFunc("POST /api/sessions/{id}/turn", s.postTurn)       // SSE
	m.HandleFunc("POST /api/sessions/{id}/inspiration", s.postInspiration)

	// 图片：上传 / 头像试生成 / 场景试生成
	m.HandleFunc("POST /api/uploads", s.uploadImage)
	m.HandleFunc("POST /api/avatars/generate", s.generateAvatar)
	m.HandleFunc("POST /api/images/generate", s.generateImage)

	// 静态资源：生成的图片/上传文件 + 前端 SPA
	m.Handle("GET /files/", cacheStatic(http.StripPrefix("/files/", http.FileServer(http.Dir(filepath.Join(s.cfg.DataDir, "files"))))))
	// 前端文件要求浏览器每次协商更新（no-cache 仍可用 ETag/Last-Modified 做 304），
	// 避免更新代码后手机残留旧版 JS。
	m.Handle("GET /", noCacheStatic(http.FileServer(http.Dir(s.cfg.WebDir))))
}

// Handler 返回根处理器。
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) getConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"llm": map[string]any{
			"provider": string(s.cfg.LLM.Provider),
			"model":    s.cfg.LLM.Model,
		},
		"image": map[string]any{
			"enabled": s.cfg.ImageEnabled(),
			"model":   s.cfg.Image.Model,
		},
	})
}

func (s *Server) sessionLock(id string) *sync.Mutex {
	muAny, _ := s.turnMu.LoadOrStore(id, &sync.Mutex{})
	return muAny.(*sync.Mutex)
}

// ---- JSON 工具 ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 12<<20) // 上传走 data URL，放宽到 12MB
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		httpError(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
		return false
	}
	return true
}

func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		// 禁止路径穿越（FileServer 本身会校验，这里只是保险）
		if strings.Contains(r.URL.Path, "..") {
			httpError(w, http.StatusBadRequest, "非法路径")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// noCacheStatic 前端资源始终协商缓存，保证更新后立即生效。
func noCacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

var errStoreNotFound = store.ErrNotFound

func notFoundOr(w http.ResponseWriter, err error, payload string) bool {
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "目标不存在或已被删除")
		return true
	}
	if err != nil {
		log.Printf("%s: %v", payload, err)
		httpError(w, http.StatusInternalServerError, "服务器内部错误")
		return true
	}
	return false
}

// ensureDir 确保数据子目录存在。
func ensureDir(paths ...string) error {
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
	}
	return nil
}
