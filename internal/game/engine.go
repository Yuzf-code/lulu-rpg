package game

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lulu-rpg/internal/config"
	"lulu-rpg/internal/imggen"
	"lulu-rpg/internal/llm"
	"lulu-rpg/internal/store"
)

// Engine 负责一次回合（或开场）的完整编排：拼提示词 → 流式生成 →
// 解析分段 → 落库 → 可选生图 → 滚动摘要。
type Engine struct {
	store *store.Store
	llm   llm.Provider
	img   imggen.Provider
	cfg   *config.Config
}

// New 创建引擎；img 可为 nil（未配置生图）。
func New(st *store.Store, lp llm.Provider, ip imggen.Provider, cfg *config.Config) *Engine {
	return &Engine{store: st, llm: lp, img: ip, cfg: cfg}
}

// ErrStopGeneration 表示下游（SSE 客户端）已断开，需要安静地中止生成。
var ErrStopGeneration = errors.New("stop generation")

// Event 是回合过程中推送给前端的事件，序列化后经 SSE 下发。
type Event struct {
	Type        string              `json:"type"` // user_saved|segment_start|delta|segment_end|image|image_failed|error|done
	SegmentIdx  int                 `json:"segment_idx,omitempty"`
	Kind        string              `json:"kind,omitempty"`
	SpeakerType string              `json:"speaker_type,omitempty"`
	CharacterID string              `json:"character_id,omitempty"`
	SpeakerName string              `json:"speaker_name,omitempty"`
	Text        string              `json:"text,omitempty"`
	Message     *store.Message      `json:"message,omitempty"`
	Turn        int64               `json:"turn,omitempty"`
	Image       *store.SessionImage `json:"image,omitempty"`
	MessageIDs  []string            `json:"message_ids,omitempty"`
	Error       string              `json:"error,omitempty"`
}

type emitFn func(Event) error

// RunOpening 生成开场。若唯一的出场角色写有开场白，则直接采用，
// 不调用模型（单角色卡的“见面语”体验更像传统角色卡应用）。
func (e *Engine) RunOpening(ctx context.Context, sess *store.Session, emit emitFn) error {
	turn, err := e.store.NextTurn(sess.ID)
	if err != nil {
		return err
	}
	chars := sess.Characters
	if len(chars) == 1 && chars[0].Greeting != "" {
		c := chars[0]
		msg := &store.Message{SessionID: sess.ID, Turn: turn, Kind: store.KindStory,
			Style: store.StyleDialogue, SpeakerType: store.SpeakerCharacter,
			CharacterID: c.ID, SpeakerName: c.Name, Content: c.Greeting}
		if err := e.emitSegment(emit, msg); err != nil {
			return err
		}
		if err := e.store.AddMessages([]*store.Message{msg}); err != nil {
			return err
		}
		return emit(Event{Type: "done", Turn: turn, MessageIDs: []string{msg.ID}})
	}
	return e.generate(ctx, sess, turn, nil, false, emit)
}

// RunTurn 执行一轮：落库玩家输入 → 生成剧情分段 → 落库 → 可选配图。
// content/mode 为玩家输入与模式（say|direct）；wantImage 由会话开关与请求共同决定。
func (e *Engine) RunTurn(ctx context.Context, sess *store.Session, content, mode string, wantImage bool, emit emitFn) error {
	turn, err := e.store.NextTurn(sess.ID)
	if err != nil {
		return err
	}
	// 先取历史（不含本轮输入），再落库输入。
	history, err := e.store.ListMessages(sess.ID, 0, e.historyWindow())
	if err != nil {
		return err
	}
	userMsg := &store.Message{SessionID: sess.ID, Turn: turn, Kind: store.KindUser,
		Style: mode, SpeakerName: personaName(sess.Persona), Content: content}
	if err := e.store.AddMessages([]*store.Message{userMsg}); err != nil {
		return err
	}
	if err := emit(Event{Type: "user_saved", Message: userMsg, Turn: turn}); err != nil {
		return ErrStopGeneration
	}
	return e.generate(ctx, sess, turn, userMsg, wantImage, emit, history...)
}

func (e *Engine) historyWindow() int {
	return e.cfg.Context.RecentMessages + e.cfg.Context.CompactThreshold + 16
}

// generate 是开场与回合共用的生成主流程。
func (e *Engine) generate(ctx context.Context, sess *store.Session, turn int64, userMsg *store.Message, wantImage bool, emit emitFn, history ...*store.Message) error {
	chars := sess.Characters
	persona := sess.Persona
	opening := userMsg == nil

	parser := NewParser(chars)
	req := llm.Request{
		Messages:    BuildChatMessages(sess.Scenario, persona, chars, sess.Summary, history, userMsg, e.cfg),
		Temperature: e.cfg.LLM.Temperature,
		MaxTokens:   e.cfg.LLM.MaxTokens,
		Mock: llm.MockHint{
			Opening:     opening,
			Turn:        turn,
			CharNames:   charNames(chars),
			PersonaName: personaName(persona),
		},
	}
	forward := func(evs []ParserEvent) error {
		for _, ev := range evs {
			if err := emit(Event{Type: ev.Type, SegmentIdx: ev.SegmentIdx, Kind: ev.Kind,
				SpeakerType: ev.SpeakerType, CharacterID: ev.CharacterID,
				SpeakerName: ev.SpeakerName, Text: ev.Text}); err != nil {
				return ErrStopGeneration
			}
		}
		return nil
	}

	_, err := e.llm.Stream(ctx, req, func(chunk string) error {
		return forward(parser.Feed(chunk))
	})
	aborted := errors.Is(err, ErrStopGeneration) || ctx.Err() != nil
	if err != nil && !aborted {
		// 模型侧错误：已完成的分段仍然保留，然后向客户端报告。
		_ = forward(parser.Finish())
		if perr := e.persistSegments(sess, turn, parser); perr != nil {
			log.Printf("保存剧情分段失败: %v", perr)
		}
		_ = emit(Event{Type: "error", Error: err.Error()})
		return err
	}
	_ = forward(parser.Finish())

	msgs := segmentMessages(sess, turn, parser.Segments())
	if len(msgs) > 0 {
		if err := e.store.AddMessages(msgs); err != nil {
			return err
		}
	}
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}

	if aborted {
		return nil
	}

	// 回合配图。
	if wantImage && e.img != nil {
		prompt := BuildImagePrompt(sess.Scenario, chars, parser.Segments(), e.cfg.Image.StyleSuffix)
		img, err := e.makeImage(ctx, sess, turn, prompt)
		if err != nil {
			log.Printf("回合配图失败: %v", err)
			if emitErr := emit(Event{Type: "image_failed", Turn: turn, Error: err.Error()}); emitErr != nil {
				return nil
			}
		} else if emitErr := emit(Event{Type: "image", Turn: turn, Image: img}); emitErr != nil {
			return nil
		}
	}

	if err := emit(Event{Type: "done", Turn: turn, MessageIDs: ids}); err != nil {
		return nil
	}
	// 摘要压缩在后台执行，不阻塞回合结束。
	go e.maybeCompact(sess.ID)
	return nil
}

// persistSegments 在出错路径上尽量保住已完成的分段。
func (e *Engine) persistSegments(sess *store.Session, turn int64, p *Parser) error {
	msgs := segmentMessages(sess, turn, p.Segments())
	return e.store.AddMessages(msgs)
}

func segmentMessages(sess *store.Session, turn int64, segs []*Segment) []*store.Message {
	var out []*store.Message
	for _, s := range segs {
		text := s.Text()
		if text == "" {
			continue
		}
		out = append(out, &store.Message{
			SessionID: sess.ID, Turn: turn, Kind: store.KindStory,
			Style: s.Kind, SpeakerType: s.SpeakerType,
			CharacterID: s.CharacterID, SpeakerName: s.SpeakerName, Content: text,
		})
	}
	return out
}

// emitSegment 把一条现成消息按分段事件推送（用于卡牌开场白）。
func (e *Engine) emitSegment(emit emitFn, m *store.Message) error {
	idx := 0
	if err := emit(Event{Type: "segment_start", SegmentIdx: idx, Kind: m.Style,
		SpeakerType: m.SpeakerType, CharacterID: m.CharacterID, SpeakerName: m.SpeakerName}); err != nil {
		return ErrStopGeneration
	}
	if err := emit(Event{Type: "delta", SegmentIdx: idx, Text: m.Content}); err != nil {
		return ErrStopGeneration
	}
	return emit(Event{Type: "segment_end", SegmentIdx: idx})
}

func (e *Engine) makeImage(ctx context.Context, sess *store.Session, turn int64, prompt string) (*store.SessionImage, error) {
	data, err := e.img.Generate(ctx, prompt)
	if err != nil {
		return nil, err
	}
	urlPath, err := saveFile(e.cfg.DataDir, "images", turn, data)
	if err != nil {
		return nil, err
	}
	img := &store.SessionImage{SessionID: sess.ID, Turn: turn, Path: urlPath, Prompt: prompt}
	if err := e.store.AddImage(img); err != nil {
		return nil, err
	}
	return img, nil
}

// saveFile 把生成的 PNG 写入 <DataDir>/files/<kind>/，返回可访问的 URL 路径。
func saveFile(dataDir, kind string, turn int64, data []byte) (string, error) {
	dir := filepath.Join(dataDir, "files", kind)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("t%d_%d.png", turn, time.Now().UnixNano())
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return "", err
	}
	return "/files/" + kind + "/" + name, nil
}

// GenerateAvatar 为角色卡/档案生成头像，返回 URL 路径。
func (e *Engine) GenerateAvatar(ctx context.Context, name, appearance string) (string, error) {
	if e.img == nil {
		return "", errors.New("未配置图像服务（IMG_MODEL）")
	}
	prompt := fmt.Sprintf("单人角色立绘头像，%s，%s，半身像，柔和打光，%s",
		name, clip(appearance, 200), e.cfg.Image.StyleSuffix)
	data, err := e.img.Generate(ctx, prompt)
	if err != nil {
		return "", err
	}
	return saveFile(e.cfg.DataDir, "avatars", 0, data)
}

// GenerateImage 直接按提示词生成一张图（供创作页试笔）。返回 PNG 数据。
func (e *Engine) GenerateImage(ctx context.Context, prompt string) ([]byte, error) {
	if e.img == nil {
		return nil, errors.New("未配置图像服务（IMG_MODEL）")
	}
	return e.img.Generate(ctx, prompt)
}

// maybeCompact 在未摘要的剧情超出阈值时，用模型生成滚动摘要。
func (e *Engine) maybeCompact(sessionID string) {
	sess, err := e.store.GetSession(sessionID)
	if err != nil {
		return
	}
	newer, err := e.store.ListMessagesForSummary(sessionID, sess.SummarizedUptoSeq)
	if err != nil || len(newer) <= e.cfg.Context.RecentMessages+e.cfg.Context.CompactThreshold {
		return
	}
	chunk := newer[:len(newer)-e.cfg.Context.RecentMessages]
	if len(chunk) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var sb strings.Builder
	if sess.Summary != "" {
		sb.WriteString("【已有摘要】\n" + clip(sess.Summary, e.cfg.Context.MaxFieldChars*2) + "\n\n")
	}
	sb.WriteString("【新增剧情】\n")
	for _, m := range chunk {
		if m.Kind == store.KindStory {
			sb.WriteString(taggedLine(m) + "\n")
			continue
		}
		if m.Kind == store.KindUser {
			sb.WriteString("【玩家】" + clip(m.Content, 200) + "\n")
		}
	}
	sys := "你是剧情记录员。请把剧情片段合并压缩为一份连贯的剧情备忘，供后续写作时保持前后一致。\n" +
		"必须保留：关键事件与因果、人物关系与相互称呼、承诺与伏笔、重要物品、地点与时间线、立场变化。\n" +
		"用短句列表、按时间顺序，不要评论、不要发挥，600字以内。"
	req := llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: sys},
			{Role: llm.RoleUser, Content: sb.String()},
		},
		Temperature: 0.3,
		MaxTokens:   512,
		Mock:        llm.MockHint{Task: llm.TaskSummary, CharNames: charNames(sess.Characters)},
	}
	out, err := e.llm.Complete(ctx, req)
	if err != nil {
		log.Printf("剧情摘要失败: %v", err)
		return
	}
	if out = strings.TrimSpace(out); out == "" {
		return
	}
	upto := chunk[len(chunk)-1].Seq
	// 乐观更新：若期间有新摘要写入则放弃本次结果，避免互相覆盖。
	if err := e.store.SaveSummary(sessionID, out, upto, sess.SummarizedUptoSeq); err != nil {
		log.Printf("保存剧情摘要失败: %v", err)
	}
}

func charNames(chars []*store.Character) []string {
	names := make([]string, 0, len(chars))
	for _, c := range chars {
		names = append(names, c.Name)
	}
	return names
}
