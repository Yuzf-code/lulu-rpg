package game

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"lulu-rpg/internal/llm"
	"lulu-rpg/internal/store"
)

// 本文件实现角色卡辅助能力：文本蒸馏（新建/增强）、开场白与对话示例
// 草稿、行动灵感。全部基于大模型非流式补全 + 宽松 JSON 解析，
// 对 JSON 能力较弱的中小模型保持稳健。

// DistilledCard 是蒸馏/草稿产出的角色卡数据，字段名与前端表单一致。
type DistilledCard struct {
	Name             string                  `json:"name"`
	Title            string                  `json:"title"`
	Appearance       string                  `json:"appearance"`
	Personality      string                  `json:"personality"`
	Background       string                  `json:"background"`
	Greeting         string                  `json:"greeting"`
	Tags             []string                `json:"tags"`
	ExampleDialogues []store.ExampleDialogue `json:"example_dialogues"`
	Relationships    []store.Relationship    `json:"relationships"`
}

// DistillResult 是单个蒸馏对象的结果（失败时带 error）。
type DistillResult struct {
	Target    string         `json:"target"`
	Character *DistilledCard `json:"character,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// DistilledStyle 是从文本中提炼的写作风格草稿。
type DistilledStyle struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// DistillOutput 是一次完整蒸馏的产出：角色卡草稿 + 写作风格草稿。
type DistillOutput struct {
	Characters []DistillResult `json:"drafts"`
	Style      *DistilledStyle `json:"style,omitempty"`
	StyleError string          `json:"style_error,omitempty"`
}

// maxDistillText 限制蒸馏输入原文长度（rune），保护小上下文模型。
const maxDistillText = 16000

// Distill 从原文中蒸馏角色卡草稿与写作风格。
// targets 为空时先让模型识别人物；subject 指定“主体”时，额外蒸馏
// 各对象对主体的态度/关系。
func (e *Engine) Distill(ctx context.Context, text, subject string, targets []string) (*DistillOutput, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("原文不能为空")
	}
	if len([]rune(text)) > maxDistillText {
		return nil, fmt.Errorf("原文过长（上限 %d 字），请分段蒸馏", maxDistillText)
	}
	if len(targets) == 0 {
		var err error
		targets, err = e.detectTargets(ctx, text)
		if err != nil {
			return nil, err
		}
		if len(targets) == 0 {
			return nil, fmt.Errorf("未能从原文中识别出可蒸馏的人物，请手动指定对象")
		}
	}
	if len(targets) > 5 {
		targets = targets[:5]
	}
	out := &DistillOutput{}
	// 角色卡（并行）与写作风格同时进行，互不阻塞。
	done := make(chan struct{}, len(targets)+1)
	results := make([]DistillResult, len(targets))
	for i, target := range targets {
		go func(i int, target string) {
			card, err := e.distillOne(ctx, text, target, subject, nil)
			if err != nil {
				results[i] = DistillResult{Target: target, Error: err.Error()}
			} else {
				results[i] = DistillResult{Target: target, Character: card}
			}
			done <- struct{}{}
		}(i, target)
	}
	go func() {
		st, err := e.distillStyle(ctx, text)
		if err != nil {
			out.StyleError = err.Error()
		} else {
			out.Style = st
		}
		done <- struct{}{}
	}()
	for i := 0; i < len(targets)+1; i++ {
		<-done
	}
	out.Characters = results
	return out, nil
}

// DistillInto 蒸馏并融合进已有角色卡：模型在原卡设定基础上结合原文
// 补全/丰富设定。返回融合后的草稿，由前端确认后再保存。
func (e *Engine) DistillInto(ctx context.Context, card *store.Character, text, subject string) (*DistilledCard, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("原文不能为空")
	}
	merged, err := e.distillOne(ctx, text, card.Name, subject, card)
	if err != nil {
		return nil, err
	}
	return mergeCard(card, merged), nil
}

// distillOne 执行单对象蒸馏。base 为已有卡时提示模型做“增强合并”。
func (e *Engine) distillOne(ctx context.Context, text, target, subject string, base *store.Character) (*DistilledCard, error) {
	sys := "你是角色卡提取器。从给定文本中提取指定人物的设定，输出严格的 JSON 对象：\n" +
		"`{\"name\":\"\",\"title\":\"\",\"appearance\":\"\",\"personality\":\"\",\"background\":\"\",\"greeting\":\"\",\"tags\":[],\"example_dialogues\":[{\"user\":\"\",\"char\":\"\"}],\"relationships\":[{\"subject\":\"\",\"text\":\"\"}]}`\n" +
		"要求：\n" +
		"- 只依据文本内容提炼，不要编造文本中没有的设定；信息不足的字段用空字符串（数组用空数组）；\n" +
		"- appearance 汇总外貌与标志性穿着；personality 汇总性格、说话方式与在意之事；\n" +
		"- background 按时间线概括关键经历，保留伏笔、秘密与未解之事；\n" +
		"- greeting 以该角色身份写一段符合文本情境的开场白（150字内，可直接用于游戏开场）；\n" +
		"- example_dialogues 给 2~3 组能体现其语气的示例问答；tags 给 2~4 个短标签；\n" +
		"- relationships：若指定了【主体】，写该角色对主体的态度/关系/评价及关键事件；未指定则为空数组。\n" +
		"只输出 JSON，不要 markdown 代码块、不要任何解释。"

	var user strings.Builder
	if subject != "" {
		fmt.Fprintf(&user, "【主体】%s\n", subject)
	}
	fmt.Fprintf(&user, "【蒸馏对象】%s\n", target)
	if base != nil {
		fmt.Fprintf(&user, "\n【该角色已有设定（在其基础上结合原文补全与丰富，已有内容不要无故删改）】\n")
		fmt.Fprintf(&user, "头衔：%s\n外貌：%s\n性格：%s\n背景：%s\n", base.Title, base.Appearance, base.Personality, base.Background)
		if rels := base.RelationshipList(); len(rels) > 0 {
			fmt.Fprintf(&user, "已有关系：")
			for _, r := range rels {
				fmt.Fprintf(&user, "（对%s：%s）", r.Subject, r.Text)
			}
			user.WriteString("\n")
		}
	}
	fmt.Fprintf(&user, "\n【原文】\n%s", clip(text, maxDistillText))

	out, err := e.llm.Complete(ctx, llm.Request{
		Messages:    []llm.Message{{Role: llm.RoleSystem, Content: sys}, {Role: llm.RoleUser, Content: user.String()}},
		Temperature: 0.5,
		MaxTokens:   1200,
		Mock:        llm.MockHint{Task: llm.TaskDistill, Target: target, Subject: subject},
	})
	if err != nil {
		return nil, err
	}
	var card DistilledCard
	if err := extractJSON(out, &card); err != nil {
		return nil, fmt.Errorf("蒸馏结果解析失败: %w", err)
	}
	if strings.TrimSpace(card.Name) == "" {
		card.Name = target
	}
	if subject != "" {
		// 保证主体关系确实挂在主体名下。
		found := false
		for i := range card.Relationships {
			if normName(card.Relationships[i].Subject) == normName(subject) {
				card.Relationships[i].Subject = subject
				found = true
			}
		}
		if !found && len(card.Relationships) == 0 {
			card.Relationships = []store.Relationship{{Subject: subject, Text: "（模型未给出，可留空待补充）"}}
		}
	}
	return &card, nil
}

// distillStyle 分析文本的写作风格，产出供剧情写手使用的风格指令。
// 注意：写手是“逐行带视角标签”的输出格式（[旁白]/[角色·台词/动作/内心]），
// 因此风格指令必须分视角描述各自的笔触，而不是泛泛的人称/排版建议。
func (e *Engine) distillStyle(ctx context.Context, text string) (*DistilledStyle, error) {
	sys := "你要为一部互动式对话RPG的「剧情写手」提炼写作风格。" +
		"该写手逐行输出，每行带视角标签：[旁白]（环境与场面描写）、[角色名]（台词）、[角色名·动作]（外部动作与神态）、[角色名·内心]（心理独白）。" +
		"请分析给定文本的写作风格，输出严格 JSON 对象：" +
		"`{\"name\":\"简短风格名\",\"description\":\"写给该写手的风格指令\"}`。" +
		"description（150字内）分视角说明：[旁白]如何写环境与氛围（意象、感官细节、句式节奏）；" +
		"台词什么语感（长短、潜台词、口吻差异）；动作与内心独白用什么笔触（克制还是浓烈、具体还是留白）；" +
		"以及整体基调。不要指定人称与排版格式（由系统固定），不要复述剧情内容。只输出 JSON，不要代码块、不要解释。"
	out, err := e.llm.Complete(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: sys},
			{Role: llm.RoleUser, Content: clip(text, maxDistillText)},
		},
		Temperature: 0.4,
		MaxTokens:   300,
		Mock:        llm.MockHint{Task: llm.TaskStyle},
	})
	if err != nil {
		return nil, err
	}
	var st DistilledStyle
	if err := extractJSON(out, &st); err != nil {
		return nil, fmt.Errorf("风格解析失败: %w", err)
	}
	if strings.TrimSpace(st.Name) == "" {
		return nil, fmt.Errorf("未生成有效的风格名")
	}
	return &st, nil
}

// mergeCard 把蒸馏结果融合进已有卡（非空字段覆盖、列表合并去重）。
func mergeCard(base *store.Character, d *DistilledCard) *DistilledCard {
	out := &DistilledCard{
		Name:             base.Name,
		Title:            base.Title,
		Appearance:       base.Appearance,
		Personality:      base.Personality,
		Background:       base.Background,
		Greeting:         base.Greeting,
		Tags:             parseTagsRaw(base.Tags),
		ExampleDialogues: parseDialoguesRaw(base.ExampleDialogues),
		Relationships:    base.RelationshipList(),
	}
	if d.Title != "" {
		out.Title = d.Title
	}
	if d.Appearance != "" {
		out.Appearance = d.Appearance
	}
	if d.Personality != "" {
		out.Personality = d.Personality
	}
	if d.Background != "" {
		out.Background = d.Background
	}
	if d.Greeting != "" {
		out.Greeting = d.Greeting
	}
	out.Tags = mergeUnique(out.Tags, d.Tags, 10)
	out.ExampleDialogues = append(out.ExampleDialogues, d.ExampleDialogues...)
	if len(out.ExampleDialogues) > 8 {
		out.ExampleDialogues = out.ExampleDialogues[:8]
	}
	// 关系按主体去重：同主体以新蒸馏结果为准。
	bySubject := make(map[string]store.Relationship, len(out.Relationships)+len(d.Relationships))
	order := make([]string, 0, len(out.Relationships)+len(d.Relationships))
	for _, r := range out.Relationships {
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		k := normName(r.Subject)
		if _, ok := bySubject[k]; !ok {
			order = append(order, k)
		}
		bySubject[k] = r
	}
	for _, r := range d.Relationships {
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		k := normName(r.Subject)
		if _, ok := bySubject[k]; !ok {
			order = append(order, k)
		}
		bySubject[k] = r
	}
	out.Relationships = out.Relationships[:0]
	for _, k := range order {
		out.Relationships = append(out.Relationships, bySubject[k])
	}
	return out
}

// detectTargets 让模型从原文中识别人物名。
func (e *Engine) detectTargets(ctx context.Context, text string) ([]string, error) {
	out, err := e.llm.Complete(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "列出文本中设定信息较丰富的 1~5 个人物名字。只输出 JSON 字符串数组，如 [\"张三\",\"李四\"]，不要解释。"},
			{Role: llm.RoleUser, Content: clip(text, maxDistillText)},
		},
		Temperature: 0.2,
		MaxTokens:   100,
		Mock:        llm.MockHint{Task: llm.TaskDetect},
	})
	if err != nil {
		return nil, err
	}
	var names []string
	if err := extractJSON(out, &names); err != nil {
		return nil, fmt.Errorf("人物识别失败: %w", err)
	}
	clean := names[:0]
	for _, n := range names {
		if t := strings.TrimSpace(n); t != "" {
			clean = append(clean, t)
		}
	}
	return clean, nil
}

// ---- 开场白 / 对话示例草稿 ----

// CardSeed 是“尚未保存的角色卡”的最小信息，用于生成草稿。
type CardSeed struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Appearance  string `json:"appearance"`
	Personality string `json:"personality"`
	Background  string `json:"background"`
	Scenario    string `json:"scenario"` // 可选：期望的故事舞台
}

// DraftGreeting 生成开场白草稿。
func (e *Engine) DraftGreeting(ctx context.Context, seed CardSeed) (string, error) {
	if strings.TrimSpace(seed.Name) == "" {
		return "", fmt.Errorf("请先填写角色名称")
	}
	sys := "你是互动式RPG的角色卡写手。为指定角色写一段开场白：以角色身份登场，可包含简短动作/神态描写（括号内）与台词，" +
		"展示性格与说话方式，结尾给玩家留出接话空间。150字以内，直接输出正文，不要解释。" +
		"如果给定了世界设定，开场白应贴合该设定。"
	out, err := e.llm.Complete(ctx, llm.Request{
		Messages:    []llm.Message{{Role: llm.RoleSystem, Content: sys}, {Role: llm.RoleUser, Content: cardSeedPrompt(seed)}},
		Temperature: 0.8,
		MaxTokens:   400,
		Mock:        llm.MockHint{Task: llm.TaskGreeting, Target: seed.Name},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// DraftDialogues 生成对话示例草稿。
func (e *Engine) DraftDialogues(ctx context.Context, seed CardSeed) ([]store.ExampleDialogue, error) {
	if strings.TrimSpace(seed.Name) == "" {
		return nil, fmt.Errorf("请先填写角色名称")
	}
	sys := "你是互动式RPG的角色卡写手。为指定角色生成 3 组对话示例（体现其性格与说话方式）：\n" +
		"输出严格 JSON 数组：[{\"user\":\"玩家说的话\",\"char\":\"角色的回答\"}]，不要解释、不要代码块。"
	out, err := e.llm.Complete(ctx, llm.Request{
		Messages:    []llm.Message{{Role: llm.RoleSystem, Content: sys}, {Role: llm.RoleUser, Content: cardSeedPrompt(seed)}},
		Temperature: 0.8,
		MaxTokens:   600,
		Mock:        llm.MockHint{Task: llm.TaskDialogues, Target: seed.Name},
	})
	if err != nil {
		return nil, err
	}
	var rows []store.ExampleDialogue
	if err := extractJSON(out, &rows); err != nil {
		return nil, fmt.Errorf("对话示例解析失败: %w", err)
	}
	clean := rows[:0]
	for _, r := range rows {
		if strings.TrimSpace(r.User) != "" || strings.TrimSpace(r.Char) != "" {
			clean = append(clean, r)
		}
	}
	if len(clean) == 0 {
		return nil, fmt.Errorf("未生成有效的对话示例")
	}
	return clean, nil
}

func cardSeedPrompt(s CardSeed) string {
	var b strings.Builder
	fmt.Fprintf(&b, "角色名：%s\n", s.Name)
	if s.Title != "" {
		fmt.Fprintf(&b, "头衔：%s\n", s.Title)
	}
	if s.Appearance != "" {
		fmt.Fprintf(&b, "外貌：%s\n", clip(s.Appearance, 400))
	}
	if s.Personality != "" {
		fmt.Fprintf(&b, "性格：%s\n", clip(s.Personality, 400))
	}
	if s.Background != "" {
		fmt.Fprintf(&b, "背景：%s\n", clip(s.Background, 500))
	}
	if s.Scenario != "" {
		fmt.Fprintf(&b, "世界设定：%s\n", clip(s.Scenario, 300))
	}
	return b.String()
}

// ---- 行动灵感 ----

// InspirationOption 是一条下一步行动建议。
type InspirationOption struct {
	Kind string `json:"kind"` // say | direct
	Text string `json:"text"`
}

// Inspiration 根据当前场景与近期剧情生成行动灵感（台词与导演指令混合）。
func (e *Engine) Inspiration(ctx context.Context, sess *store.Session) ([]InspirationOption, error) {
	history, err := e.store.ListMessages(sess.ID, 0, 12)
	if err != nil {
		return nil, err
	}
	var user strings.Builder
	persona := sess.Persona
	fmt.Fprintf(&user, "【玩家角色】%s：%s\n", personaName(persona), clip(personaDesc(persona), 200))
	fmt.Fprintf(&user, "【出场角色】%s\n", strings.Join(charNames(sess.Characters), "、"))
	if sess.Scenario != "" {
		fmt.Fprintf(&user, "【场景】%s\n", clip(sess.Scenario, 200))
	}
	if sess.Summary != "" {
		fmt.Fprintf(&user, "【剧情摘要】%s\n", clip(sess.Summary, 400))
	}
	user.WriteString("【近期剧情】\n")
	for _, m := range history {
		switch m.Kind {
		case store.KindStory:
			user.WriteString(taggedLine(m) + "\n")
		case store.KindUser:
			if m.Style == store.StyleSay {
				fmt.Fprintf(&user, "【%s】%s\n", personaName(persona), clip(m.Content, 120))
			}
		}
	}
	sys := "你是互动式RPG的行动灵感助手。根据当前场景与最新剧情，为玩家生成 4~6 条下一步行动建议：\n" +
		"- 约一半为 {\"kind\":\"say\",\"text\":\"…\"}：以【玩家角色】第一人称台词，符合其身份与当下处境，简短有力；\n" +
		"- 其余为 {\"kind\":\"direct\",\"text\":\"…\"}：第三人称剧情走向指令，描述玩家希望发生的事件或转折；\n" +
		"- 建议必须紧扣最新剧情，方向多样（试探、推进、调查、撤退、制造冲突……）；\n" +
		"只输出 JSON 数组，不要解释、不要代码块。"
	out, err := e.llm.Complete(ctx, llm.Request{
		Messages:    []llm.Message{{Role: llm.RoleSystem, Content: sys}, {Role: llm.RoleUser, Content: user.String()}},
		Temperature: 0.9,
		MaxTokens:   600,
		Mock:        llm.MockHint{Task: llm.TaskInspiration, PersonaName: personaName(persona), CharNames: charNames(sess.Characters)},
	})
	if err != nil {
		return nil, err
	}
	var rows []InspirationOption
	if err := extractJSON(out, &rows); err != nil {
		return nil, fmt.Errorf("灵感解析失败: %w", err)
	}
	clean := rows[:0]
	for _, r := range rows {
		t := strings.TrimSpace(r.Text)
		if t == "" {
			continue
		}
		kind := r.Kind
		if kind != store.StyleSay && kind != store.StyleDirect {
			kind = store.StyleDirect
		}
		clean = append(clean, InspirationOption{Kind: kind, Text: t})
	}
	if len(clean) == 0 {
		return nil, fmt.Errorf("未生成有效灵感")
	}
	if len(clean) > 6 {
		clean = clean[:6]
	}
	return clean, nil
}

// ---- 宽松 JSON 提取 ----

// extractJSON 从模型输出中尽力提取 JSON（容忍 markdown 代码块、前后缀噪声）。
func extractJSON(s string, target any) error {
	s = strings.TrimSpace(s)
	// 去掉 markdown 围栏。
	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		rest = strings.TrimPrefix(rest, "json")
		if j := strings.LastIndex(rest, "```"); j >= 0 {
			s = strings.TrimSpace(rest[:j])
		} else {
			s = strings.TrimSpace(rest)
		}
	}
	// 定位首个 { 或 [ 与其配对的最后一个 } 或 ]。
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return fmt.Errorf("输出中没有 JSON")
	}
	var open, close byte = s[start], '}'
	if open == '[' {
		close = ']'
	}
	end := strings.LastIndexByte(s, close)
	if end < start {
		return fmt.Errorf("JSON 不完整")
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), target); err != nil {
		return err
	}
	return nil
}

// ---- 小工具 ----

func parseTagsRaw(raw json.RawMessage) []string {
	var out []string
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func parseDialoguesRaw(raw json.RawMessage) []store.ExampleDialogue {
	var out []store.ExampleDialogue
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func mergeUnique(base, extra []string, cap int) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, s := range append(append([]string{}, base...), extra...) {
		t := strings.TrimSpace(s)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > cap {
		out = out[:cap]
	}
	return out
}
