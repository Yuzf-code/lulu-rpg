package api

import (
	"net/http"
	"strconv"
	"strings"

	"lulu-rpg/internal/store"
)

// ---- 会话（游戏实例） ----

type createSessionPayload struct {
	Title        string   `json:"title"`
	Scenario     string   `json:"scenario"`
	PersonaID    string   `json:"persona_id"`
	CharacterIDs []string `json:"character_ids"`
	AutoImage    bool     `json:"auto_image"`
	ParentID     string   `json:"parent_id"` // 派生私聊时指向主线会话
}

func (s *Server) listSessions(w http.ResponseWriter, _ *http.Request) {
	items, err := s.store.ListSessions()
	if notFoundOr(w, err, "list sessions") {
		return
	}
	if items == nil {
		items = []*store.SessionSummary{}
	}
	writeJSON(w, http.StatusOK, items)
}

// createSession 创建新的游戏实例；携带 parent_id 时为“派生私聊”，
// 自动继承主线的场景设定与剧情摘要（记忆）。
func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var p createSessionPayload
	if !readJSON(w, r, &p) {
		return
	}
	ids := dedupeIDs(p.CharacterIDs)
	if len(ids) == 0 {
		httpError(w, http.StatusBadRequest, "请至少选择一个角色")
		return
	}
	chars, err := s.store.CharactersByIDs(ids)
	if notFoundOr(w, err, "load characters") {
		return
	}
	if len(chars) != len(ids) {
		httpError(w, http.StatusBadRequest, "部分角色不存在，请刷新后重试")
		return
	}

	sess := &store.Session{
		Title:     strings.TrimSpace(p.Title),
		Scenario:  p.Scenario,
		PersonaID: p.PersonaID,
		ParentID:  p.ParentID,
		AutoImage: p.AutoImage,
	}
	// 派生私聊：继承主线设定；主线摘要存为 inherited_summary 快照
	// （此后主线继续推进时，私聊会动态读取主线最新摘要）。
	if sess.ParentID != "" {
		parent, err := s.store.GetSession(sess.ParentID)
		if notFoundOr(w, err, "load parent session") {
			return
		}
		if sess.Scenario == "" {
			sess.Scenario = parent.Scenario
		}
		if sess.PersonaID == "" {
			sess.PersonaID = parent.PersonaID
		}
		sess.InheritedSummary = strings.TrimSpace(strings.Join(nonEmptyStrings(parent.Summary, parent.InheritedSummary), "\n\n"))
		if sess.Title == "" {
			sess.Title = "私聊 · " + joinNames(chars, 3)
		}
	}
	if sess.Title == "" {
		sess.Title = joinNames(chars, 3) + "的故事"
	}
	if err := s.store.CreateSession(sess, ids); err != nil {
		httpError(w, http.StatusInternalServerError, "创建会话失败")
		return
	}
	full, err := s.store.GetSession(sess.ID)
	if notFoundOr(w, err, "load session") {
		return
	}
	writeJSON(w, http.StatusCreated, full)
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.store.GetSession(r.PathValue("id"))
	if notFoundOr(w, err, "get session") {
		return
	}
	// 附带派生私聊列表（前端展示「N 段私聊记忆」提示）。
	children, _ := s.store.ListChildren(sess.ID)
	chats := make([]map[string]any, 0, len(children))
	for _, c := range children {
		names := make([]string, 0, len(c.Characters))
		for _, ch := range c.Characters {
			names = append(names, ch.Name)
		}
		chats = append(chats, map[string]any{
			"id": c.ID, "title": c.Title, "characters": names,
			"has_memory": strings.TrimSpace(c.Summary) != "",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session":       sess,
		"private_chats": chats,
	})
}

func (s *Server) updateSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.store.GetSession(r.PathValue("id"))
	if notFoundOr(w, err, "get session") {
		return
	}
	var p struct {
		Title     *string `json:"title"`
		Scenario  *string `json:"scenario"`
		AutoImage *bool   `json:"auto_image"`
	}
	if !readJSON(w, r, &p) {
		return
	}
	if p.Title != nil && strings.TrimSpace(*p.Title) != "" {
		sess.Title = strings.TrimSpace(*p.Title)
	}
	if p.Scenario != nil {
		sess.Scenario = *p.Scenario
	}
	if p.AutoImage != nil {
		sess.AutoImage = *p.AutoImage
	}
	if err := s.store.UpdateSession(sess); err != nil {
		httpError(w, http.StatusInternalServerError, "保存会话失败")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSession(r.PathValue("id")); err != nil {
		if notFoundOr(w, err, "delete session") {
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.GetSession(id); notFoundOr(w, err, "get session") {
		return
	}
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before_seq"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	msgs, err := s.store.ListMessages(id, before, limit)
	if notFoundOr(w, err, "list messages") {
		return
	}
	if msgs == nil {
		msgs = []*store.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

// ---- 小工具 ----

func nonEmptyStrings(ss ...string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func dedupeIDs(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		t := strings.TrimSpace(s)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

func joinNames(chars []*store.Character, max int) string {
	names := make([]string, 0, max)
	for i, c := range chars {
		if i >= max {
			names = append(names, "…")
			break
		}
		names = append(names, c.Name)
	}
	return strings.Join(names, "、")
}
