package api

import (
	"net/http"
	"strings"

	"lulu-rpg/internal/game"
)

// ---- 角色卡蒸馏 / 草稿 / 灵感 ----

type distillPayload struct {
	Text    string   `json:"text"`    // 原文
	Subject string   `json:"subject"` // 可选主体：蒸馏对象对其的态度/关系
	Targets []string `json:"targets"` // 蒸馏对象名；留空自动识别
}

// distillCharacters 从原文蒸馏角色卡草稿（不直接落库，由前端确认后保存）。
func (s *Server) distillCharacters(w http.ResponseWriter, r *http.Request) {
	var p distillPayload
	if !readJSON(w, r, &p) {
		return
	}
	if strings.TrimSpace(p.Text) == "" {
		httpError(w, http.StatusBadRequest, "请提供要蒸馏的原文")
		return
	}
	out, err := s.engine.Distill(r.Context(), p.Text, p.Subject, p.Targets)
	if err != nil {
		httpError(w, userOrUpstream(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// distillIntoCharacter 蒸馏并增强已有角色卡，返回融合后的草稿。
func (s *Server) distillIntoCharacter(w http.ResponseWriter, r *http.Request) {
	card, err := s.store.GetCharacter(r.PathValue("id"))
	if notFoundOr(w, err, "get character") {
		return
	}
	var p distillPayload
	if !readJSON(w, r, &p) {
		return
	}
	if strings.TrimSpace(p.Text) == "" {
		httpError(w, http.StatusBadRequest, "请提供要蒸馏的原文")
		return
	}
	merged, err := s.engine.DistillInto(r.Context(), card, p.Text, p.Subject)
	if err != nil {
		httpError(w, userOrUpstream(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"character": merged})
}

// generateCardDraft 为（尚未保存的）角色卡生成开场白或对话示例草稿。
func (s *Server) generateCardDraft(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Kind string `json:"kind"` // greeting | dialogues
		game.CardSeed
	}
	if !readJSON(w, r, &p) {
		return
	}
	switch p.Kind {
	case "greeting":
		greeting, err := s.engine.DraftGreeting(r.Context(), p.CardSeed)
		if err != nil {
			httpError(w, userOrUpstream(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"greeting": greeting})
	case "dialogues":
		rows, err := s.engine.DraftDialogues(r.Context(), p.CardSeed)
		if err != nil {
			httpError(w, userOrUpstream(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"example_dialogues": rows})
	default:
		httpError(w, http.StatusBadRequest, "kind 仅支持 greeting / dialogues")
	}
}

// postInspiration 根据当前剧情生成下一步行动灵感。
func (s *Server) postInspiration(w http.ResponseWriter, r *http.Request) {
	sess, err := s.store.GetSession(r.PathValue("id"))
	if notFoundOr(w, err, "get session") {
		return
	}
	if len(sess.Characters) == 0 {
		httpError(w, http.StatusBadRequest, "会话没有出场角色")
		return
	}
	opts, err := s.engine.Inspiration(r.Context(), sess)
	if err != nil {
		httpError(w, userOrUpstream(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"options": opts})
}

// userOrUpstream 区分参数类错误（400）与大模型侧错误（502）。
func userOrUpstream(err error) int {
	msg := err.Error()
	for _, marker := range []string{"不能为空", "过长", "请", "解析失败", "未生成", "识别失败"} {
		if strings.Contains(msg, marker) {
			return http.StatusBadRequest
		}
	}
	return http.StatusBadGateway
}
