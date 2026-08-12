package user

import (
	"database/sql"

	"carelockconsulting/internal/db"
)

type Repository interface {
	Create(name, email string) (User, error)
	List() ([]User, error)
	GetByID(id int64) (User, error)
	Update(id int64, name, email string) (User, error)
	Delete(id int64) error
}

type SQLiteRepository struct {
	db *db.Conn
}

func NewSQLiteRepository(conn *db.Conn) *SQLiteRepository {
	return &SQLiteRepository{db: conn}
}

func (r *SQLiteRepository) Create(name, email string) (User, error) {
	id, err := r.db.Insert(`INSERT INTO users(name, email) VALUES(?, ?)`, name, email)
	if err != nil {
		return User{}, err
	}

	return User{ID: id, Name: name, Email: email}, nil
}

func (r *SQLiteRepository) List() ([]User, error) {
	rows, err := r.db.Query(`SELECT id, name, email FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email); err != nil {
			return nil, err
		}
		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

func (r *SQLiteRepository) GetByID(id int64) (User, error) {
	var u User
	err := r.db.QueryRow(`SELECT id, name, email FROM users WHERE id = ?`, id).Scan(&u.ID, &u.Name, &u.Email)
	if err != nil {
		return User{}, err
	}
	return u, nil
}

func (r *SQLiteRepository) Update(id int64, name, email string) (User, error) {
	result, err := r.db.Exec(`UPDATE users SET name = ?, email = ? WHERE id = ?`, name, email, id)
	if err != nil {
		return User{}, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return User{}, err
	}
	if rowsAffected == 0 {
		return User{}, sql.ErrNoRows
	}

	return User{ID: id, Name: name, Email: email}, nil
}

func (r *SQLiteRepository) Delete(id int64) error {
	result, err := r.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}
