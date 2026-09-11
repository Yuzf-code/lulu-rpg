package store

import (
	"database/sql"
	"errors"
)

// WritingStyle 是一份「写作风格」指令：可由文本蒸馏得到，开局时选用，
// 约束剧情写手的叙述视角、句式节奏与氛围。
type WritingStyle struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"` // 写给写手的风格指令
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

func (s *Store) CreateStyle(st *WritingStyle) error {
	st.ID = NewID("sty")
	st.CreatedAt = now()
	st.UpdatedAt = st.CreatedAt
	_, err := s.db.Exec(`INSERT INTO styles (id, name, description, created_at, updated_at) VALUES (?,?,?,?,?)`,
		st.ID, st.Name, st.Description, st.CreatedAt, st.UpdatedAt)
	return err
}

func (s *Store) UpdateStyle(st *WritingStyle) error {
	st.UpdatedAt = now()
	res, err := s.db.Exec(`UPDATE styles SET name=?, description=?, updated_at=? WHERE id=?`,
		st.Name, st.Description, st.UpdatedAt, st.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteStyle(id string) error {
	res, err := s.db.Exec(`DELETE FROM styles WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetStyle(id string) (*WritingStyle, error) {
	row := s.db.QueryRow(`SELECT id, name, description, created_at, updated_at FROM styles WHERE id=?`, id)
	st, err := scanStyle(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return st, err
}

func (s *Store) ListStyles() ([]*WritingStyle, error) {
	rows, err := s.db.Query(`SELECT id, name, description, created_at, updated_at FROM styles ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*WritingStyle
	for rows.Next() {
		st, err := scanStyle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func scanStyle(r rowScanner) (*WritingStyle, error) {
	var st WritingStyle
	if err := r.Scan(&st.ID, &st.Name, &st.Description, &st.CreatedAt, &st.UpdatedAt); err != nil {
		return nil, err
	}
	return &st, nil
}
