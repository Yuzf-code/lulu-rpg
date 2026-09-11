package api

import (
	"strings"
	"testing"
)

func TestDistillAndDraftsAPI(t *testing.T) {
	ts := newTestServer(t)

	// 开场白草稿
	r := jreq(t, ts, "POST", "/api/characters/generate-draft",
		map[string]any{"kind": "greeting", "name": "艾莉娅", "personality": "冷静"}, 200)
	if r["greeting"] == "" || r["greeting"] == nil {
		t.Fatalf("开场白草稿为空: %v", r)
	}

	// 对话示例草稿
	r = jreq(t, ts, "POST", "/api/characters/generate-draft",
		map[string]any{"kind": "dialogues", "name": "艾莉娅"}, 200)
	rows := r["example_dialogues"].([]any)
	if len(rows) == 0 {
		t.Fatal("对话示例草稿为空")
	}

	// 非法 kind
	jreq(t, ts, "POST", "/api/characters/generate-draft", map[string]any{"kind": "x"}, 400)
	// 缺名称
	jreq(t, ts, "POST", "/api/characters/generate-draft", map[string]any{"kind": "greeting"}, 400)

	// 人物识别
	r = jreq(t, ts, "POST", "/api/characters/distill-detect", map[string]any{
		"text": "一段关于艾莉娅的小说文本。",
	}, 200)
	if names := r["targets"].([]any); len(names) == 0 {
		t.Fatal("人物识别结果为空")
	}

	// 蒸馏：单对象 + 主体
	r = jreq(t, ts, "POST", "/api/characters/distill", map[string]any{
		"text": "一段关于艾莉娅在雾谷活动的小说文本，篇幅足够提取设定。", "subject": "林远", "target": "艾莉娅",
	}, 200)
	c := r["character"].(map[string]any)
	if c["name"] != "艾莉娅" {
		t.Fatalf("卡名错误: %v", c)
	}
	rels := c["relationships"].([]any)
	if len(rels) == 0 || rels[0].(map[string]any)["subject"] != "林远" {
		t.Fatalf("主体关系缺失: %v", rels)
	}

	// 空原文 / 缺对象
	jreq(t, ts, "POST", "/api/characters/distill", map[string]any{"text": "  ", "target": "x"}, 400)
	jreq(t, ts, "POST", "/api/characters/distill", map[string]any{"text": "有原文", "target": ""}, 400)

	// 风格蒸馏
	r = jreq(t, ts, "POST", "/api/characters/distill-style", map[string]any{
		"text": "一段关于艾莉娅的小说文本。",
	}, 200)
	st := r["style"].(map[string]any)
	if st["name"] == "" || st["description"] == "" {
		t.Fatalf("风格草稿不完整: %v", st)
	}

	// 蒸馏增强已有卡：合并结果应保留原卡名并带来新头衔
	charA := jreq(t, ts, "POST", "/api/characters", map[string]any{
		"name": "艾莉娅", "title": "老头衔", "background": "旧背景",
	}, 201)
	r = jreq(t, ts, "POST", "/api/characters/"+charA["id"].(string)+"/distill",
		map[string]any{"text": "补充文本：她在雾谷守了三年。", "subject": ""}, 200)
	merged := r["character"].(map[string]any)
	if merged["name"] != "艾莉娅" || merged["title"] != "雾谷的守望者" {
		t.Fatalf("增强合并错误: %v %v", merged["name"], merged["title"])
	}
	if merged["background"] == "旧背景" {
		t.Fatal("背景应被蒸馏结果更新")
	}
	// 不存在的卡
	jreq(t, ts, "POST", "/api/characters/c_missing/distill", map[string]any{"text": "x"}, 404)
}

func TestInspirationAPI(t *testing.T) {
	ts := newTestServer(t)
	charA := jreq(t, ts, "POST", "/api/characters", map[string]any{"name": "艾莉娅"}, 201)
	sess := jreq(t, ts, "POST", "/api/sessions", map[string]any{
		"character_ids": []string{charA["id"].(string)}, "scenario": "雾谷",
	}, 201)
	sid := sess["id"].(string)

	// 开场后剧情有上下文
	postSSE(t, ts, "/api/sessions/"+sid+"/opening", map[string]any{})

	r := jreq(t, ts, "POST", "/api/sessions/"+sid+"/inspiration", map[string]any{}, 200)
	opts := r["options"].([]any)
	if len(opts) < 4 {
		t.Fatalf("灵感数量不足: %d", len(opts))
	}
	sayCount := 0
	for _, raw := range opts {
		o := raw.(map[string]any)
		k := o["kind"].(string)
		if k != "say" && k != "direct" {
			t.Fatalf("非法 kind: %s", k)
		}
		if k == "say" {
			sayCount++
		}
		if strings.TrimSpace(o["text"].(string)) == "" {
			t.Fatal("灵感文本为空")
		}
	}
	if sayCount == 0 || sayCount == len(opts) {
		t.Fatalf("台词与导演应混合: %d/%d", sayCount, len(opts))
	}

	// 不存在的会话
	jreq(t, ts, "POST", "/api/sessions/s_missing/inspiration", map[string]any{}, 404)
}

func TestReasoningEffortSetting(t *testing.T) {
	ts := newTestServer(t)

	// 默认 medium
	r := jreq(t, ts, "GET", "/api/settings", nil, 200)
	if r["reasoning_effort"] != "medium" {
		t.Fatalf("默认档位应为 medium: %v", r)
	}

	// 切换 + 回读
	jreq(t, ts, "PUT", "/api/settings", map[string]any{"reasoning_effort": "high"}, 200)
	r = jreq(t, ts, "GET", "/api/settings", nil, 200)
	if r["reasoning_effort"] != "high" {
		t.Fatalf("切换后应为 high: %v", r)
	}

	// 非法档位
	jreq(t, ts, "PUT", "/api/settings", map[string]any{"reasoning_effort": "ultra"}, 400)
}
