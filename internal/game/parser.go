package game

import (
	"strings"

	"lulu-rpg/internal/store"
)

// 本包实现“剧情写手”输出协议的流式解析。
//
// 约定模型逐行输出，行首为方括号标签：
//
//	[旁白]夜色像浸了墨的绸缎，压在废弃钟楼上空。
//	[艾莉娅·动作]她贴着墙根移动，指尖搭在剑柄上。
//	[艾莉娅·内心]（他到底知不知道自己在往火坑里跳……）
//	[艾莉娅]"别出声。前面有人。"
//
// 标签决定该行的叙事视角：与具体角色无关的场面用 [旁白]；谁的言行/内心
// 就用谁的名字。解析器把 Token 流实时切成带归属的分段（Segment），
// 未打标签、但以已知角色名开头的行也会被善意纠正为该角色的台词。

// Segment 是一个归属明确的剧情分段。
type Segment struct {
	Kind        string // narration | dialogue | action | thought
	SpeakerType string // narrator | character | npc
	CharacterID string // 命中角色卡时为其 ID
	SpeakerName string // character=规范名；npc=模型起的名字
	text        strings.Builder
	idx         int
}

// Text 返回分段正文（去除首尾空白）。
func (s *Segment) Text() string { return strings.TrimSpace(s.text.String()) }

// ParserEvent 是解析过程中产生的一个增量事件。
type ParserEvent struct {
	Type        string // segment_start | delta | segment_end
	SegmentIdx  int
	Kind        string
	SpeakerType string
	CharacterID string
	SpeakerName string
	Text        string
}

// Parser 把流式文本切块为分段事件流。
type Parser struct {
	names   map[string]*store.Character // 规范化名字 → 角色卡
	bare    []string                    // 按长度降序的名字，用于 “名字：台词” 兜底
	buffer  strings.Builder
	current *Segment
	all     []*Segment
}

// NewParser 创建解析器；chars 是本局出场角色，用于把标签名解析回角色卡。
func NewParser(chars []*store.Character) *Parser {
	names := make(map[string]*store.Character, len(chars))
	keys := make([]string, 0, len(chars))
	for _, c := range chars {
		k := normName(c.Name)
		if k == "" {
			continue
		}
		names[k] = c
		keys = append(keys, k)
	}
	// 长名优先，避免 “小艾” 抢走 “小艾莉娅” 的前缀。
	sortStringsByLenDesc(keys)
	return &Parser{names: names, bare: keys}
}

// Feed 喂入一段流式文本，返回触发的事件。
func (p *Parser) Feed(chunk string) []ParserEvent {
	p.buffer.WriteString(chunk)
	var events []ParserEvent
	for {
		s := p.buffer.String()
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			break
		}
		p.buffer.Reset()
		p.buffer.WriteString(s[i+1:])
		events = append(events, p.handleLine(strings.TrimRight(s[:i], "\r"))...)
	}
	return events
}

// Finish 处理残留缓冲并关闭最后一个分段。
func (p *Parser) Finish() []ParserEvent {
	events := p.handleLine(p.buffer.String())
	p.buffer.Reset()
	return append(events, p.closeCurrent()...)
}

// Segments 返回所有非空分段（只有标签没有内容的行会被剔除）。
func (p *Parser) Segments() []*Segment {
	out := make([]*Segment, 0, len(p.all))
	for _, s := range p.all {
		if s.Text() != "" {
			out = append(out, s)
		}
	}
	return out
}

func (p *Parser) handleLine(line string) []ParserEvent {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	line = strings.ReplaceAll(line, "**", "") // 模型偶尔夹带 markdown 加粗

	if inner, rest, ok := splitTag(line); ok {
		events := p.closeCurrent()
		events = append(events, p.openSegment(inner)...)
		if rest != "" {
			events = append(events, p.appendText(rest+"\n")...)
		}
		return events
	}

	// 无标签行：若尚未开启分段，尝试 “名字：台词” 兜底，否则视为旁白延续。
	if p.current == nil {
		if name, rest, ok := p.splitBareDialogue(line); ok {
			events := p.openNamed(name)
			if strings.TrimSpace(rest) != "" {
				events = append(events, p.appendText(rest+"\n")...)
			}
			return events
		}
		return append(p.openSegment("旁白"), p.appendText(line+"\n")...)
	}
	return p.appendText(line + "\n")
}

func (p *Parser) openSegment(inner string) []ParserEvent {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return p.openSegment("旁白")
	}
	name, style := splitNameStyle(inner)
	if narratorNames(normName(name)) {
		return p.start(&Segment{Kind: store.StyleNarration, SpeakerType: store.SpeakerNarrator})
	}
	kind := styleKind(style) // 无风格词时默认台词
	if kind == store.StyleNarration {
		return p.start(&Segment{Kind: store.StyleNarration, SpeakerType: store.SpeakerNarrator})
	}
	return p.openNamed(name, kind)
}

// openNamed 按名字开启角色/NPC 分段；kind 缺省为台词。
func (p *Parser) openNamed(name string, kind ...string) []ParserEvent {
	k := store.StyleDialogue
	if len(kind) > 0 && kind[0] != "" {
		k = kind[0]
	}
	n := normName(name)
	if c, ok := p.names[n]; ok {
		return p.start(&Segment{Kind: k, SpeakerType: store.SpeakerCharacter, CharacterID: c.ID, SpeakerName: c.Name})
	}
	return p.start(&Segment{Kind: k, SpeakerType: store.SpeakerNPC, SpeakerName: strings.TrimSpace(name)})
}

func (p *Parser) start(seg *Segment) []ParserEvent {
	seg.idx = len(p.all)
	p.current = seg
	p.all = append(p.all, seg)
	return []ParserEvent{{Type: "segment_start", SegmentIdx: seg.idx, Kind: seg.Kind,
		SpeakerType: seg.SpeakerType, CharacterID: seg.CharacterID, SpeakerName: seg.SpeakerName}}
}

func (p *Parser) appendText(t string) []ParserEvent {
	if p.current == nil {
		return append(p.openSegment("旁白"), p.appendText(t)...)
	}
	p.current.text.WriteString(t)
	return []ParserEvent{{Type: "delta", SegmentIdx: p.current.idx, Text: t}}
}

func (p *Parser) closeCurrent() []ParserEvent {
	if p.current == nil || p.current.Text() == "" {
		p.current = nil
		return nil
	}
	idx := p.current.idx
	p.current = nil
	return []ParserEvent{{Type: "segment_end", SegmentIdx: idx}}
}

// splitTag 判断一行是否为 “[xxx]yyy” 标签行。
func splitTag(line string) (inner, rest string, ok bool) {
	rs := []rune(line)
	if rs[0] != '[' && rs[0] != '【' {
		return "", "", false
	}
	closing := ']'
	if rs[0] == '【' {
		closing = '】'
	}
	for i := 1; i < len(rs); i++ {
		if rs[i] == rune(closing) {
			return string(rs[1:i]), strings.TrimSpace(string(rs[i+1:])), true
		}
	}
	return "", "", false
}

// splitNameStyle 从标签内容拆出名字与风格词。取“最右侧命中已知风格词”的
// 分隔点，避免把 “艾莉娅·风语者” 这类名字拆坏。
func splitNameStyle(inner string) (name, style string) {
	for _, sep := range []string{"·", "|", "・", "•", "：", ":"} {
		if i := strings.LastIndex(inner, sep); i > 0 {
			tail := inner[i+len(sep):]
			if styleKind(tail) != "" {
				return strings.TrimSpace(inner[:i]), strings.TrimSpace(tail)
			}
		}
	}
	return strings.TrimSpace(inner), ""
}

// splitBareDialogue 识别 “角色名：台词” 形式（模型偶尔忘写括号）。
func (p *Parser) splitBareDialogue(line string) (name, rest string, ok bool) {
	for _, k := range p.bare {
		cname := p.names[k].Name
		for _, sep := range []string{"：", ":"} {
			if rest, ok := strings.CutPrefix(line, cname+sep); ok {
				return cname, rest, true
			}
		}
	}
	return "", "", false
}

func narratorNames(s string) bool {
	switch normName(s) {
	case "旁白", "narrator", "narration", "场景", "环境", "scene", "画外音", "系统":
		return true
	}
	return false
}

func styleKind(s string) string {
	switch normName(s) {
	case "", "台词", "对白", "说话", "道", "dialogue", "say", "speech":
		return store.StyleDialogue
	case "动作", "行动", "神态", "肢体", "action", "act":
		return store.StyleAction
	case "内心", "心理", "想法", "心声", "独白", "thought", "thinking", "inner":
		return store.StyleThought
	case "旁白", "环境", "场景", "narration", "scene":
		return store.StyleNarration
	}
	return ""
}

func normName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "　", "")
	return s
}

func sortStringsByLenDesc(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && len([]rune(ss[j])) > len([]rune(ss[j-1])); j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}
