// Package store 基于 SQLite 提供全部实体的持久化。
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store 封装数据库连接与所有查询。
type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）SQLite 数据库并执行迁移。
func Open(path string) (*Store, error) {
	// modernc 驱动纯 Go 实现；WAL 提升并发读性能，busy_timeout 避免写锁冲突。
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite 单写多读，限制连接数避免并发写排队放大延迟。
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}
	return s, nil
}

// Close 关闭底层连接。
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// 轻量列迁移：为旧库补齐新增列。
	if err := s.ensureColumn("characters", "relationships", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if err := s.ensureColumn("sessions", "inherited_summary", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return s.ensureColumn("sessions", "style_id", "TEXT REFERENCES styles(id) ON DELETE SET NULL")
}

// ensureColumn 在表缺少指定列时以 ALTER TABLE 补上。
func (s *Store) ensureColumn(table, col, def string) error {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == col {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, col, def))
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS characters (
id TEXT PRIMARY KEY NOT NULL,
	name              TEXT NOT NULL,
	title             TEXT NOT NULL DEFAULT '',
	appearance        TEXT NOT NULL DEFAULT '',
	personality       TEXT NOT NULL DEFAULT '',
	background        TEXT NOT NULL DEFAULT '',
	greeting          TEXT NOT NULL DEFAULT '',
	example_dialogues TEXT NOT NULL DEFAULT '[]',
	tags              TEXT NOT NULL DEFAULT '[]',
	relationships     TEXT NOT NULL DEFAULT '[]',
	avatar_path       TEXT NOT NULL DEFAULT '',
	created_at        INTEGER NOT NULL,
	updated_at        INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS personas (
id TEXT PRIMARY KEY NOT NULL,
	name        TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	avatar_path TEXT NOT NULL DEFAULT '',
	is_default  INTEGER NOT NULL DEFAULT 0,
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
id TEXT PRIMARY KEY NOT NULL,
	title                TEXT NOT NULL,
	scenario             TEXT NOT NULL DEFAULT '',
	persona_id           TEXT REFERENCES personas(id) ON DELETE SET NULL,
	parent_id            TEXT REFERENCES sessions(id) ON DELETE SET NULL,
	auto_image           INTEGER NOT NULL DEFAULT 0,
	turn_seq             INTEGER NOT NULL DEFAULT 0,
	summary              TEXT NOT NULL DEFAULT '',
	inherited_summary    TEXT NOT NULL DEFAULT '',
	summarized_upto_seq  INTEGER NOT NULL DEFAULT 0,
	created_at           INTEGER NOT NULL,
	updated_at           INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS session_characters (
	session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	character_id TEXT NOT NULL REFERENCES characters(id),
	ord          INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (session_id, character_id)
);

CREATE TABLE IF NOT EXISTS messages (
id TEXT PRIMARY KEY NOT NULL,
	session_id    TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	seq           INTEGER NOT NULL,
	turn          INTEGER NOT NULL DEFAULT 0,
	kind          TEXT NOT NULL,             -- user | story | system
	style         TEXT NOT NULL DEFAULT '',  -- user: say|direct  story: narration|dialogue|action|thought
	speaker_type  TEXT NOT NULL DEFAULT '',  -- narrator | character | npc
	character_id  TEXT NOT NULL DEFAULT '',
	speaker_name  TEXT NOT NULL DEFAULT '',
	content       TEXT NOT NULL,
	created_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, seq);

CREATE TABLE IF NOT EXISTS images (
id TEXT PRIMARY KEY NOT NULL,
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	turn       INTEGER NOT NULL DEFAULT 0,
	path       TEXT NOT NULL,
	prompt     TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_images_session ON images(session_id, turn);

CREATE TABLE IF NOT EXISTS styles (
	id          TEXT PRIMARY KEY NOT NULL,
	name        TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL
);
`

// ---- 通用小工具 ----

func now() int64 { return time.Now().Unix() }

// NewID 生成带前缀的随机 ID。
func NewID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// 随机源失效属于极端情况，退化为时间戳保证可用性。
		return fmt.Sprintf("%s_%x", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(b)
}

func nullStr(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}
