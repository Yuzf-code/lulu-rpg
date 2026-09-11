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

	// 蒸馏：显式对象 + 主体
	r = jreq(t, ts, "POST", "/api/characters/distill", map[string]any{
		"text": "一段关于艾莉娅在雾谷活动的小说文本，篇幅足够提取设定。", "subject": "林远", "targets": []string{"艾莉娅"},
	}, 200)
	drafts := r["drafts"].([]any)
	if len(drafts) != 1 {
		t.Fatalf("应有一个蒸馏结果: %v", r)
	}
	d0 := drafts[0].(map[string]any)
	if d0["target"] != "艾莉娅" {
		t.Fatalf("蒸馏对象错误: %v", d0)
	}
	c := d0["character"].(map[string]any)
	if c["name"] != "艾莉娅" {
		t.Fatalf("卡名错误: %v", c)
	}
	rels := c["relationships"].([]any)
	if len(rels) == 0 || rels[0].(map[string]any)["subject"] != "林远" {
		t.Fatalf("主体关系缺失: %v", rels)
	}

	// 空原文
	jreq(t, ts, "POST", "/api/characters/distill", map[string]any{"text": "  "}, 400)

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
