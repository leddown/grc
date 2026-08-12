package user

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	ErrNotFound       = errors.New("user not found")
	ErrDuplicateEmail = errors.New("email already exists")
	ErrInvalidInput   = errors.New("invalid input")
	ErrInvalidID      = errors.New("id must be a positive integer")
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ParseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrInvalidID
	}
	return id, nil
}

func (s *Service) Create(name, email string) (User, error) {
	if err := validateUserInput(name, email); err != nil {
		return User{}, err
	}

	u, err := s.repo.Create(name, email)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrDuplicateEmail
		}
		return User{}, err
	}
	return u, nil
}

func (s *Service) List() ([]User, error) {
	return s.repo.List()
}

func (s *Service) GetByID(id int64) (User, error) {
	u, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return u, nil
}

func (s *Service) Update(id int64, name, email string) (User, error) {
	if err := validateUserInput(name, email); err != nil {
		return User{}, err
	}

	u, err := s.repo.Update(id, name, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		if isUniqueViolation(err) {
			return User{}, ErrDuplicateEmail
		}
		return User{}, err
	}

	return u, nil
}

func (s *Service) Delete(id int64) error {
	err := s.repo.Delete(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func validateUserInput(name, email string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if strings.TrimSpace(email) == "" {
		return fmt.Errorf("%w: email is required", ErrInvalidInput)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}
