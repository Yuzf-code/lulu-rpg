package game

import (
	"strings"
	"testing"

	"lulu-rpg/internal/config"
	"lulu-rpg/internal/store"
)

func testCfg() *config.Config {
	return &config.Config{
		Context: config.ContextConfig{
			RecentMessages:   20,
			CompactThreshold: 12,
			MaxPromptTokens:  6000,
			MaxFieldChars:    700,
		},
	}
}

func TestBuildChatMessagesShape(t *testing.T) {
	chars := mkChars()
	persona := &store.Persona{Name: "林远", Description: "旅人"}
	history := []*store.Message{
		{Turn: 1, Kind: store.KindStory, Style: store.StyleNarration, SpeakerType: store.SpeakerNarrator, SpeakerName: "", Content: "开场。"},
		{Turn: 2, Kind: store.KindUser, Style: store.StyleSay, Content: "你好。"},
		{Turn: 2, Kind: store.KindStory, Style: store.StyleDialogue, SpeakerType: store.SpeakerCharacter, CharacterID: "c_a", SpeakerName: "艾莉娅", Content: "……嗯。"},
	}
	userMsg := &store.Message{Turn: 3, Kind: store.KindUser, Style: store.StyleDirect, Content: "让天下雨"}

	msgs := BuildChatMessages("东方王朝", persona, chars, "此前：一行人抵达客栈。", history, userMsg, testCfg())

	if msgs[0].Role != "system" || !strings.Contains(msgs[0].Content, "剧情写手") {
		t.Fatalf("系统提示缺失或错误")
	}
	if !strings.Contains(msgs[0].Content, "艾莉娅") || !strings.Contains(msgs[0].Content, "林远") {
		t.Fatalf("角色卡/档案未进入系统提示")
	}
	var foundSummary, foundSummaryContent bool
	for _, m := range msgs {
		if strings.Contains(m.Content, "此前剧情摘要") && strings.Contains(m.Content, "抵达客栈") {
			foundSummary, foundSummaryContent = true, true
		}
	}
	if !foundSummary || !foundSummaryContent {
		t.Fatalf("剧情摘要未注入")
	}

	// 历史还原：剧情分段合并为 assistant 且保留标签格式。
	joined := ""
	var roles []string
	for _, m := range msgs[1:] {
		roles = append(roles, m.Role)
		joined += m.Content + "\n"
	}
	if !strings.Contains(joined, "[旁白]开场。") || !strings.Contains(joined, "[艾莉娅]……嗯。") {
		t.Fatalf("历史标签还原失败：%q", joined)
	}
	if !strings.Contains(joined, "【林远（玩家扮演）说】你好。") {
		t.Fatalf("玩家台词格式错误")
	}
	if !strings.Contains(joined, "【导演指令】") || !strings.Contains(joined, "让天下雨") {
		t.Fatalf("导演指令格式错误")
	}
	if roles[len(roles)-1] != "user" {
		t.Fatalf("最后一条应为 user 消息")
	}
}

func TestBuildChatMessagesBudgetDropsOldest(t *testing.T) {
	chars := mkChars()
	cfg := testCfg()
	cfg.Context.MaxPromptTokens = 120 // 极小预算（只约束历史部分）
	var history []*store.Message
	for i := 0; i < 50; i++ {
		history = append(history, &store.Message{Turn: int64(i), Kind: store.KindUser,
			Style: store.StyleSay, Content: strings.Repeat("这是一条比较长的历史消息，用来撑爆预算。", 3)})
	}
	msgs := BuildChatMessages("", nil, chars, "", history, history[len(history)-1], cfg)

	// 历史必然被大幅裁剪：总消息数远小于 50，且最新一条内容必须保留。
	if len(msgs) >= 12 {
		t.Fatalf("历史未按预算裁剪：%d 条", len(msgs))
	}
	last := msgs[len(msgs)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "撑爆预算") {
		t.Fatalf("最新输入丢失：%+v", last)
	}
}

func TestBuildImagePrompt(t *testing.T) {
	chars := mkChars()
	segs := []*Segment{
		{Kind: store.StyleDialogue, SpeakerType: store.SpeakerCharacter, CharacterID: "c_a", SpeakerName: "艾莉娅"},
		{Kind: store.StyleNarration, SpeakerType: store.SpeakerNarrator},
		{Kind: store.StyleAction, SpeakerType: store.SpeakerCharacter, CharacterID: "c_a", SpeakerName: "艾莉娅"},
	}
	segs[1].text.WriteString("钟楼耸立在雾中。")
	segs[2].text.WriteString("她按住剑柄。")
	p := BuildImagePrompt("雾谷", chars, segs, "概念艺术")
	if !strings.Contains(p, "钟楼耸立在雾中") {
		t.Fatalf("应以旁白为画面主体：%q", p)
	}
	if !strings.Contains(p, "艾莉娅") || !strings.Contains(p, "概念艺术") {
		t.Fatalf("应包含出场角色外貌与风格词：%q", p)
	}
}

func TestEstimateTokens(t *testing.T) {
	if n := estimateTokens("abcd"); n != 1 { // 4 ascii ≈ 1 token
		t.Fatalf("ascii 估算 %d", n)
	}
	if n := estimateTokens("一二三四"); n != 4 {
		t.Fatalf("cjk 估算 %d", n)
	}
}
