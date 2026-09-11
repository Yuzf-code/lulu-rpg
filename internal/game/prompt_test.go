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

	msgs := BuildChatMessages("东方王朝", persona, chars, "此前：一行人抵达客栈。", nil, history, userMsg, testCfg())

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
	msgs := BuildChatMessages("", nil, chars, "", nil, history, history[len(history)-1], cfg)

	// 历史必然被大幅裁剪：总消息数远小于 50，且最新一条内容必须保留。
	if len(msgs) >= 12 {
		t.Fatalf("历史未按预算裁剪：%d 条", len(msgs))
	}
	last := msgs[len(msgs)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "撑爆预算") {
		t.Fatalf("最新输入丢失：%+v", last)
	}
}

func TestPrivateMemoryInjection(t *testing.T) {
	chars := mkChars()
	privates := []PrivateMemory{{
		Title:        "私聊 · 艾莉娅",
		Summary:      "艾莉娅与玩家私下约定了暗号「北风」，并透露了她对修士的怀疑。",
		CharacterIDs: []string{"c_a"},
	}}
	msgs := BuildChatMessages("", &store.Persona{Name: "林远"}, chars, "", privates, nil, nil, testCfg())
	sys := msgs[0].Content

	idxA := strings.Index(sys, "1. 艾莉娅")
	idxB := strings.Index(sys, "2. 老巴德")
	idxSecret := strings.Index(sys, "北风")
	if idxA < 0 || idxB < 0 || idxSecret < 0 {
		t.Fatalf("关键段落缺失: %d %d %d", idxA, idxSecret, idxB)
	}
	// 私下经历必须挂在 A 的卡段内（A 之后、B 之前）。
	if !(idxA < idxSecret && idxSecret < idxB) {
		t.Fatalf("私下经历未挂在正确角色下: A=%d secret=%d B=%d", idxA, idxSecret, idxB)
	}
	if !strings.Contains(sys, "其他角色并不知情") {
		t.Fatal("缺少知情范围说明")
	}
	if !strings.Contains(sys, "其他角色不应表现出知情") {
		t.Fatal("缺少规则第 6 条")
	}

	// 无私聊时不应出现该段落（规则第 6 条里的「私下经历」字样除外）。
	msgs2 := BuildChatMessages("", nil, chars, "", nil, nil, nil, testCfg())
	if strings.Contains(msgs2[0].Content, "私下经历（仅该角色知晓") {
		t.Fatal("无私聊时不应出现私下经历段落")
	}
}

func TestSessionMemory(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/mem.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ca := &store.Character{Name: "艾莉娅"}
	cb := &store.Character{Name: "老巴德"}
	_ = st.CreateCharacter(ca)
	_ = st.CreateCharacter(cb)

	parent := &store.Session{Title: "主线"}
	if err := st.CreateSession(parent, []string{ca.ID, cb.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSummary(parent.ID, "主线：一行人抵达钟楼。", 5, 0); err != nil {
		t.Fatal(err)
	}
	child := &store.Session{Title: "私聊 · 艾莉娅", ParentID: parent.ID, InheritedSummary: "主线：一行人抵达钟楼。"}
	if err := st.CreateSession(child, []string{ca.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSummary(child.ID, "私下：艾莉娅与玩家约定了暗号。", 3, 0); err != nil {
		t.Fatal(err)
	}

	e := New(st, nil, nil, testCfg())

	// 主线视角：有效摘要 = 自身摘要；私下记忆把 child 摘要挂到 A。
	// （sessionMemory 使用传入的会话对象，生产路径中总是新加载的；这里重新加载。）
	parentFresh, err := st.GetSession(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	eff, priv, err := e.sessionMemory(parentFresh)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(eff) != "主线：一行人抵达钟楼。" {
		t.Fatalf("主线有效摘要错误: %q", eff)
	}
	if len(priv) != 1 || priv[0].Summary != "私下：艾莉娅与玩家约定了暗号。" || len(priv[0].CharacterIDs) != 1 || priv[0].CharacterIDs[0] != ca.ID {
		t.Fatalf("私下记忆错误: %+v", priv)
	}

	// 私聊视角：有效摘要 = 主线（+继承）+ 自身私下；无私聊子级。
	childFresh, err := st.GetSession(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	eff2, priv2, err := e.sessionMemory(childFresh)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(eff2, "主线：一行人抵达钟楼。") || !strings.Contains(eff2, "私下：艾莉娅与玩家约定了暗号。") {
		t.Fatalf("私聊有效摘要错误: %q", eff2)
	}
	if len(priv2) != 0 {
		t.Fatalf("私聊不应有下级私下记忆: %+v", priv2)
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
