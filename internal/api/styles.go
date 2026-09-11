package api

import (
	"fmt"
	"net/http"
	"strings"

	"lulu-rpg/internal/store"
)

// ---- 写作风格 ----

type stylePayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (p *stylePayload) apply(st *store.WritingStyle) error {
	st.Name = strings.TrimSpace(p.Name)
	if st.Name == "" {
		return fmt.Errorf("风格名不能为空")
	}
	st.Description = strings.TrimSpace(p.Description)
	return nil
}

func (s *Server) listStyles(w http.ResponseWriter, _ *http.Request) {
	items, err := s.store.ListStyles()
	if notFoundOr(w, err, "list styles") {
		return
	}
	if items == nil {
		items = []*store.WritingStyle{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createStyle(w http.ResponseWriter, r *http.Request) {
	var p stylePayload
	if !readJSON(w, r, &p) {
		return
	}
	st := &store.WritingStyle{}
	if err := p.apply(st); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.CreateStyle(st); err != nil {
		httpError(w, http.StatusInternalServerError, "保存风格失败")
		return
	}
	writeJSON(w, http.StatusCreated, st)
}

func (s *Server) updateStyle(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetStyle(r.PathValue("id"))
	if notFoundOr(w, err, "get style") {
		return
	}
	var p stylePayload
	if !readJSON(w, r, &p) {
		return
	}
	if err := p.apply(st); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.UpdateStyle(st); err != nil {
		httpError(w, http.StatusInternalServerError, "保存风格失败")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) deleteStyle(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteStyle(r.PathValue("id")); err != nil {
		if notFoundOr(w, err, "delete style") {
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
