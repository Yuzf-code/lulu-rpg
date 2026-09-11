package game

import (
	"strings"
	"testing"

	"lulu-rpg/internal/store"
)

func mkChars() []*store.Character {
	return []*store.Character{
		{ID: "c_a", Name: "艾莉娅", Appearance: "银灰短发，深绿斗篷"},
		{ID: "c_b", Name: "老巴德", Appearance: "圆胖，红鼻头"},
	}
}

func feedAll(t *testing.T, chars []*store.Character, text string) ([]ParserEvent, []*Segment) {
	t.Helper()
	p := NewParser(chars)
	evs := p.Feed(text)
	evs = append(evs, p.Finish()...)
	return evs, p.Segments()
}

func TestParserBasicTags(t *testing.T) {
	_, segs := feedAll(t, mkChars(), `
[旁白]夜色像浸了墨的绸缎。
[艾莉娅·动作]她按住剑柄。
[艾莉娅·内心]（有埋伏。）
[艾莉娅]"别出声。"
`)
	if len(segs) != 4 {
		t.Fatalf("期望 4 段，得到 %d：%+v", len(segs), segs)
	}
	want := []struct {
		kind, sp, name, text string
		cid                  string
	}{
		{store.StyleNarration, store.SpeakerNarrator, "", "夜色像浸了墨的绸缎。", ""},
		{store.StyleAction, store.SpeakerCharacter, "艾莉娅", "她按住剑柄。", "c_a"},
		{store.StyleThought, store.SpeakerCharacter, "艾莉娅", "（有埋伏。）", "c_a"},
		{store.StyleDialogue, store.SpeakerCharacter, "艾莉娅", "\"别出声。\"", "c_a"},
	}
	for i, w := range want {
		s := segs[i]
		if s.Kind != w.kind || s.SpeakerType != w.sp || s.SpeakerName != w.name || s.Text() != w.text || s.CharacterID != w.cid {
			t.Errorf("段 %d = %+v, 期望 %+v", i, s, w)
		}
	}
}

func TestParserStreamingChunked(t *testing.T) {
	text := "[旁白]雾从山脊漫下来。\n[老巴德]进来喝一杯吧。\n"
	p := NewParser(mkChars())
	var evs []ParserEvent
	// 逐 rune 喂入，模拟真实流式。
	for _, r := range text {
		evs = append(evs, p.Feed(string(r))...)
	}
	evs = append(evs, p.Finish()...)

	var deltas, starts, ends int
	for _, ev := range evs {
		switch ev.Type {
		case "segment_start":
			starts++
		case "delta":
			deltas++
		case "segment_end":
			ends++
		}
	}
	if starts != 2 || ends != 2 || deltas == 0 {
		t.Fatalf("事件计数异常 starts=%d ends=%d deltas=%d", starts, ends, deltas)
	}
	segs := p.Segments()
	if len(segs) != 2 || segs[0].Text() != "雾从山脊漫下来。" || segs[1].Text() != "进来喝一杯吧。" {
		t.Fatalf("分段内容错误：%+v", segs)
	}
	if segs[1].SpeakerType != store.SpeakerCharacter || segs[1].CharacterID != "c_b" {
		t.Fatalf("老巴德未解析为角色：%+v", segs[1])
	}
}

func TestParserUnknownSpeakerIsNPC(t *testing.T) {
	_, segs := feedAll(t, mkChars(), "[神秘旅人]借个火。\n")
	if len(segs) != 1 {
		t.Fatalf("段数 %d", len(segs))
	}
	if segs[0].SpeakerType != store.SpeakerNPC || segs[0].SpeakerName != "神秘旅人" {
		t.Fatalf("应为 NPC：%+v", segs[0])
	}
}

func TestParserBareDialogueFallback(t *testing.T) {
	_, segs := feedAll(t, mkChars(), "老巴德：这杯算我请。\n")
	if len(segs) != 1 || segs[0].CharacterID != "c_b" || segs[0].Text() != "这杯算我请。" {
		t.Fatalf("裸台词兜底失败：%+v", segs)
	}
}

func TestParserPreambleBecomesNarration(t *testing.T) {
	_, segs := feedAll(t, mkChars(), "好的，下面是剧情：\n[旁白]天亮了。\n")
	// 前言行应落入旁白段。
	if len(segs) != 2 || segs[0].Kind != store.StyleNarration {
		t.Fatalf("前言处理错误：%+v", segs)
	}
	if !strings.Contains(segs[0].Text(), "下面是剧情") {
		t.Fatalf("前言内容丢失：%q", segs[0].Text())
	}
}

func TestParserMultiLineContinuation(t *testing.T) {
	_, segs := feedAll(t, mkChars(), "[旁白]第一行。\n第二行。\n[艾莉娅]好。\n")
	if len(segs) != 2 {
		t.Fatalf("段数 %d", len(segs))
	}
	if segs[0].Text() != "第一行。\n第二行。" {
		t.Fatalf("多行合并失败：%q", segs[0].Text())
	}
}

func TestParserFullWidthBracketsAndPipe(t *testing.T) {
	_, segs := feedAll(t, mkChars(), "【艾莉娅|内心】（不妙。）\n")
	if len(segs) != 1 || segs[0].Kind != store.StyleThought || segs[0].CharacterID != "c_a" {
		t.Fatalf("全角括号解析失败：%+v", segs)
	}
}

func TestParserMarkdownBoldStripped(t *testing.T) {
	_, segs := feedAll(t, mkChars(), "[旁白]**夜色**渐深。\n")
	if segs[0].Text() != "夜色渐深。" {
		t.Fatalf("加粗未剥离：%q", segs[0].Text())
	}
}

func TestParserEmptySegmentsSkipped(t *testing.T) {
	_, segs := feedAll(t, mkChars(), "[艾莉娅]\n[旁白]只有这段有内容。\n")
	// [艾莉娅] 行无内容 → 不产生有效分段。
	if len(segs) != 1 || segs[0].Text() != "只有这段有内容。" {
		t.Fatalf("空段处理错误：%+v", segs)
	}
}
