package service

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/AlekseyZapadovnikov/pr-manager/internal/domain"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/models"
)

const UserNumber = 200

type UserRepository interface {
	SaveUser(ctx context.Context, user *models.User) error
	GetUser(ctx context.Context, userID string) (*models.User, error)
	GetAllUsersInTeam(ctx context.Context, teamdId string) ([]*models.User, error)
}

type TeamRepository interface {
	SaveTeam(ctx context.Context, team *models.Team) error
	GetTeam(ctx context.Context, teamID string) (*models.Team, error)
}

type UserTeamRepository interface {
	UserRepository
	TeamRepository
	CreateTeamWithMembers(ctx context.Context, team *models.Team, users []models.User) error
}

type UserManager struct {
	repo  UserTeamRepository
	users map[string]*models.User
	mu    sync.RWMutex
}

// NewUserManager создаёт менеджер пользователей с кэшем в памяти.
func NewUserManager(repo UserTeamRepository) *UserManager {
	return &UserManager{
		repo:  repo,
		users: make(map[string]*models.User, UserNumber),
		mu:    sync.RWMutex{},
	}
}

// PrimeCacheUser загружает пользователя из репозитория для прогрева кэша.
func (um *UserManager) PrimeCacheUser(ctx context.Context, userID string) error {
	if um.repo == nil {
		return fmt.Errorf("repository is not configured")
	}

	user, err := um.repo.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user %s: %w", userID, err)
	}

	um.mu.Lock()
	defer um.mu.Unlock()
	um.users[user.UserId] = user
	return nil
}

// AssignRewiers выбирает до двух активных ревьюеров указанной команды.
func (um *UserManager) AssignRewiers(teamId string) []string {
	counter := 0
	ans := make([]string, 0, 2)
	um.mu.Lock()
	defer um.mu.Unlock()
	for _, user := range um.users {
		if user.TeamName == teamId && user.IsActive {
			ans = append(ans, user.UserId)
			counter++
		}
		if counter == 2 {
			break
		}
	}
	return ans
}

// SetActivity обновляет признак активности выбранных пользователей в кэше.
func (um *UserManager) SetActivity(rewIds []string, status bool) error {
	um.mu.Lock()
	defer um.mu.Unlock()
	for _, el := range rewIds {
		if user, exists := um.users[el]; exists {
			user.IsActive = status
		}
		// Если пользователя нет в кэше, пропускаем его.
	}
	return nil // потенциально, реализация может быть другой, поэтому возвращаем ошибку
}

// AddTeam сохраняет новую команду и пополняет кэш её участниками.
func (um *UserManager) AddTeam(ctx context.Context, team models.Team) error {
	// Проверяем участников на пустые и дублирующиеся идентификаторы.
	userIDs := make(map[string]bool)
	for i, m := range team.Members {
		if m.UserId == "" {
			return fmt.Errorf("empty user_id found in team members at index %d", i)
		}
		if userIDs[m.UserId] {
			return fmt.Errorf("duplicate user_id '%s' found in team members at index %d", m.UserId, i)
		}
		userIDs[m.UserId] = true
	}

	// Формируем список пользователей для атомарной операции.
	var users []models.User
	for _, m := range team.Members {
		user := models.ConvertTmToUser(m, team.TeamName)
		users = append(users, user)
	}

	// Сохраняем команду и пользователей атомарно (если есть репозиторий).
	if um.repo != nil {
		if err := um.repo.CreateTeamWithMembers(ctx, &team, users); err != nil {
			return fmt.Errorf("failed to persist team %s: %w", team.TeamName, err)
		}
	} else {
		// Если репозитория нет, ограничиваемся сообщением в лог.
		fmt.Printf("No repository configured, only updating cache for team %s\n", team.TeamName)
	}

	// Кэш обновляем только после успешной записи в базу.
	um.mu.Lock()
	defer um.mu.Unlock()

	fmt.Printf("Adding %d members to team %s cache\n", len(team.Members), team.TeamName)
	for _, user := range users {
	userCopy := user // фиксируем копию, чтобы карта указывала на отдельные структуры.
		fmt.Printf("Adding user %s (%s) to cache\n", userCopy.UserId, userCopy.Username)
		um.users[userCopy.UserId] = &userCopy
	}
	fmt.Printf("Total users in cache: %d\n", len(um.users))

	return nil
}

// GetTeam возвращает команду, используя репозиторий либо кэш.
func (um *UserManager) GetTeam(ctx context.Context, teamName string) (*models.Team, error) {
	// Сначала спрашиваем репозиторий — он источник истины.
	if um.repo != nil {
		team, err := um.repo.GetTeam(ctx, teamName)
		if err != nil {
			if !errors.Is(err, domain.ErrNotFound) {
				// Нестандартная ошибка, а не просто "не найдено".
				return nil, fmt.Errorf("failed to get team from repository: %w", err)
			}
			// Команды нет в репозитории — пробуем собрать из кэша.
		} else {
			// Нашли команду — возвращаем эталонные данные.
			return team, nil
		}
	}

	// План Б: собираем команду из кэша (менее надёжно, но лучше чем ничего).
	um.mu.RLock()
	defer um.mu.RUnlock()

	var members []models.TeamMember

	for _, user := range um.users {
		if user.TeamName == teamName {
			member := models.ConvertUserToTeamMember(*user)
			members = append(members, member)
		}
	}

	if len(members) == 0 {
		// Команду нигде не нашли.
		return nil, domain.NewNotFoundError("team")
	}

	fmt.Printf("Built team %s from cache with %d members\n", teamName, len(members))

	team := &models.Team{
		TeamName: teamName,
		Members:  members,
	}

	return team, nil
}

// GetUserTeam ищет команду пользователя и при необходимости подгружает данные из репозитория.
func (um *UserManager) GetUserTeam(userID string) (string, error) {
	um.mu.RLock()
	user, exists := um.users[userID]
	um.mu.RUnlock()
	if exists {
		return user.TeamName, nil
	}

	if um.repo == nil {
		return "", domain.NewNotFoundError("user")
	}

	ctx := context.Background()
	user, err := um.repo.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", domain.NewNotFoundError("user")
		}
		return "", fmt.Errorf("failed to get user from repository: %w", err)
	}

	um.mu.Lock()
	um.users[user.UserId] = user
	um.mu.Unlock()

	return user.TeamName, nil
}

// FindReplacementReviewer подбирает замену активному ревьюеру, исключая заданные ID.
func (um *UserManager) FindReplacementReviewer(teamName string, excludeUserIDs []string) (string, error) {
	um.mu.RLock()
	defer um.mu.RUnlock()

	// Создаем множество для быстрой проверки исключенных пользователей
	excludeSet := make(map[string]bool, len(excludeUserIDs))
	for _, id := range excludeUserIDs {
		excludeSet[id] = true
	}

	// Ищем активного пользователя из команды, которого нет в исключениях
	for _, user := range um.users {
		if user.TeamName == teamName &&
			user.IsActive &&
			!excludeSet[user.UserId] {
			return user.UserId, nil
		}
	}

	return "", domain.ErrNoCandidate
}

// SetUserActivity меняет активность пользователя и синхронизирует её с хранилищем.
func (um *UserManager) SetUserActivity(userID string, isActive bool) (*models.User, error) {
	um.mu.Lock()
	defer um.mu.Unlock()

	user, exists := um.users[userID]
	if !exists {
		return nil, domain.NewNotFoundError("user")
	}

	// Сохраняем исходное значение для возможного отката.
	originalStatus := user.IsActive
	user.IsActive = isActive

	// При наличии репозитория фиксируем изменение в базе.
	if um.repo != nil {
		ctx := context.Background()
		if err := um.repo.SaveUser(ctx, user); err != nil {
			// Откат при ошибке сохранения.
			user.IsActive = originalStatus
			return nil, fmt.Errorf("failed to save user: %w", err)
		}
	}

	return user, nil
}

// SyncUsersActivity массово обновляет флаг активности пользователей только в кэше.
func (um *UserManager) SyncUsersActivity(userIDs []string, isActive bool) {
	if len(userIDs) == 0 {
		return
	}

	um.mu.Lock()
	defer um.mu.Unlock()
	for _, id := range userIDs {
		if user, ok := um.users[id]; ok {
			user.IsActive = isActive
		}
	}
}
