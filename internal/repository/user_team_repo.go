package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/AlekseyZapadovnikov/pr-manager/internal/domain"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/models"
)

// SaveUser выполняет upsert пользователя в таблицу users.
func (s *Storage) SaveUser(ctx context.Context, user *models.User) error {
	if user == nil {
		return fmt.Errorf("user is nil")
	}

	const upsertUser = `
	INSERT INTO users (user_id, username, is_active, team_name)
	VALUES ($1, $2, $3, NULLIF($4, ''))
	ON CONFLICT (user_id) DO UPDATE
	SET username = EXCLUDED.username,
		is_active = EXCLUDED.is_active,
		team_name = EXCLUDED.team_name
	`
	// Р•СЃР»Рё user.TeamName == "" - РїРµСЂРµРґР°С‘Рј РїСѓСЃС‚СѓСЋ СЃС‚СЂРѕРє, Р° РІ Р·Р°РїСЂРѕСЃРµ NULLIF('', '') -> NULL
	_, err := s.pool.Exec(ctx, upsertUser, user.UserId, user.Username, user.IsActive, user.TeamName)
	if err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	return nil
}

// GetUser возвращает пользователя по идентификатору.
func (s *Storage) GetUser(ctx context.Context, userID string) (*models.User, error) {
	const q = `
	SELECT user_id, username, is_active, team_name
	FROM users
	WHERE user_id = $1
	`
	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("query GetUser: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, domain.NewNotFoundError(fmt.Sprintf("user %s", userID))
	}

	var (
		id       string
		username string
		isActive bool
		teamName *string // может быть NULL
	)
	if err := rows.Scan(&id, &username, &isActive, &teamName); err != nil {
		return nil, fmt.Errorf("scan GetUser: %w", err)
	}

	tn := ""
	if teamName != nil {
		tn = *teamName
	}

	u := &models.User{
		IsActive: isActive,
		TeamName: tn,
		UserId:   id,
		Username: username,
	}
	return u, nil
}

// GetAllUsersInTeam достаёт всех пользователей указанной команды.
func (s *Storage) GetAllUsersInTeam(ctx context.Context, teamId string) ([]*models.User, error) {
	const q = `
SELECT user_id, username, is_active, team_name
FROM users
WHERE team_name = $1
ORDER BY username
`
	rows, err := s.pool.Query(ctx, q, teamId)
	if err != nil {
		return nil, fmt.Errorf("query getAllUsersInTeam: %w", err)
	}
	defer rows.Close()

	var result []*models.User
	for rows.Next() {
		var (
			id       string
			username string
			isActive bool
			teamName *string
		)
		if err := rows.Scan(&id, &username, &isActive, &teamName); err != nil {
			return nil, fmt.Errorf("scan getAllUsersInTeam: %w", err)
		}
		tn := ""
		if teamName != nil {
			tn = *teamName
		}
		u := &models.User{
			IsActive: isActive,
			TeamName: tn,
			UserId:   id,
			Username: username,
		}
		result = append(result, u)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error getAllUsersInTeam: %w", err)
	}

	return result, nil
}

// SaveTeam создаёт команду, если записи ещё нет.
func (s *Storage) SaveTeam(ctx context.Context, team *models.Team) error {
	if team == nil {
		return fmt.Errorf("team is nil")
	}
	const upsertTeam = `
		INSERT INTO teams (team_name)
		VALUES ($1)
		ON CONFLICT (team_name) DO NOTHING
	`
	tag, err := s.pool.Exec(ctx, upsertTeam, team.TeamName)
	if err != nil {
		return fmt.Errorf("upsert team: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.NewTeamExistsError(team.TeamName)
	}
	return nil
}

// CreateTeamWithMembers сохраняет команду и всех участников в одной транзакции.
func (s *Storage) CreateTeamWithMembers(ctx context.Context, team *models.Team, users []models.User) (err error) {
	if team == nil {
		return fmt.Errorf("team is nil")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
				err = errors.Join(err, fmt.Errorf("rollback tx: %w", rollbackErr))
			}
		}
	}()

	const insertTeam = `
		INSERT INTO teams (team_name)
		VALUES ($1)
		ON CONFLICT (team_name) DO NOTHING
	`
	tag, err := tx.Exec(ctx, insertTeam, team.TeamName)
	if err != nil {
		return fmt.Errorf("insert team: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.NewTeamExistsError(team.TeamName)
	}

	const upsertUser = `
	INSERT INTO users (user_id, username, is_active, team_name)
	VALUES ($1, $2, $3, NULLIF($4, ''))
	ON CONFLICT (user_id) DO UPDATE
	SET username = EXCLUDED.username,
		is_active = EXCLUDED.is_active,
		team_name = EXCLUDED.team_name
	`
	for _, user := range users {
		userCopy := user
		if _, err := tx.Exec(ctx, upsertUser, userCopy.UserId, userCopy.Username, userCopy.IsActive, userCopy.TeamName); err != nil {
			return fmt.Errorf("upsert user %s: %w", userCopy.UserId, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	committed = true
	return nil
}

// GetTeam возвращает команду вместе с участниками.
func (s *Storage) GetTeam(ctx context.Context, teamID string) (*models.Team, error) {
	// РЎРЅР°С‡Р°Р»Р° РїСЂРѕРІРµСЂРёРј, С‡С‚Рѕ РєРѕРјР°РЅРґР° СЃСѓС‰РµСЃС‚РІСѓРµС‚
	const qTeam = `SELECT team_name FROM teams WHERE team_name = $1`
	rows, err := s.pool.Query(ctx, qTeam, teamID)
	if err != nil {
		return nil, fmt.Errorf("query GetTeam: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, domain.NewNotFoundError(fmt.Sprintf("team %s", teamID))
	}
	var tn string
	if err := rows.Scan(&tn); err != nil {
		return nil, fmt.Errorf("scan GetTeam: %w", err)
	}

	// РџРѕР»СѓС‡Р°РµРј РІСЃРµС… РїРѕР»СЊР·РѕРІР°С‚РµР»РµР№ РІ РєРѕРјР°РЅРґРµ
	users, err := s.GetAllUsersInTeam(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("get members for team %s: %w", teamID, err)
	}

	// РњСЌРїРїРёРј users -> TeamMember. РЎРј. РєРѕРјРјРµРЅС‚Р°СЂРёР№ РІС‹С€Рµ РїСЂРѕ СЃС‚СЂСѓРєС‚СѓСЂСѓ TeamMember.
	members := make([]models.TeamMember, 0, len(users))
	for _, u := range users {
		members = append(members, convertUserToTeamMember(u))
	}

	team := &models.Team{
		Members:  members,
		TeamName: tn,
	}
	return team, nil
}

// convertUserToTeamMember преобразует пользователя в представление участника команды.
func convertUserToTeamMember(u *models.User) models.TeamMember {
	return models.TeamMember{
		UserId:   u.UserId,
		Username: u.Username,
		IsActive: u.IsActive,
	}
}
