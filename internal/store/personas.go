package store

import (
	"database/sql"
	"errors"
)

// Persona 是用户为自己设定的角色档案，在每局游戏中代表玩家本人。
type Persona struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	AvatarPath  string `json:"avatar_path"`
	IsDefault   bool   `json:"is_default"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

func (s *Store) CreatePersona(p *Persona) error {
	p.ID = NewID("p")
	p.CreatedAt = now()
	p.UpdatedAt = p.CreatedAt
	return s.tx(func(tx *sql.Tx) error {
		if p.IsDefault {
			if _, err := tx.Exec(`UPDATE personas SET is_default=0`); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`INSERT INTO personas (id, name, description, avatar_path, is_default, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?)`,
			p.ID, p.Name, p.Description, p.AvatarPath, boolInt(p.IsDefault), p.CreatedAt, p.UpdatedAt)
		return err
	})
}

func (s *Store) UpdatePersona(p *Persona) error {
	p.UpdatedAt = now()
	return s.tx(func(tx *sql.Tx) error {
		if p.IsDefault {
			if _, err := tx.Exec(`UPDATE personas SET is_default=0`); err != nil {
				return err
			}
		}
		res, err := tx.Exec(`UPDATE personas SET name=?, description=?, avatar_path=?, is_default=?, updated_at=? WHERE id=?`,
			p.Name, p.Description, p.AvatarPath, boolInt(p.IsDefault), p.UpdatedAt, p.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// SetDefaultPersona 将某个档案设为默认。
func (s *Store) SetDefaultPersona(id string) error {
	return s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE personas SET is_default=0`); err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE personas SET is_default=1 WHERE id=?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *Store) DeletePersona(id string) error {
	res, err := s.db.Exec(`DELETE FROM personas WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetPersona(id string) (*Persona, error) {
	row := s.db.QueryRow(`SELECT `+personaCols+` FROM personas WHERE id=?`, id)
	p, err := scanPersona(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// DefaultPersona 返回默认档案；若不存在则返回最近创建的一个（可为 nil）。
func (s *Store) DefaultPersona() (*Persona, error) {
	row := s.db.QueryRow(`SELECT ` + personaCols + ` FROM personas ORDER BY is_default DESC, created_at DESC LIMIT 1`)
	p, err := scanPersona(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (s *Store) ListPersonas() ([]*Persona, error) {
	rows, err := s.db.Query(`SELECT ` + personaCols + ` FROM personas ORDER BY is_default DESC, updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Persona
	for rows.Next() {
		p, err := scanPersona(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

const personaCols = `id, name, description, avatar_path, is_default, created_at, updated_at`

func scanPersona(r rowScanner) (*Persona, error) {
	var p Persona
	var def int
	if err := r.Scan(&p.ID, &p.Name, &p.Description, &p.AvatarPath, &def, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.IsDefault = def != 0
	return &p, nil
}

// tx 在事务中执行 fn，自动提交/回滚。
func (s *Store) tx(fn func(tx *sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
