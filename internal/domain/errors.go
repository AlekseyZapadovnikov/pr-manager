package domain

import (
	"errors"
	"fmt"
)

// Сентинельные ошибки домена, используемые сервисами, репозиториями и веб-слоем.
var (
	ErrTeamExists   = errors.New("TEAM_EXISTS")
	ErrPRExists     = errors.New("PR_EXISTS")
	ErrPRMerged     = errors.New("PR_MERGED")
	ErrNotAssigned  = errors.New("NOT_ASSIGNED")
	ErrNoCandidate  = errors.New("NO_CANDIDATE")
	ErrNotFound     = errors.New("NOT_FOUND")
	ErrUnauthorized = errors.New("UNAUTHORIZED")
	ErrTeamIsEmty   = errors.New("EMPTY_TEAM")
)

// NewTeamExistsError возвращает ошибку о том, что команда с таким названием уже существует.
func NewTeamExistsError(teamName string) error {
	return fmt.Errorf("%w: team %s already exists", ErrTeamExists, teamName)
}

// NewPRExistsError сигнализирует, что Pull Request с таким идентификатором уже сохранён.
func NewPRExistsError(prID string) error {
	return fmt.Errorf("%w: pull request %s already exists", ErrPRExists, prID)
}

// NewPRMergedError сообщает, что указанный Pull Request уже замержен.
func NewPRMergedError(prID string) error {
	return fmt.Errorf("%w: pull request %s is already merged", ErrPRMerged, prID)
}

// NewNotAssignedError используется, когда у Pull Request нет назначенного ревьюера.
func NewNotAssignedError(prID string) error {
	return fmt.Errorf("%w: pull request %s is not assigned to reviewer", ErrNotAssigned, prID)
}

// NewNoCandidateError сообщает, что не удалось найти доступного ревьюера для PR.
func NewNoCandidateError(prID string) error {
	return fmt.Errorf("%w: no candidate reviewer available for pull request %s", ErrNoCandidate, prID)
}

// NewNotFoundError возвращает ошибку отсутствия переданного ресурса.
func NewNotFoundError(resource string) error {
	return fmt.Errorf("%w: %s not found", ErrNotFound, resource)
}

// NewUnauthorizedError используется при попытке выполнить недоступное действие.
func NewUnauthorizedError(action string) error {
	return fmt.Errorf("%w: not authorized to %s", ErrUnauthorized, action)
}

// NewErrTeamIsEmty возвращает ошибку о пустой команде.
func NewErrTeamIsEmty(teamID string) error {
	return fmt.Errorf("team with id %s is emty", teamID)
}
