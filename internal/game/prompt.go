package game

import (
	"encoding/json"
	"fmt"
	"strings"

	"lulu-rpg/internal/config"
	"lulu-rpg/internal/llm"
	"lulu-rpg/internal/store"
)

// 本文件负责把“世界设定 + 角色卡 + 玩家档案 + 滚动摘要 + 近期剧情”
// 组装成发给模型的提示词。所有长字段都会截断，保证小上下文模型可用。

// 截断说明：按 rune 截断并尽量在句末断开，避免提示词里出现半个词。
func clip(s string, max int) string {
	if max <= 0 {
		return s
	}
	rs := []rune(strings.TrimSpace(s))
	if len(rs) <= max {
		return strings.TrimSpace(s)
	}
	head := string(rs[:max])
	if i := strings.LastIndexAny(head, "。！？\n；"); i > max/2 {
		return head[:i+1]
	}
	return head + "…"
}

func estimateTokens(s string) int {
	cjk, other := 0, 0
	for _, r := range s {
		if r > 0x2E80 { // CJK 及全角区
			cjk++
		} else {
			other++
		}
	}
	return cjk + (other+3)/4
}

// OpeningDirective 是开局时注入的用户消息（不落库）。
const OpeningDirective = "【系统指令】这是故事的开场。请写出开场片段：\n" +
	"- 描绘环境与氛围，交代场景；\n" +
	"- 让出场角色自然登场（台词、动作、内心皆可）；\n" +
	"- 在结尾为玩家留出介入空间；\n" +
	"- 严格遵守输出格式（行首标签），不要替玩家角色说话或行动。"

// PrivateMemory 是一段派生私聊的「私下剧情摘要」，按参与角色回流到
// 主线提示词：只有参与该私聊的角色知情，其他角色不应表现出知情。
type PrivateMemory struct {
	Title        string
	Summary      string
	CharacterIDs []string
}

// BuildChatMessages 组装一次生成的完整消息序列。
// history 不包含本轮刚写入的玩家输入（由 userMsg 单独给出）。
func BuildChatMessages(
	scenario string,
	persona *store.Persona,
	chars []*store.Character,
	summary string,
	privates []PrivateMemory,
	history []*store.Message,
	userMsg *store.Message,
	cfg *config.Config,
) []llm.Message {
	msgs := make([]llm.Message, 0, 8)
	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: buildSystemPrompt(scenario, persona, chars, privates, cfg)})
	if strings.TrimSpace(summary) != "" {
		msgs = append(msgs, llm.Message{Role: llm.RoleSystem,
			Content: "【此前剧情摘要】\n" + clip(summary, cfg.Context.MaxFieldChars*2)})
	}

	// 在 token 预算内尽量多保留历史（新→旧累积，保留最新前缀）。
	budget := cfg.Context.MaxPromptTokens - estimateTokens(msgs[0].Content) - estimateTokens(summary)
	if budget < 400 {
		budget = 400
	}
	kept := make([]*store.Message, 0, len(history))
	used := 0
	for i := len(history) - 1; i >= 0; i-- {
		cost := estimateTokens(history[i].Content) + 16
		if used+cost > budget && len(kept) > 0 {
			break
		}
		kept = append(kept, history[i])
		used += cost
	}
	// kept 目前是新→旧，恢复为旧→新。
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	msgs = append(msgs, historyToLLM(kept, personaName(persona))...)

	if userMsg != nil {
		msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: userPromptLine(userMsg, personaName(persona))})
	} else {
		msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: OpeningDirective})
	}
	return msgs
}

func personaName(p *store.Persona) string {
	if p != nil && p.Name != "" {
		return p.Name
	}
	return "玩家"
}

// buildSystemPrompt 生成整局游戏共享的系统提示词。
func buildSystemPrompt(scenario string, persona *store.Persona, chars []*store.Character, privates []PrivateMemory, cfg *config.Config) string {
	var b strings.Builder
	b.WriteString("你是一部互动式对话RPG的“剧情写手”。你负责讲述故事、扮演所有NPC和下面列出的角色，但绝不控制玩家角色。\n\n")

	b.WriteString("## 输出格式（必须严格遵守）\n")
	b.WriteString("每一行都以标签开头，标签决定该行的叙事视角：\n")
	b.WriteString("- [旁白]：与具体角色无关的环境、场面、时间推进等描写；\n")
	b.WriteString("- [角色名]：该角色说的话（默认类别，可省略风格词）；\n")
	b.WriteString("- [角色名·动作]：该角色的外部动作与神态；\n")
	b.WriteString("- [角色名·内心]：该角色的内心活动，用（……）包裹心理独白；\n")
	b.WriteString("示例：\n")
	b.WriteString("[旁白]夜色像浸了墨的绸缎。\n[艾莉娅·动作]她按住剑柄。\n[艾莉娅·内心]（有埋伏。）\n[艾莉娅]\"别出声。\"\n")
	b.WriteString("规则：\n")
	b.WriteString("1. 只输出标签行，不要输出任何解释、标题、markdown、序号或对格式的说明；\n")
	b.WriteString("2. 描述谁在说话/行动/思考，就用谁的名字做标签；与角色无关的内容一律用[旁白]；\n")
	b.WriteString("3. 只能给“出场角色”与无关路人NPC安排台词，绝不出现在玩家角色名下的台词、动作或心理；\n")
	b.WriteString("4. 保持每个角色的人设、语气与称谓一致；剧情要接续上文与既定事实；\n")
	b.WriteString("5. 每轮通常3~8行，推进适度，结尾常以某个角色的台词或动作收束，给玩家留出反应空间；\n")
	b.WriteString("6. 角色只知道自己亲历的事：角色卡中标注的「私下经历」仅该角色知晓，其他角色不应表现出知情，除非剧情中已被公开。\n\n")

	if strings.TrimSpace(scenario) != "" {
		b.WriteString("## 世界与场景设定\n" + clip(scenario, cfg.Context.MaxFieldChars*2) + "\n\n")
	} else {
		b.WriteString("## 世界与场景设定\n未指定，可根据角色设定选择一个合适的幻想/冒险舞台。\n\n")
	}

	name := personaName(persona)
	b.WriteString("## 玩家角色（绝对不可代其发言/行动/描写心理）\n")
	b.WriteString(name + "：" + clip(personaDesc(persona), cfg.Context.MaxFieldChars) + "\n\n")

	b.WriteString("## 出场角色\n")
	for i, c := range chars {
		fmt.Fprintf(&b, "%d. %s", i+1, c.Name)
		if c.Title != "" {
			fmt.Fprintf(&b, "（%s）", clip(c.Title, 40))
		}
		b.WriteString("\n")
		if c.Appearance != "" {
			b.WriteString("   外貌：" + clip(c.Appearance, cfg.Context.MaxFieldChars) + "\n")
		}
		if c.Personality != "" {
			b.WriteString("   性格：" + clip(c.Personality, cfg.Context.MaxFieldChars) + "\n")
		}
		if c.Background != "" {
			b.WriteString("   背景：" + clip(c.Background, cfg.Context.MaxFieldChars) + "\n")
		}
		if rels := relevantRelationships(c, persona, chars); rels != "" {
			b.WriteString(rels)
		}
		if pm := privateMemoriesFor(c.ID, privates); pm != "" {
			b.WriteString(pm)
		}
		if ex := exampleDialogues(c, 3); ex != "" {
			b.WriteString("   说话示例：\n" + ex)
		}
	}
	b.WriteString("\n记住：现在开始，你就是剧情写手。只按标签格式输出剧情。\n")
	return b.String()
}

func personaDesc(p *store.Persona) string {
	if p == nil {
		return "（未提供档案）"
	}
	if strings.TrimSpace(p.Description) == "" {
		return p.Name + "（暂无更多设定）"
	}
	return p.Description
}

// relevantRelationships 渲染角色对「本局主体」（玩家档案或其他出场角色）
// 的态度/关系；与场上无关的关系不进入提示词。
func relevantRelationships(c *store.Character, persona *store.Persona, chars []*store.Character) string {
	rels := c.RelationshipList()
	if len(rels) == 0 {
		return ""
	}
	relevant := make(map[string]bool)
	if persona != nil && persona.Name != "" {
		relevant[normName(persona.Name)] = true
	}
	for _, ch := range chars {
		relevant[normName(ch.Name)] = true
	}
	var lines []string
	for _, r := range rels {
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		if relevant[normName(r.Subject)] {
			lines = append(lines, fmt.Sprintf("   与%s的关系：%s\n", r.Subject, clip(r.Text, 300)))
		}
	}
	return strings.Join(lines, "")
}

// privateMemoriesFor 渲染某角色的「私下经历」：该角色参与的私聊摘要。
// 这是单方向的信息隔离——只有参与的角色拿到这段记忆。
func privateMemoriesFor(characterID string, privates []PrivateMemory) string {
	var lines []string
	for _, pm := range privates {
		if strings.TrimSpace(pm.Summary) == "" {
			continue
		}
		for _, id := range pm.CharacterIDs {
			if id == characterID {
				title := pm.Title
				if title == "" {
					title = "私聊"
				}
				lines = append(lines, fmt.Sprintf("   - 「%s」：%s\n", title, clip(pm.Summary, 500)))
				break
			}
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "   私下经历（仅该角色知晓，其他角色并不知情）：\n" + strings.Join(lines, "")
}

func exampleDialogues(c *store.Character, max int) string {
	var rows []store.ExampleDialogue
	if len(c.ExampleDialogues) > 0 {
		_ = json.Unmarshal(c.ExampleDialogues, &rows)
	}
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	for i, r := range rows {
		if i >= max {
			break
		}
		if strings.TrimSpace(r.User) == "" && strings.TrimSpace(r.Char) == "" {
			continue
		}
		fmt.Fprintf(&b, "   - 玩家说：%s\n     %s答：%s\n", clip(r.User, 120), c.Name, clip(r.Char, 200))
	}
	return b.String()
}

// historyToLLM 把落库消息还原为对话序列。剧情分段按回合合并，
// 并还原为标签行，让模型始终看到自己熟悉的输出格式。
func historyToLLM(history []*store.Message, persona string) []llm.Message {
	var msgs []llm.Message
	var curTurn int64 = -1
	var curBuf strings.Builder
	flush := func() {
		if curBuf.Len() > 0 {
			msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: strings.TrimRight(curBuf.String(), "\n")})
			curBuf.Reset()
		}
	}
	for _, m := range history {
		switch m.Kind {
		case store.KindStory:
			if curTurn != m.Turn {
				flush()
				curTurn = m.Turn
			}
			curBuf.WriteString(taggedLine(m))
			curBuf.WriteString("\n")
		case store.KindUser:
			flush()
			curTurn = -1
			msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: userPromptLine(m, persona)})
		}
	}
	flush()
	return msgs
}

// taggedLine 把一条剧情消息还原回协议格式的行。
func taggedLine(m *store.Message) string {
	var tag string
	switch m.SpeakerType {
	case store.SpeakerNarrator:
		tag = "旁白"
	default:
		tag = m.SpeakerName
		if tag == "" {
			tag = "旁白"
		}
	}
	if m.SpeakerType != store.SpeakerNarrator {
		switch m.Style {
		case store.StyleAction:
			tag += "·动作"
		case store.StyleThought:
			tag += "·内心"
		}
	}
	return "[" + tag + "]" + m.Content
}

// userPromptLine 渲染玩家输入：台词模式直接进入剧情；导演模式明确标记为场外指令。
func userPromptLine(m *store.Message, persona string) string {
	if m.Kind != store.KindUser {
		return m.Content
	}
	if m.Style == store.StyleDirect {
		return "【导演指令】" + m.Content + "\n（以上是场外指令：按其意图推进剧情，但不要在正文中出现“导演”“指令”等字眼，也不要直接复述该指令。）"
	}
	return "【" + persona + "（玩家扮演）说】" + m.Content
}

// BuildImagePrompt 由本回合剧情生成本轮配图的提示词。
func BuildImagePrompt(scenario string, chars []*store.Character, segs []*Segment, styleSuffix string) string {
	// 取最后一段旁白作为画面主体；没有旁白则取最后一段。
	var body string
	for i := len(segs) - 1; i >= 0; i-- {
		if segs[i].Kind == store.StyleNarration {
			body = segs[i].Text()
			break
		}
	}
	if body == "" && len(segs) > 0 {
		body = segs[len(segs)-1].Text()
	}
	if strings.TrimSpace(scenario) != "" {
		body = clip(scenario, 60) + "。" + body
	}
	var b strings.Builder
	b.WriteString(clip(body, 180))
	// 本回合实际出场的角色补充外貌（最多 2 人，控制提示词长度）。
	added := 0
	seen := map[string]bool{}
	for i := len(segs) - 1; i >= 0 && added < 2; i-- {
		s := segs[i]
		if s.SpeakerType != store.SpeakerCharacter || seen[s.CharacterID] {
			continue
		}
		seen[s.CharacterID] = true
		for _, c := range chars {
			if c.ID == s.CharacterID && strings.TrimSpace(c.Appearance) != "" {
				fmt.Fprintf(&b, "，%s（%s）", c.Name, clip(c.Appearance, 80))
				added++
				break
			}
		}
	}
	if styleSuffix != "" {
		b.WriteString("，" + styleSuffix)
	}
	return b.String()
}
