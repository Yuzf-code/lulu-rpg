package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"lulu-rpg/internal/store"
)

// ---- 角色卡 ----

type characterPayload struct {
	Name             string                  `json:"name"`
	Title            string                  `json:"title"`
	Appearance       string                  `json:"appearance"`
	Personality      string                  `json:"personality"`
	Background       string                  `json:"background"`
	Greeting         string                  `json:"greeting"`
	ExampleDialogues []store.ExampleDialogue `json:"example_dialogues"`
	Tags             []string                `json:"tags"`
	Relationships    []store.Relationship    `json:"relationships"`
	AvatarPath       string                  `json:"avatar_path"` // /files/avatars/xxx.png
}

func (p *characterPayload) apply(c *store.Character) error {
	c.Name = strings.TrimSpace(p.Name)
	if c.Name == "" {
		return fmt.Errorf("角色名不能为空")
	}
	c.Title = p.Title
	c.Appearance = p.Appearance
	c.Personality = p.Personality
	c.Background = p.Background
	c.Greeting = p.Greeting
	exJSON, err := json.Marshal(normalizeDialogues(p.ExampleDialogues))
	if err != nil {
		return err
	}
	c.ExampleDialogues = exJSON
	tagJSON, err := json.Marshal(nonEmpty(p.Tags))
	if err != nil {
		return err
	}
	c.Tags = tagJSON
	relJSON, err := json.Marshal(normalizeRelationships(p.Relationships))
	if err != nil {
		return err
	}
	c.Relationships = relJSON
	// 只接受本服务生成的 /files/ 路径，避免外链注入。
	c.AvatarPath = safeFilePath(p.AvatarPath)
	return nil
}

func (s *Server) listCharacters(w http.ResponseWriter, _ *http.Request) {
	cs, err := s.store.ListCharacters()
	if notFoundOr(w, err, "list characters") {
		return
	}
	if cs == nil {
		cs = []*store.Character{}
	}
	writeJSON(w, http.StatusOK, cs)
}

func (s *Server) getCharacter(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetCharacter(r.PathValue("id"))
	if notFoundOr(w, err, "get character") {
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) createCharacter(w http.ResponseWriter, r *http.Request) {
	var p characterPayload
	if !readJSON(w, r, &p) {
		return
	}
	c := &store.Character{}
	if err := p.apply(c); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.CreateCharacter(c); err != nil {
		httpError(w, http.StatusInternalServerError, "保存角色卡失败")
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) updateCharacter(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetCharacter(r.PathValue("id"))
	if notFoundOr(w, err, "get character") {
		return
	}
	var p characterPayload
	if !readJSON(w, r, &p) {
		return
	}
	if err := p.apply(c); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.UpdateCharacter(c); err != nil {
		httpError(w, http.StatusInternalServerError, "保存角色卡失败")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) deleteCharacter(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteCharacter(r.PathValue("id")); err != nil {
		if notFoundOr(w, err, "delete character") {
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// generateCharacterAvatar 为已有角色卡重生成头像。
func (s *Server) generateCharacterAvatar(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetCharacter(r.PathValue("id"))
	if notFoundOr(w, err, "get character") {
		return
	}
	url, err := s.engine.GenerateAvatar(r.Context(), c.Name, c.Appearance)
	if err != nil {
		httpError(w, http.StatusBadGateway, "生成头像失败: "+err.Error())
		return
	}
	c.AvatarPath = url
	if err := s.store.UpdateCharacter(c); err != nil {
		httpError(w, http.StatusInternalServerError, "保存头像失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"avatar_path": url})
}

// ---- 角色档案 ----

type personaPayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	AvatarPath  string `json:"avatar_path"`
	IsDefault   bool   `json:"is_default"`
}

func (p *personaPayload) apply(x *store.Persona) error {
	x.Name = strings.TrimSpace(p.Name)
	if x.Name == "" {
		return fmt.Errorf("档案名不能为空")
	}
	x.Description = p.Description
	x.AvatarPath = safeFilePath(p.AvatarPath)
	x.IsDefault = p.IsDefault
	return nil
}

func (s *Server) listPersonas(w http.ResponseWriter, _ *http.Request) {
	ps, err := s.store.ListPersonas()
	if notFoundOr(w, err, "list personas") {
		return
	}
	if ps == nil {
		ps = []*store.Persona{}
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) getPersona(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPersona(r.PathValue("id"))
	if notFoundOr(w, err, "get persona") {
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) createPersona(w http.ResponseWriter, r *http.Request) {
	var p personaPayload
	if !readJSON(w, r, &p) {
		return
	}
	x := &store.Persona{}
	if err := p.apply(x); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.CreatePersona(x); err != nil {
		httpError(w, http.StatusInternalServerError, "保存档案失败")
		return
	}
	writeJSON(w, http.StatusCreated, x)
}

func (s *Server) updatePersona(w http.ResponseWriter, r *http.Request) {
	x, err := s.store.GetPersona(r.PathValue("id"))
	if notFoundOr(w, err, "get persona") {
		return
	}
	var p personaPayload
	if !readJSON(w, r, &p) {
		return
	}
	if err := p.apply(x); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.UpdatePersona(x); err != nil {
		httpError(w, http.StatusInternalServerError, "保存档案失败")
		return
	}
	writeJSON(w, http.StatusOK, x)
}

func (s *Server) deletePersona(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeletePersona(r.PathValue("id")); err != nil {
		if notFoundOr(w, err, "delete persona") {
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) setDefaultPersona(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SetDefaultPersona(r.PathValue("id")); err != nil {
		if notFoundOr(w, err, "set default persona") {
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- 图片上传与生成 ----

// uploadImage 接收前端压缩后的 data URL（data:image/png;base64,...），
// 落盘到 files/avatars/ 并返回可访问 URL。
func (s *Server) uploadImage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DataURL string `json:"data_url"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	mime, data, ok := decodeDataURL(body.DataURL)
	if !ok {
		httpError(w, http.StatusBadRequest, "data_url 不合法（支持 png/jpeg/webp，≤8MB）")
		return
	}
	url, err := saveUpload(s.cfg.DataDir, mime, data)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "保存图片失败")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"url": url})
}

// generateAvatar 为尚未保存的卡片试生成头像（编辑页预览用）。
func (s *Server) generateAvatar(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string `json:"name"`
		Appearance string `json:"appearance"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	url, err := s.engine.GenerateAvatar(r.Context(), strings.TrimSpace(body.Name), body.Appearance)
	if err != nil {
		httpError(w, http.StatusBadGateway, "生成头像失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// generateImage 按自由提示词生成一张图（试笔/场景草稿）。
func (s *Server) generateImage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prompt string `json:"prompt"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	data, err := s.engine.GenerateImage(r.Context(), body.Prompt)
	if err != nil {
		httpError(w, http.StatusBadGateway, "生成图片失败: "+err.Error())
		return
	}
	url, err := saveUpload(s.cfg.DataDir, "image/png", data)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "保存图片失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// ---- 小工具 ----

func normalizeDialogues(rows []store.ExampleDialogue) []store.ExampleDialogue {
	out := make([]store.ExampleDialogue, 0, len(rows))
	for _, r := range rows {
		r.User = strings.TrimSpace(r.User)
		r.Char = strings.TrimSpace(r.Char)
		if r.User == "" && r.Char == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}

func normalizeRelationships(rows []store.Relationship) []store.Relationship {
	out := make([]store.Relationship, 0, len(rows))
	for _, r := range rows {
		r.Subject = strings.TrimSpace(r.Subject)
		r.Text = strings.TrimSpace(r.Text)
		if r.Subject == "" || r.Text == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}

func nonEmpty(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// safeFilePath 仅放行本服务 /files/ 下的相对路径。
func safeFilePath(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "/files/") && !strings.Contains(p, "..") {
		return p
	}
	return ""
}

// decodeDataURL 解析 data URL，返回 mime 与原始字节。
func decodeDataURL(dataURL string) (string, []byte, bool) {
	const prefix = "data:"
	if !strings.HasPrefix(dataURL, prefix) {
		return "", nil, false
	}
	rest := dataURL[len(prefix):]
	comma := strings.Index(rest, ",")
	if comma < 0 {
		return "", nil, false
	}
	meta := rest[:comma]
	if !strings.HasSuffix(meta, ";base64") {
		return "", nil, false
	}
	mime := strings.TrimSuffix(strings.TrimPrefix(meta, "image/"), ";base64")
	switch strings.ToLower(mime) {
	case "png", "jpeg", "webp", "gif":
	default:
		return "", nil, false
	}
	data, err := base64.StdEncoding.DecodeString(rest[comma+1:])
	if err != nil || len(data) == 0 || len(data) > 8<<20 {
		return "", nil, false
	}
	return "image/" + strings.ToLower(mime), data, true
}
