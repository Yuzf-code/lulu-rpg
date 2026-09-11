package store

import (
	"database/sql"
	"errors"
)

// 消息与回合的类型常量。
const (
	KindUser   = "user"   // 玩家输入
	KindStory  = "story"  // 剧情写手产出的一个分段
	KindSystem = "system" // 系统提示（如开场说明）

	StyleDirect    = "direct"    // 玩家输入：剧情方向指令
	StyleSay       = "say"       // 玩家输入：角色台词
	StyleNarration = "narration" // 剧情：旁白/场景
	StyleDialogue  = "dialogue"  // 剧情：角色台词
	StyleAction    = "action"    // 剧情：角色动作
	StyleThought   = "thought"   // 剧情：角色内心
)

// SpeakerType 表示分段归属。
const (
	SpeakerNarrator  = "narrator"
	SpeakerCharacter = "character"
	SpeakerNPC       = "npc"
)

// Message 是聊天记录中的一条：玩家输入、一个剧情分段，或系统消息。
type Message struct {
	ID          string `json:"id"`
	SessionID   string `json:"session_id"`
	Seq         int64  `json:"seq"`
	Turn        int64  `json:"turn"`
	Kind        string `json:"kind"`
	Style       string `json:"style"`
	SpeakerType string `json:"speaker_type"`
	CharacterID string `json:"character_id"`
	SpeakerName string `json:"speaker_name"`
	Content     string `json:"content"`
	CreatedAt   int64  `json:"created_at"`

	// Image 为该回合的配图（查询消息时按回合聚合填充，仅每个回合展示一次）。
	Image *SessionImage `json:"image,omitempty"`
}

// SessionImage 是一次回合配图。
type SessionImage struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Turn      int64  `json:"turn"`
	Path      string `json:"path"`
	Prompt    string `json:"prompt"`
	CreatedAt int64  `json:"created_at"`
}

const messageCols = `id, session_id, seq, turn, kind, style, speaker_type, character_id, speaker_name, content, created_at`

// AddMessages 在一个事务里按顺序写入一组消息，seq 自动递增。
func (s *Store) AddMessages(msgs []*Message) error {
	if len(msgs) == 0 {
		return nil
	}
	return s.tx(func(tx *sql.Tx) error {
		for _, m := range msgs {
			if m.ID == "" {
				m.ID = NewID("m")
			}
			m.CreatedAt = now()
			err := tx.QueryRow(`INSERT INTO messages
				(id, session_id, seq, turn, kind, style, speaker_type, character_id, speaker_name, content, created_at)
				VALUES (?,?,(SELECT COALESCE(MAX(seq),0)+1 FROM messages WHERE session_id=?),?,?,?,?,?,?,?,?)
				RETURNING seq`,
				m.ID, m.SessionID, m.SessionID, m.Turn, m.Kind, m.Style, m.SpeakerType, m.CharacterID, m.SpeakerName, m.Content, m.CreatedAt,
			).Scan(&m.Seq)
			if err != nil {
				return err
			}
		}
		// 触发会话 updated_at，保证列表排序把活跃会话排前面。
		_, err := tx.Exec(`UPDATE sessions SET updated_at=? WHERE id=?`, now(), msgs[0].SessionID)
		return err
	})
}

// AddImage 记录一张回合配图。
func (s *Store) AddImage(img *SessionImage) error {
	img.ID = NewID("img")
	img.CreatedAt = now()
	_, err := s.db.Exec(`INSERT INTO images (id, session_id, turn, path, prompt, created_at) VALUES (?,?,?,?,?,?)`,
		img.ID, img.SessionID, img.Turn, img.Path, img.Prompt, img.CreatedAt)
	return err
}

// ListImages 返回会话内全部配图（按回合升序）。
func (s *Store) ListImages(sessionID string) ([]*SessionImage, error) {
	rows, err := s.db.Query(`SELECT id, session_id, turn, path, prompt, created_at FROM images WHERE session_id=? ORDER BY turn`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SessionImage
	for rows.Next() {
		var img SessionImage
		if err := rows.Scan(&img.ID, &img.SessionID, &img.Turn, &img.Path, &img.Prompt, &img.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &img)
	}
	return out, rows.Err()
}

// ListMessages 返回会话消息（seq 升序）。beforeSeq>0 时取该序号之前的消息；
// limit>0 时取最新的 limit 条（仍按升序返回）；limit<=0 表示全量。
func (s *Store) ListMessages(sessionID string, beforeSeq int64, limit int) ([]*Message, error) {
	inner := `SELECT ` + messageCols + ` FROM messages WHERE session_id=?`
	args := []any{sessionID}
	if beforeSeq > 0 {
		inner += ` AND seq < ?`
		args = append(args, beforeSeq)
	}
	var q string
	if limit > 0 {
		// 先取最新的 limit 条，再反转为升序。
		q = `SELECT * FROM (` + inner + ` ORDER BY seq DESC LIMIT ?) ORDER BY seq`
		args = append(args, limit)
	} else {
		q = inner + ` ORDER BY seq`
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Message
	for rows.Next() {
		m := &Message{}
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &m.Turn, &m.Kind, &m.Style,
			&m.SpeakerType, &m.CharacterID, &m.SpeakerName, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachImages(sessionID, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) attachImages(sessionID string, msgs []*Message) error {
	if len(msgs) == 0 {
		return nil
	}
	images, err := s.ListImages(sessionID)
	if err != nil {
		return err
	}
	if len(images) == 0 {
		return nil
	}
	byTurn := make(map[int64]*SessionImage, len(images))
	for _, img := range images {
		byTurn[img.Turn] = img
	}
	// 每回合只把配图挂到该回合的第一条「剧情」消息上（没有剧情分段时
	// 退回首条消息），前端按回合取用。
	firstAny, firstStory := make(map[int64]*Message), make(map[int64]*Message)
	for _, m := range msgs {
		if _, ok := firstAny[m.Turn]; !ok {
			firstAny[m.Turn] = m
		}
		if m.Kind == KindStory {
			if _, ok := firstStory[m.Turn]; !ok {
				firstStory[m.Turn] = m
			}
		}
	}
	for turn, img := range byTurn {
		target := firstStory[turn]
		if target == nil {
			target = firstAny[turn]
		}
		if target != nil {
			target.Image = img
		}
	}
	return nil
}

// ListMessagesForSummary 返回 afterSeq 之后所有玩家/剧情消息（升序），
// 供滚动摘要使用；系统消息不参与摘要。
func (s *Store) ListMessagesForSummary(sessionID string, afterSeq int64) ([]*Message, error) {
	rows, err := s.db.Query(`SELECT `+messageCols+` FROM messages
		WHERE session_id=? AND seq>? AND kind IN (?,?) ORDER BY seq`,
		sessionID, afterSeq, KindUser, KindStory)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Message
	for rows.Next() {
		m := &Message{}
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &m.Turn, &m.Kind, &m.Style,
			&m.SpeakerType, &m.CharacterID, &m.SpeakerName, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountMessagesSince 返回 summarizedUpto 之后（不含）的消息数，用于触发摘要。
func (s *Store) CountMessagesSince(sessionID string, afterSeq int64) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id=? AND seq>? AND kind IN (?,?)`,
		sessionID, afterSeq, KindUser, KindStory).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n, err
}
