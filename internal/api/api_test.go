package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lulu-rpg/internal/config"
	"lulu-rpg/internal/game"
	"lulu-rpg/internal/imggen"
	"lulu-rpg/internal/llm"
	"lulu-rpg/internal/store"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Addr: "", DataDir: dir, WebDir: "../../web",
		LLM:   config.LLMConfig{Provider: config.ProviderMock, Temperature: 0.9, MaxTokens: 512},
		Image: config.ImageConfig{Model: "mock", StyleSuffix: "测试风格"},
		Context: config.ContextConfig{
			RecentMessages: 20, CompactThreshold: 12, MaxPromptTokens: 6000, MaxFieldChars: 700,
		},
	}
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	eng := game.New(st, &llm.Mock{Delay: time.Microsecond}, imggen.NewMock(), cfg)
	ts := httptest.NewServer(New(st, eng, cfg).Handler())
	t.Cleanup(func() { ts.Close(); st.Close() })
	return ts
}

// jreq 发送 JSON 请求并解析响应；wantCode<0 表示不校验。
func jreq(t *testing.T, ts *httptest.Server, method, path string, body any, wantCode int) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, ts.URL+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if wantCode >= 0 && resp.StatusCode != wantCode {
		t.Fatalf("%s %s = %d（期望 %d）: %v", method, path, resp.StatusCode, wantCode, out)
	}
	return out
}

// postSSE 发起流式请求并收集事件，直到 done。
func postSSE(t *testing.T, ts *httptest.Server, path string, body any) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	resp, err := http.Post(ts.URL+path, "application/json", &buf)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s = %d", path, resp.StatusCode)
	}
	var events []map[string]any
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data:")), &ev) != nil {
			continue
		}
		events = append(events, ev)
		if ev["type"] == "done" {
			break
		}
	}
	return events
}

func countType(events []map[string]any, typ string) int {
	n := 0
	for _, ev := range events {
		if ev["type"] == typ {
			n++
		}
	}
	return n
}

func TestFullGameFlow(t *testing.T) {
	ts := newTestServer(t)

	// 健康检查与配置
	jreq(t, ts, "GET", "/api/health", nil, 200)
	cfgResp := jreq(t, ts, "GET", "/api/config", nil, 200)
	if cfgResp["llm"].(map[string]any)["provider"] != "mock" {
		t.Fatalf("provider 应为 mock: %v", cfgResp)
	}
	if cfgResp["image"].(map[string]any)["enabled"] != true {
		t.Fatalf("image 应已启用")
	}

	// 建角色与档案
	charA := jreq(t, ts, "POST", "/api/characters", map[string]any{
		"name": "艾莉娅", "title": "剑士", "personality": "冷静", "tags": []string{"佣兵"},
		"example_dialogues": []map[string]string{{"user": "hi", "char": "嗯。"}},
	}, 201)
	charB := jreq(t, ts, "POST", "/api/characters", map[string]any{
		"name": "老巴德", "personality": "热络",
	}, 201)
	persona := jreq(t, ts, "POST", "/api/personas", map[string]any{
		"name": "林远", "description": "旅人", "is_default": true,
	}, 201)

	// 无角色建会话应被拒绝
	jreq(t, ts, "POST", "/api/sessions", map[string]any{"character_ids": []string{}}, 400)

	// 建会话
	sess := jreq(t, ts, "POST", "/api/sessions", map[string]any{
		"character_ids": []string{charA["id"].(string), charB["id"].(string)},
		"persona_id":    persona["id"],
		"scenario":      "测试世界设定",
		"auto_image":    true,
	}, 201)
	sid := sess["id"].(string)
	if len(sess["characters"].([]any)) != 2 {
		t.Fatalf("会话角色数错误")
	}

	// 开场（SSE）
	events := postSSE(t, ts, "/api/sessions/"+sid+"/opening", map[string]any{})
	if countType(events, "segment_start") == 0 || countType(events, "delta") == 0 || countType(events, "done") != 1 {
		t.Fatalf("开场事件流异常: %d 个事件", len(events))
	}
	msgs := jreq(t, ts, "GET", "/api/sessions/"+sid+"/messages", nil, 200)["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatal("开场后应有剧情消息")
	}

	// 回合：台词模式 + 自动配图
	events = postSSE(t, ts, "/api/sessions/"+sid+"/turn", map[string]any{
		"content": "大家好，我推门进来了。", "mode": "say",
	})
	if countType(events, "user_saved") != 1 {
		t.Fatalf("缺少 user_saved: %v", events)
	}
	if countType(events, "image") != 1 {
		t.Fatalf("auto_image 开启时应有配图事件: %v", events)
	}
	// 事件里的消息应含玩家输入与角色分段
	hasUser := false
	for _, ev := range events {
		if ev["type"] == "user_saved" {
			m := ev["message"].(map[string]any)
			hasUser = m["content"] == "大家好，我推门进来了。" && m["style"] == "say"
		}
	}
	if !hasUser {
		t.Fatal("user_saved 内容不正确")
	}

	msgs = jreq(t, ts, "GET", "/api/sessions/"+sid+"/messages", nil, 200)["messages"].([]any)
	var userCount, storyCount, imgCount int
	for _, raw := range msgs {
		m := raw.(map[string]any)
		switch m["kind"] {
		case "user":
			userCount++
		case "story":
			storyCount++
			if m["image"] != nil {
				imgCount++
			}
		}
	}
	if userCount != 1 || storyCount < 3 {
		t.Fatalf("消息统计异常 user=%d story=%d", userCount, storyCount)
	}
	if imgCount == 0 {
		t.Fatal("图片未挂到回合消息上")
	}

	// 回合：导演模式，明确关闭配图
	events = postSSE(t, ts, "/api/sessions/"+sid+"/turn", map[string]any{
		"content": "让突然下起大雨", "mode": "direct", "generate_image": false,
	})
	if countType(events, "image") != 0 {
		t.Fatal("generate_image=false 不应配图")
	}

	// 空输入
	jreq(t, ts, "POST", "/api/sessions/"+sid+"/turn", map[string]any{"content": ""}, 400)
	// 不存在的会话
	jreq(t, ts, "GET", "/api/sessions/s_missing", nil, 404)
	jreq(t, ts, "POST", "/api/sessions/s_missing/turn", map[string]any{"content": "x"}, 404)

	// 派生私聊
	child := jreq(t, ts, "POST", "/api/sessions", map[string]any{
		"parent_id":     sid,
		"character_ids": []string{charA["id"].(string)},
	}, 201)
	if child["parent_id"] != sid {
		t.Fatal("派生会话应记录 parent_id")
	}
	if !strings.Contains(child["title"].(string), "私聊") {
		t.Fatalf("派生会话标题应含「私聊」: %v", child["title"])
	}
	cid := child["id"].(string)
	_ = cid

	// 会话列表（数组响应，用原始请求解析）
	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&items)
	resp.Body.Close()
	if len(items) != 2 {
		t.Fatalf("应有 2 个会话: %d", len(items))
	}

	// 更新会话
	jreq(t, ts, "PATCH", "/api/sessions/"+sid, map[string]any{"title": "新标题", "auto_image": false}, 200)
	got := jreq(t, ts, "GET", "/api/sessions/"+sid, nil, 200)
	gs := got["session"].(map[string]any)
	if gs["title"] != "新标题" || gs["auto_image"] != false {
		t.Fatalf("会话更新失败: %v %v", gs["title"], gs["auto_image"])
	}
	// 主线应能看到派生私聊（记忆回流列表）。
	chats := got["private_chats"].([]any)
	if len(chats) != 1 || chats[0].(map[string]any)["id"] != cid {
		t.Fatalf("private_chats 应包含刚派生的私聊: %v", got["private_chats"])
	}

	// 删除
	jreq(t, ts, "DELETE", "/api/sessions/"+cid, nil, 200)
	jreq(t, ts, "DELETE", "/api/sessions/"+sid, nil, 200)
	req, _ = http.NewRequest("GET", ts.URL+"/api/sessions", nil)
	resp, _ = http.DefaultClient.Do(req)
	var left []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&left)
	resp.Body.Close()
	if len(left) != 0 {
		t.Fatalf("删除后列表应为空: %d", len(left))
	}
	_ = cid
}

func TestGreetingOpening(t *testing.T) {
	ts := newTestServer(t)
	charA := jreq(t, ts, "POST", "/api/characters", map[string]any{
		"name": "艾莉娅", "greeting": "（她抬起头）你来了。",
	}, 201)
	sess := jreq(t, ts, "POST", "/api/sessions", map[string]any{
		"character_ids": []string{charA["id"].(string)},
	}, 201)
	sid := sess["id"].(string)

	events := postSSE(t, ts, "/api/sessions/"+sid+"/opening", map[string]any{})
	// 卡牌开场白路径：单段 + done，不调用模型。
	if countType(events, "segment_start") != 1 || countType(events, "done") != 1 {
		t.Fatalf("卡牌开场白事件异常: %v", events)
	}
	msgs := jreq(t, ts, "GET", "/api/sessions/"+sid+"/messages", nil, 200)["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("应只有 1 条消息: %d", len(msgs))
	}
	m := msgs[0].(map[string]any)
	if m["content"] != "（她抬起头）你来了。" || m["style"] != "dialogue" {
		t.Fatalf("开场白消息不正确: %v", m)
	}
	// 重复触发开场应幂等返回 done
	events = postSSE(t, ts, "/api/sessions/"+sid+"/opening", map[string]any{})
	if len(events) != 1 || events[0]["type"] != "done" {
		t.Fatalf("重复开场应幂等: %v", events)
	}
}

func TestUploadAndAvatar(t *testing.T) {
	ts := newTestServer(t)
	// 1x1 PNG 的 data URL
	dataURL := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	up := jreq(t, ts, "POST", "/api/uploads", map[string]any{"data_url": dataURL}, 201)
	url := up["url"].(string)
	if !strings.HasPrefix(url, "/files/avatars/") {
		t.Fatalf("上传路径异常: %s", url)
	}
	// 引用到角色卡
	c := jreq(t, ts, "POST", "/api/characters", map[string]any{"name": "有头像", "avatar_path": url}, 201)
	if c["avatar_path"] != url {
		t.Fatal("头像路径未保存")
	}
	// 非法 data URL
	jreq(t, ts, "POST", "/api/uploads", map[string]any{"data_url": "http://evil/x.png"}, 400)

	// Mock 画师生成头像
	av := jreq(t, ts, "POST", "/api/avatars/generate", map[string]any{"name": "艾莉娅", "appearance": "银发"}, 200)
	if !strings.HasPrefix(av["url"].(string), "/files/avatars/") {
		t.Fatalf("生成头像路径异常: %v", av)
	}

	// 头像生成端点对角色卡本身
	cid := c["id"].(string)
	up2 := jreq(t, ts, "POST", "/api/characters/"+cid+"/avatar", map[string]any{}, 200)
	if !strings.HasPrefix(up2["avatar_path"].(string), "/files/avatars/") {
		t.Fatalf("角色头像重生成失败: %v", up2)
	}
}

func TestConcurrentTurnRejected(t *testing.T) {
	// 用一个慢 Mock 保证窗口期，第二个并发请求应得到 409。
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir: dir, WebDir: "../../web",
		LLM:     config.LLMConfig{Provider: config.ProviderMock, MaxTokens: 512},
		Image:   config.ImageConfig{Model: "mock"},
		Context: config.ContextConfig{RecentMessages: 20, CompactThreshold: 12, MaxPromptTokens: 6000, MaxFieldChars: 700},
	}
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// 25ms/字符 × 开场 ~200 字符 ≈ 5 秒，足以保证并发窗口。
	eng := game.New(st, llm.NewMock(), imggen.NewMock(), cfg)
	ts := httptest.NewServer(New(st, eng, cfg).Handler())
	defer ts.Close()

	c := jreq(t, ts, "POST", "/api/characters", map[string]any{"name": "艾莉娅"}, 201)
	sess := jreq(t, ts, "POST", "/api/sessions", map[string]any{"character_ids": []string{c["id"].(string)}}, 201)
	sid := sess["id"].(string)

	type result struct {
		code int
	}
	done := make(chan result, 2)
	fire := func() {
		resp, err := http.Post(ts.URL+"/api/sessions/"+sid+"/opening", "application/json", bytes.NewReader([]byte("{}")))
		if err != nil {
			done <- result{code: -1}
			return
		}
		code := resp.StatusCode
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		done <- result{code: code}
	}
	go fire()
	time.Sleep(150 * time.Millisecond) // 等第一发进入生成状态
	go fire()

	codes := map[int]int{}
	for i := 0; i < 2; i++ {
		r := <-done
		codes[r.code]++
	}
	if codes[409] != 1 || codes[200] != 1 {
		t.Fatalf("应为一个 200 一个 409: %v", codes)
	}
}
