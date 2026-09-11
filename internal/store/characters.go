package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// Character 是一张角色卡：设定信息 + 头像。
type Character struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Title            string          `json:"title"`
	Appearance       string          `json:"appearance"`        // 外貌描述，用于写作与生图
	Personality      string          `json:"personality"`       // 性格
	Background       string          `json:"background"`        // 背景故事
	Greeting         string          `json:"greeting"`          // 可选开场白
	ExampleDialogues json.RawMessage `json:"example_dialogues"` // [{user, char}]
	Tags             json.RawMessage `json:"tags"`
	Relationships    json.RawMessage `json:"relationships"` // [{subject, text}] 对特定主体的态度/关系
	AvatarPath       string          `json:"avatar_path"`
	CreatedAt        int64           `json:"created_at"`
	UpdatedAt        int64           `json:"updated_at"`
}

// Relationship 描述角色对某个主体的态度或关系（主体通常是玩家档案或另一角色）。
type Relationship struct {
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

// ErrNotFound 表示目标实体不存在。
var ErrNotFound = errors.New("not found")

// ExampleDialogue 是一条示例问答，用于提示模型角色的说话方式。
type ExampleDialogue struct {
	User string `json:"user"`
	Char string `json:"char"`
}

func (s *Store) CreateCharacter(c *Character) error {
	c.ID = NewID("c")
	c.CreatedAt = now()
	c.UpdatedAt = c.CreatedAt
	_, err := s.db.Exec(`INSERT INTO characters
		(id, name, title, appearance, personality, background, greeting, example_dialogues, tags, relationships, avatar_path, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Name, c.Title, c.Appearance, c.Personality, c.Background, c.Greeting,
		string(c.ExampleDialogues), string(c.Tags), string(c.Relationships), c.AvatarPath, c.CreatedAt, c.UpdatedAt)
	return err
}

func (s *Store) UpdateCharacter(c *Character) error {
	c.UpdatedAt = now()
	res, err := s.db.Exec(`UPDATE characters SET
		name=?, title=?, appearance=?, personality=?, background=?, greeting=?, example_dialogues=?, tags=?, relationships=?, avatar_path=?, updated_at=?
		WHERE id=?`,
		c.Name, c.Title, c.Appearance, c.Personality, c.Background, c.Greeting,
		string(c.ExampleDialogues), string(c.Tags), string(c.Relationships), c.AvatarPath, c.UpdatedAt, c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCharacter 删除角色卡（历史消息保留 speaker_name，因此旧剧情仍可渲染）。
func (s *Store) DeleteCharacter(id string) error {
	res, err := s.db.Exec(`DELETE FROM characters WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetCharacter(id string) (*Character, error) {
	row := s.db.QueryRow(`SELECT `+characterCols+` FROM characters WHERE id=?`, id)
	c, err := scanCharacter(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

func (s *Store) ListCharacters() ([]*Character, error) {
	rows, err := s.db.Query(`SELECT ` + characterCols + ` FROM characters ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Character
	for rows.Next() {
		c, err := scanCharacter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CharactersByIDs 按给定顺序返回角色卡。
func (s *Store) CharactersByIDs(ids []string) ([]*Character, error) {
	out := make([]*Character, 0, len(ids))
	for _, id := range ids {
		c, err := s.GetCharacter(id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

const characterCols = `id, name, title, appearance, personality, background, greeting, example_dialogues, tags, relationships, avatar_path, created_at, updated_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanCharacter(r rowScanner) (*Character, error) {
	var c Character
	var example, tags, rels string
	err := r.Scan(&c.ID, &c.Name, &c.Title, &c.Appearance, &c.Personality, &c.Background,
		&c.Greeting, &example, &tags, &rels, &c.AvatarPath, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.ExampleDialogues = json.RawMessage(fallbackJSON(example, "[]"))
	c.Tags = json.RawMessage(fallbackJSON(tags, "[]"))
	c.Relationships = json.RawMessage(fallbackJSON(rels, "[]"))
	return &c, nil
}

// Relationships 将角色的关系 JSON 解析为结构化切片（容错）。
func (c *Character) RelationshipList() []Relationship {
	var out []Relationship
	if len(c.Relationships) > 0 {
		_ = json.Unmarshal(c.Relationships, &out)
	}
	return out
}

func fallbackJSON(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
