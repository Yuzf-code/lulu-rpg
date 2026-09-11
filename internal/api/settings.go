package api

import (
	"net/http"
	"strings"
)

// ---- 运行时设置 ----

var validReasoningEfforts = map[string]bool{"none": true, "low": true, "medium": true, "high": true}

func (s *Server) getSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"reasoning_effort": s.engine.ReasoningEffort(),
	})
}

// updateSettings 修改运行时设置；思考档位持久化到数据库，重启保留。
func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var p struct {
		ReasoningEffort string `json:"reasoning_effort"`
	}
	if !readJSON(w, r, &p) {
		return
	}
	level := strings.ToLower(strings.TrimSpace(p.ReasoningEffort))
	if !validReasoningEfforts[level] {
		httpError(w, http.StatusBadRequest, "思考档位仅支持 none / low / medium / high")
		return
	}
	s.engine.SetReasoningEffort(level)
	if err := s.store.SetSetting("reasoning_effort", level); err != nil {
		httpError(w, http.StatusInternalServerError, "保存设置失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reasoning_effort": level})
}
