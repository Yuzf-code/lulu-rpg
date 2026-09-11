package store

import (
	"database/sql"
	"errors"
)

// Session 是一局游戏（一个对话实例）；派生私聊通过 ParentID 关联主线。
//
// Summary 是「本会话自身消息」的滚动摘要：主线会话里即主线剧情，
// 私聊会话里即私下剧情（供主线按角色召回）。InheritedSummary 是派生时
// 复制的主线摘要快照，供私聊自身构建上下文用。
type Session struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	Scenario          string `json:"scenario"`
	PersonaID         string `json:"persona_id"`
	ParentID          string `json:"parent_id"`
	AutoImage         bool   `json:"auto_image"`
	TurnSeq           int64  `json:"turn_seq"`
	Summary           string `json:"summary"`
	InheritedSummary  string `json:"inherited_summary"`
	SummarizedUptoSeq int64  `json:"summarized_upto_seq"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`

	// 关联数据（查询时填充）
	Persona    *Persona     `json:"persona,omitempty"`
	Characters []*Character `json:"characters"`
}

// SessionSummary 是会话列表里的轻量视图。
type SessionSummary struct {
	Session
	Preview      string `json:"preview"` // 最后一条消息摘录
	MessageCount int64  `json:"message_count"`
	ImageCount   int64  `json:"image_count"`
}

const sessionCols = `id, title, scenario, persona_id, parent_id, auto_image, turn_seq, summary, inherited_summary, summarized_upto_seq, created_at, updated_at`

func scanSession(r rowScanner) (*Session, error) {
	var s Session
	var personaID, parentID sql.NullString
	var autoImage int
	err := r.Scan(&s.ID, &s.Title, &s.Scenario, &personaID, &parentID, &autoImage,
		&s.TurnSeq, &s.Summary, &s.InheritedSummary, &s.SummarizedUptoSeq, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.PersonaID = nullStr(personaID)
	s.ParentID = nullStr(parentID)
	s.AutoImage = autoImage != 0
	return &s, nil
}

// CreateSession 创建会话并写入出场角色（ord 保持传入顺序）。
func (s *Store) CreateSession(sess *Session, characterIDs []string) error {
	sess.ID = NewID("s")
	sess.CreatedAt = now()
	sess.UpdatedAt = sess.CreatedAt
	return s.tx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO sessions (id, title, scenario, persona_id, parent_id, auto_image, turn_seq, summary, inherited_summary, summarized_upto_seq, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			sess.ID, sess.Title, sess.Scenario, nullIfEmpty(sess.PersonaID), nullIfEmpty(sess.ParentID),
			boolInt(sess.AutoImage), 0, sess.Summary, sess.InheritedSummary, sess.SummarizedUptoSeq, sess.CreatedAt, sess.UpdatedAt)
		if err != nil {
			return err
		}
		for i, cid := range characterIDs {
			if _, err := tx.Exec(`INSERT INTO session_characters (session_id, character_id, ord) VALUES (?,?,?)`,
				sess.ID, cid, i); err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateSession 更新标题 / 场景设定 / 自动配图开关。
func (s *Store) UpdateSession(sess *Session) error {
	sess.UpdatedAt = now()
	res, err := s.db.Exec(`UPDATE sessions SET title=?, scenario=?, auto_image=?, updated_at=? WHERE id=?`,
		sess.Title, sess.Scenario, boolInt(sess.AutoImage), sess.UpdatedAt, sess.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteSession(id string) error {
	res, err := s.db.Exec(`DELETE FROM sessions WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetSession 返回会话本体 + 出场角色（不填充 Persona）。
func (s *Store) GetSession(id string) (*Session, error) {
	row := s.db.QueryRow(`SELECT `+sessionCols+` FROM sessions WHERE id=?`, id)
	sess, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.fillSessionRefs(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) fillSessionRefs(sess *Session) error {
	rows, err := s.db.Query(`SELECT `+characterCols+` FROM session_characters sc
		JOIN characters c ON c.id = sc.character_id WHERE sc.session_id=? ORDER BY sc.ord`, sess.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	sess.Characters = nil
	for rows.Next() {
		c, err := scanCharacter(rows)
		if err != nil {
			return err
		}
		sess.Characters = append(sess.Characters, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if sess.PersonaID != "" {
		p, err := s.GetPersona(sess.PersonaID)
		if err == nil {
			sess.Persona = p
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return nil
}

func (s *Store) ListSessions() ([]*SessionSummary, error) {
	rows, err := s.db.Query(`SELECT ` + sessionCols + `,
			(SELECT content FROM messages m WHERE m.session_id = s_.id ORDER BY m.seq DESC LIMIT 1),
			(SELECT COUNT(*) FROM messages m WHERE m.session_id = s_.id),
			(SELECT COUNT(*) FROM images i WHERE i.session_id = s_.id)
		FROM sessions s_ ORDER BY s_.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SessionSummary
	for rows.Next() {
		var item SessionSummary
		var personaID, parentID sql.NullString
		var autoImage int
		var preview sql.NullString
		err := rows.Scan(&item.ID, &item.Title, &item.Scenario, &personaID, &parentID, &autoImage,
			&item.TurnSeq, &item.Summary, &item.InheritedSummary, &item.SummarizedUptoSeq, &item.CreatedAt, &item.UpdatedAt,
			&preview, &item.MessageCount, &item.ImageCount)
		if err != nil {
			return nil, err
		}
		item.PersonaID = nullStr(personaID)
		item.ParentID = nullStr(parentID)
		item.AutoImage = autoImage != 0
		item.Preview = nullStr(preview)
		out = append(out, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 填充每局的出场角色（仅取头像所需字段即可，这里复用完整查询，规模小无妨）
	for _, item := range out {
		sess, err := s.GetSession(item.ID)
		if err != nil {
			continue
		}
		item.Characters = sess.Characters
		item.Persona = sess.Persona
	}
	return out, nil
}

// NextTurn 原子地分配下一个回合号。
func (s *Store) NextTurn(sessionID string) (int64, error) {
	var turn int64
	err := s.db.QueryRow(
		`UPDATE sessions SET turn_seq = turn_seq + 1, updated_at=? WHERE id=? RETURNING turn_seq`,
		now(), sessionID).Scan(&turn)
	return turn, err
}

// ListChildren 返回某会话派生出的全部子会话（含出场角色）。
// 用于把私聊的「私下剧情摘要」按角色回流到主线提示词。
func (s *Store) ListChildren(parentID string) ([]*Session, error) {
	rows, err := s.db.Query(`SELECT `+sessionCols+` FROM sessions WHERE parent_id=? ORDER BY created_at`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, sess := range out {
		if err := s.fillSessionRefs(sess); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SaveSummary 乐观更新滚动摘要：仅当 summarized_upto_seq 仍为期望值时写入，
// 避免后台摘要与新一轮生成相互覆盖。
func (s *Store) SaveSummary(sessionID string, summary string, upto, expectUpto int64) error {
	_, err := s.db.Exec(`UPDATE sessions SET summary=?, summarized_upto_seq=? WHERE id=? AND summarized_upto_seq=?`,
		summary, upto, sessionID, expectUpto)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
