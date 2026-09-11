package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCharacterCRUD(t *testing.T) {
	s := openTest(t)
	c := &Character{Name: "艾莉娅", Title: "剑士", Personality: "冷",
		ExampleDialogues: json.RawMessage(`[{"user":"hi","char":"嗯"}]`), Tags: json.RawMessage(`["佣兵"]`)}
	if err := s.CreateCharacter(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCharacter(c.ID)
	if err != nil || got.Name != "艾莉娅" || string(got.ExampleDialogues) != `[{"user":"hi","char":"嗯"}]` {
		t.Fatalf("读取失败: %+v %v", got, err)
	}
	got.Title = "流浪剑士"
	if err := s.UpdateCharacter(got); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListCharacters()
	if err != nil || len(list) != 1 || list[0].Title != "流浪剑士" {
		t.Fatalf("列表失败: %+v %v", list, err)
	}
	if err := s.DeleteCharacter(c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCharacter(c.ID); err != ErrNotFound {
		t.Fatalf("删除后应 ErrNotFound，得到 %v", err)
	}
}

func TestSessionAndMessages(t *testing.T) {
	s := openTest(t)
	p := &Persona{Name: "林远", IsDefault: true}
	if err := s.CreatePersona(p); err != nil {
		t.Fatal(err)
	}
	ca, cb := &Character{Name: "艾莉娅"}, &Character{Name: "老巴德"}
	_ = s.CreateCharacter(ca)
	_ = s.CreateCharacter(cb)

	sess := &Session{Title: "测试局", Scenario: "雾谷", PersonaID: p.ID}
	if err := s.CreateSession(sess, []string{ca.ID, cb.ID}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(sess.ID)
	if err != nil || got.Title != "测试局" || len(got.Characters) != 2 || got.Characters[0].Name != "艾莉娅" {
		t.Fatalf("会话读取失败: %+v %v", got, err)
	}
	if got.Persona == nil || got.Persona.Name != "林远" {
		t.Fatalf("档案未填充")
	}

	turn1, _ := s.NextTurn(sess.ID)
	turn2, _ := s.NextTurn(sess.ID)
	if turn1 != 1 || turn2 != 2 {
		t.Fatalf("回合号分配错误: %d %d", turn1, turn2)
	}

	msgs := []*Message{
		{SessionID: sess.ID, Turn: turn1, Kind: KindUser, Style: StyleSay, Content: "你好"},
		{SessionID: sess.ID, Turn: turn1, Kind: KindStory, Style: StyleNarration, SpeakerType: SpeakerNarrator, Content: "夜幕降临。"},
		{SessionID: sess.ID, Turn: turn2, Kind: KindStory, Style: StyleDialogue, SpeakerType: SpeakerCharacter, CharacterID: ca.ID, SpeakerName: "艾莉娅", Content: "……嗯。"},
	}
	if err := s.AddMessages(msgs); err != nil {
		t.Fatal(err)
	}
	if msgs[0].Seq == 0 || msgs[1].Seq != msgs[0].Seq+1 {
		t.Fatalf("seq 应连续递增: %d %d", msgs[0].Seq, msgs[1].Seq)
	}
	all, err := s.ListMessages(sess.ID, 0, 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("消息读取失败: %v", err)
	}
	recent, _ := s.ListMessages(sess.ID, 0, 2)
	if len(recent) != 2 || recent[0].Content != "夜幕降临。" {
		t.Fatalf("limit 应取最新 N 条: %+v", recent)
	}

	if err := s.AddImage(&SessionImage{SessionID: sess.ID, Turn: turn1, Path: "/files/images/x.png"}); err != nil {
		t.Fatal(err)
	}
	withImg, _ := s.ListMessages(sess.ID, 0, 0)
	// 图片属于回合 1：应挂在回合 1 的首条剧情消息（第 2 条）上，而非玩家输入。
	if withImg[0].Image != nil || withImg[1].Image == nil || withImg[2].Image != nil {
		t.Fatalf("图片应挂在回合首条剧情消息上: %v", withImg)
	}

	// 派生：复制记忆
	if err := s.SaveSummary(sess.ID, "剧情摘要", msgs[1].Seq, 0); err != nil {
		t.Fatal(err)
	}
	child := &Session{Title: "私聊", ParentID: sess.ID}
	if err := s.CreateSession(child, []string{ca.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.CopyStoryMemory(sess.ID, child.ID); err != nil {
		t.Fatal(err)
	}
	cgot, _ := s.GetSession(child.ID)
	if cgot.Summary != "剧情摘要" {
		t.Fatalf("摘要未复制: %q", cgot.Summary)
	}

	// 级联删除
	if err := s.DeleteSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	if left, _ := s.ListMessages(sess.ID, 0, 0); len(left) != 0 {
		t.Fatalf("级联删除失败")
	}
}

func TestSaveSummaryOptimistic(t *testing.T) {
	s := openTest(t)
	sess := &Session{Title: "局"}
	_ = s.CreateSession(sess, nil)
	if err := s.SaveSummary(sess.ID, "v1", 5, 0); err != nil {
		t.Fatal(err)
	}
	// 期望值不匹配时应静默失败（不覆盖）。
	if err := s.SaveSummary(sess.ID, "v2", 9, 0); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSession(sess.ID)
	if got.Summary != "v1" || got.SummarizedUptoSeq != 5 {
		t.Fatalf("乐观锁失效: %+v", got)
	}
}

func TestSeedDemoDataOnlyOnce(t *testing.T) {
	s := openTest(t)
	if err := SeedDemoData(s); err != nil {
		t.Fatal(err)
	}
	cs, _ := s.ListCharacters()
	ps, _ := s.ListPersonas()
	if len(cs) < 2 || len(ps) != 1 {
		t.Fatalf("播种结果异常: %d 角色档 %d 档案", len(cs), len(ps))
	}
	// 二次播种应跳过。
	if err := SeedDemoData(s); err != nil {
		t.Fatal(err)
	}
	cs2, _ := s.ListCharacters()
	if len(cs2) != len(cs) {
		t.Fatalf("重复播种")
	}
}
