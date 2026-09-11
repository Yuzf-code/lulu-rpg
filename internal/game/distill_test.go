package game

import (
	"strings"
	"testing"

	"lulu-rpg/internal/llm"
	"lulu-rpg/internal/store"
)

func TestExtractJSON(t *testing.T) {
	var m map[string]any
	if err := extractJSON("  前缀噪声\n```json\n{\"a\":1}\n```\n后缀", &m); err != nil || m["a"] != float64(1) {
		t.Fatalf("围栏解析失败: %v %v", m, err)
	}
	var arr []map[string]any
	if err := extractJSON(`结果如下：[{"kind":"say","text":"hi"}] 希望有帮助`, &arr); err != nil || len(arr) != 1 {
		t.Fatalf("数组解析失败: %v %v", arr, err)
	}
	if err := extractJSON("完全没有结构化内容", &m); err == nil {
		t.Fatal("无 JSON 时应报错")
	}
}

func TestDistillWithMock(t *testing.T) {
	e := New(nil, &llm.Mock{}, nil, testCfg())
	out, err := e.Distill(t.Context(), "这是一段小说文本，讲述艾莉娅与老巴德的故事。", "林远", []string{"艾莉娅", "老巴德"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Characters) != 2 {
		t.Fatalf("应有两个结果: %d", len(out.Characters))
	}
	if out.Characters[0].Character == nil || out.Characters[0].Character.Name != "艾莉娅" {
		t.Fatalf("第一张卡错误: %+v", out.Characters[0])
	}
	// 指定主体时应生成对主体的关系。
	rels := out.Characters[0].Character.Relationships
	if len(rels) == 0 || rels[0].Subject != "林远" {
		t.Fatalf("主体关系缺失: %+v", rels)
	}
	// 应同时产出分视角的写作风格草稿。
	if out.Style == nil || out.Style.Name == "" || !strings.Contains(out.Style.Description, "[旁白]") {
		t.Fatalf("风格草稿缺失或未分视角: %+v", out.Style)
	}
}

func TestDistillAutoDetectTargets(t *testing.T) {
	e := New(nil, &llm.Mock{}, nil, testCfg())
	// Mock 的蒸馏任务在 Target 为空时返回默认名字「无名旅人」→ 能解析出人物。
	out, err := e.Distill(t.Context(), "一些文本", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Characters) == 0 || out.Characters[0].Character == nil {
		t.Fatalf("自动识别蒸馏失败: %+v", out)
	}
}

func TestDistillEmptyText(t *testing.T) {
	e := New(nil, &llm.Mock{}, nil, testCfg())
	if _, err := e.Distill(t.Context(), "  ", "", []string{"x"}); err == nil {
		t.Fatal("空原文应报错")
	}
}

func TestMergeCard(t *testing.T) {
	base := &store.Character{
		Name: "艾莉娅", Title: "剑士", Appearance: "银发",
		Tags:             []byte(`["佣兵"]`),
		ExampleDialogues: []byte(`[{"user":"a","char":"b"}]`),
		Relationships:    []byte(`[{"subject":"林远","text":"旧关系"}]`),
	}
	d := &DistilledCard{
		Title: "流浪剑士", Background: "新背景",
		Tags:             []string{"佣兵", "守望者"},
		ExampleDialogues: []store.ExampleDialogue{{User: "c", Char: "d"}},
		Relationships:    []store.Relationship{{Subject: "林远", Text: "新关系"}, {Subject: "老巴德", Text: "酒友"}},
	}
	m := mergeCard(base, d)
	if m.Title != "流浪剑士" || m.Appearance != "银发" || m.Background != "新背景" {
		t.Fatalf("字段合并错误: %+v", m)
	}
	if len(m.Tags) != 2 || len(m.ExampleDialogues) != 2 {
		t.Fatalf("列表合并错误: tags=%v dlg=%d", m.Tags, len(m.ExampleDialogues))
	}
	// 同主体关系应被新结果覆盖，异主体追加。
	if len(m.Relationships) != 2 {
		t.Fatalf("关系合并数量错误: %+v", m.Relationships)
	}
	for _, r := range m.Relationships {
		if r.Subject == "林远" && r.Text != "新关系" {
			t.Fatalf("同主体应被新结果覆盖: %+v", r)
		}
	}
}

func TestDraftGreetingAndDialogues(t *testing.T) {
	e := New(nil, &llm.Mock{}, nil, testCfg())
	g, err := e.DraftGreeting(t.Context(), CardSeed{Name: "艾莉娅", Personality: "冷"})
	if err != nil || !strings.Contains(g, "开场") && !strings.Contains(g, "坐下") {
		t.Fatalf("开场白草稿异常: %q %v", g, err)
	}
	rows, err := e.DraftDialogues(t.Context(), CardSeed{Name: "艾莉娅"})
	if err != nil || len(rows) < 2 {
		t.Fatalf("对话示例草稿异常: %v %v", rows, err)
	}
}

func TestInspiration(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/insp.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sess := &store.Session{Title: "局"}
	if err := st.CreateSession(sess, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.AddMessages([]*store.Message{
		{SessionID: sess.ID, Turn: 1, Kind: store.KindStory, Style: store.StyleNarration, SpeakerType: store.SpeakerNarrator, Content: "钟楼近在眼前。"},
	}); err != nil {
		t.Fatal(err)
	}
	e := New(st, &llm.Mock{}, nil, testCfg())
	opts, err := e.Inspiration(t.Context(), sess)
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) < 4 {
		t.Fatalf("灵感条数不足: %d", len(opts))
	}
	sayCount := 0
	for _, o := range opts {
		if o.Kind != "say" && o.Kind != "direct" {
			t.Fatalf("非法 kind: %s", o.Kind)
		}
		if o.Kind == "say" {
			sayCount++
		}
	}
	if sayCount == 0 || sayCount == len(opts) {
		t.Fatalf("台词与导演指令应混合: %d/%d", sayCount, len(opts))
	}
}
