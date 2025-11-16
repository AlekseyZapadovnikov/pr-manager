package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AlekseyZapadovnikov/pr-manager/internal/domain"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/models"
)

type PullRequestRepository interface {
	SavePullRequest(ctx context.Context, pr *models.PullRequest) error
	GetPullRequest(ctx context.Context, prID string) (*models.PullRequest, error)
	FindPullRequestsByReviewer(ctx context.Context, reviewerID string) ([]*models.PullRequest, error)
	GetAssignmentStats(ctx context.Context) (*models.AssignmentStats, error)
	FindOpenPullRequestsByReviewers(ctx context.Context, reviewerIDs []string) ([]*models.PullRequest, error)
	ApplyBulkTeamReviewerSwaps(ctx context.Context, swaps []models.ReviewerSwap, usersToDeactivate []string) error
}

type UserService interface {
	AssignRewiers(teamId string) []string // тут мб надо возвращать ошибку
	SetActivity(rew []string, status bool) error
	GetUserTeam(userID string) (string, error)                                        // Получить команду пользователя
	FindReplacementReviewer(teamName string, excludeUserIDs []string) (string, error) // Найти заменяющего ревьювера
	GetTeam(ctx context.Context, teamName string) (*models.Team, error)
	SyncUsersActivity(userIDs []string, status bool)
}

type PullRequestManager struct {
	repo        PullRequestRepository
	UserService UserService
}

// NewPullRequestService связывает менеджер с репозиторием PR и пользователями.
func (prm *PullRequestManager) NewPullRequestService(repo PullRequestRepository, user UserService) *PullRequestManager {
	return &PullRequestManager{
		repo:        repo,
		UserService: user,
	}
}

// CreatePullRequest формирует запись PR, назначает ревьюеров и сохраняет её.
func (prm *PullRequestManager) CreatePullRequest(ctx context.Context, reqData models.PostPullRequestCreateJSONBody) (*models.PullRequest, error) {
	pr := convertReqToModel(reqData)
	teamID, err := prm.UserService.GetUserTeam(pr.AuthorId)
	if err != nil {
		return nil, fmt.Errorf("failed to get author team: %w", err)
	}

	curAssignedReviewers := prm.UserService.AssignRewiers(teamID)
	pr.AssignedReviewers = curAssignedReviewers
	go prm.UserService.SetActivity(curAssignedReviewers, false)

	if err := prm.repo.SavePullRequest(ctx, pr); err != nil {
		return nil, fmt.Errorf("couldn`t add pr to DB")
	}
	return pr, nil
}

// Merge помечает PR как слитый и возвращает актуальное состояние.
func (prm *PullRequestManager) Merge(ctx context.Context, payload models.PostPullRequestMergeJSONBody) (*models.PullRequest, error) {
	// Получаем текущий PR.
	pr, err := prm.repo.GetPullRequest(ctx, payload.PullRequestId)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.NewNotFoundError("pull request")
		}
		return nil, fmt.Errorf("failed to get pull request: %w", err)
	}

	// Проверяем, не был ли PR уже замержен (для идемпотентности).
	if pr.Status == models.PullRequestStatusMERGED {
		return pr, nil
	}

	// Помечаем PR как merged.
	now := time.Now()
	pr.Status = models.PullRequestStatusMERGED
	pr.MergedAt = &now

	// Сохраняем обновлённую запись.
	if err := prm.repo.SavePullRequest(ctx, pr); err != nil {
		return nil, fmt.Errorf("failed to save merged pull request: %w", err)
	}

	// Возвращаем активность ревьюерам PR.
	if len(pr.AssignedReviewers) > 0 {
		go prm.UserService.SetActivity(pr.AssignedReviewers, true)
	}

	return pr, nil
}

// Reassign заменяет ревьюера PR и возвращает информацию о перестановке.
func (prm *PullRequestManager) Reassign(ctx context.Context, oldUserId, prId string) (*domain.ReassignResponse, error) {
	// Формируем payload в формате внутренних структур.
	payload := models.PostPullRequestReassignJSONBody{
		PullRequestId: prId,
		OldUserId:     oldUserId,
	}
	// Загружаем PR из хранилища.
	pr, err := prm.repo.GetPullRequest(ctx, payload.PullRequestId)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.NewNotFoundError("pull request")
		}
		return nil, fmt.Errorf("failed to get pull request: %w", err)
	}

	// Проверяем, не замержен ли PR.
	if pr.Status == models.PullRequestStatusMERGED {
		return nil, domain.NewPRMergedError(payload.PullRequestId)
	}

	// Убеждаемся, что старый ревьюер действительно назначен к PR.
	isAssigned := false
	for _, reviewerID := range pr.AssignedReviewers {
		if reviewerID == payload.OldUserId {
			isAssigned = true
			break
		}
	}
	if !isAssigned {
		return nil, domain.NewNotAssignedError(payload.PullRequestId)
	}

	// Узнаём команду старого ревьюера.
	teamName, err := prm.UserService.GetUserTeam(payload.OldUserId)
	if err != nil {
		return nil, fmt.Errorf("failed to get user team: %w", err)
	}

	// Ищем замену в этой же команде.
	// Исключаем текущих ревьюеров и ушедшего участника.
	excludeUserIDs := make([]string, 0, len(pr.AssignedReviewers)+1)
	excludeUserIDs = append(excludeUserIDs, pr.AssignedReviewers...)
	excludeUserIDs = append(excludeUserIDs, payload.OldUserId)

	newReviewerID, err := prm.UserService.FindReplacementReviewer(teamName, excludeUserIDs)
	if err != nil {
		if errors.Is(err, domain.ErrNoCandidate) {
			return nil, domain.NewNoCandidateError(payload.PullRequestId)
		}
		return nil, fmt.Errorf("failed to find replacement reviewer: %w", err)
	}

	// Заменяем ревьюера в списке назначенных.
	newAssignedReviewers := make([]string, 0, len(pr.AssignedReviewers))
	for _, reviewerID := range pr.AssignedReviewers {
		if reviewerID != payload.OldUserId {
			newAssignedReviewers = append(newAssignedReviewers, reviewerID)
		}
	}
	newAssignedReviewers = append(newAssignedReviewers, newReviewerID)

	pr.AssignedReviewers = newAssignedReviewers

	// Сохраняем обновлённый PR.
	if err := prm.repo.SavePullRequest(ctx, pr); err != nil {
		return nil, fmt.Errorf("failed to save reassigned pull request: %w", err)
	}

	// Обновляем активности: старого ревьюера включаем, нового выключаем.
	go prm.UserService.SetActivity([]string{payload.OldUserId}, true)
	go prm.UserService.SetActivity([]string{newReviewerID}, false)

	// Формируем ответ.
	response := &domain.ReassignResponse{
		PR:         pr,
		ReplacedBy: newReviewerID,
	}

	return response, nil
}

// BulkDeactivateTeamMembers деактивирует целевых пользователей и планирует замену ревьюеров.
func (prm *PullRequestManager) BulkDeactivateTeamMembers(ctx context.Context, teamName string, userIDs []string) (*models.TeamBulkDeactivateResult, error) {
	teamName = strings.TrimSpace(teamName)
	if teamName == "" {
		return nil, fmt.Errorf("team name is required")
	}
	if len(userIDs) == 0 {
		return nil, fmt.Errorf("no user ids provided")
	}

	targets, targetSet, err := normalizeTargetUserIDs(userIDs)
	if err != nil {
		return nil, err
	}

	team, err := prm.UserService.GetTeam(ctx, teamName)
	if err != nil {
		return nil, err
	}

	if err = ensureTargetsInTeam(targets, team.Members); err != nil {
		return nil, err
	}

	candidateIDs := collectReplacementCandidates(team.Members, targetSet)
	pool := newReviewerPool(candidateIDs)

	swaps, reassignments, replacementIDs, err := prm.planBulkReviewerSwaps(ctx, targets, targetSet, pool)
	if err != nil {
		return nil, err
	}

	usersToDeactivate := append(append(make([]string, 0, len(targets)+len(replacementIDs)), targets...), replacementIDs...)

	if err = prm.repo.ApplyBulkTeamReviewerSwaps(ctx, swaps, usersToDeactivate); err != nil {
		return nil, fmt.Errorf("bulk reviewer swap: %w", err)
	}

	if len(usersToDeactivate) > 0 {
		prm.UserService.SyncUsersActivity(usersToDeactivate, false)
	}

	result := &models.TeamBulkDeactivateResult{
		TeamName:      teamName,
		Deactivated:   targets,
		Reassignments: reassignments,
	}
	return result, nil
}

// normalizeTargetUserIDs удаляет дубли и пустые значения из списка пользователей.
func normalizeTargetUserIDs(userIDs []string) ([]string, map[string]struct{}, error) {
	targetSet := make(map[string]struct{}, len(userIDs))
	targets := make([]string, 0, len(userIDs))
	for _, raw := range userIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, exists := targetSet[id]; exists {
			continue
		}
		targetSet[id] = struct{}{}
		targets = append(targets, id)
	}
	if len(targets) == 0 {
		return nil, nil, fmt.Errorf("no valid user ids provided")
	}
	return targets, targetSet, nil
}

// ensureTargetsInTeam убеждается, что все целевые пользователи входят в команду.
func ensureTargetsInTeam(targets []string, members []models.TeamMember) error {
	memberSet := make(map[string]struct{}, len(members))
	for _, member := range members {
		memberSet[member.UserId] = struct{}{}
	}
	for _, id := range targets {
		if _, ok := memberSet[id]; !ok {
			return domain.NewNotFoundError(fmt.Sprintf("user %s", id))
		}
	}
	return nil
}

// collectReplacementCandidates собирает активных участников команды вне списка деактивации.
func collectReplacementCandidates(members []models.TeamMember, targetSet map[string]struct{}) []string {
	candidateIDs := make([]string, 0, len(members))
	for _, member := range members {
		if _, targeted := targetSet[member.UserId]; targeted {
			continue
		}
		if member.IsActive {
			candidateIDs = append(candidateIDs, member.UserId)
		}
	}
	return candidateIDs
}

// planBulkReviewerSwaps строит список замен ревьюеров и пользователей для деактивации.
func (prm *PullRequestManager) planBulkReviewerSwaps(
	ctx context.Context,
	targets []string,
	targetSet map[string]struct{},
	pool *reviewerPool,
) ([]models.ReviewerSwap, []models.TeamPRReassignment, []string, error) {
	openPRs, err := prm.repo.FindOpenPullRequestsByReviewers(ctx, targets)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("find open pull requests: %w", err)
	}

	var (
		swaps          []models.ReviewerSwap
		reassignments  []models.TeamPRReassignment
		replacementIDs []string
	)

	for _, pr := range openPRs {
		assigned := make(map[string]struct{}, len(pr.AssignedReviewers))
		for _, reviewer := range pr.AssignedReviewers {
			assigned[reviewer] = struct{}{}
		}

		replacementsForPR := make([]models.ReviewerReplacement, 0, len(pr.AssignedReviewers))
		for _, reviewer := range pr.AssignedReviewers {
			if _, targeted := targetSet[reviewer]; !targeted {
				continue
			}
			newReviewer, ok := pool.take(assigned)
			if !ok {
				return nil, nil, nil, domain.NewNoCandidateError(pr.PullRequestId)
			}
			assigned[newReviewer] = struct{}{}
			swaps = append(swaps, models.ReviewerSwap{
				PullRequestId: pr.PullRequestId,
				OldUserId:     reviewer,
				NewUserId:     newReviewer,
			})
			replacementIDs = append(replacementIDs, newReviewer)
			replacementsForPR = append(replacementsForPR, models.ReviewerReplacement{
				OldUserId: reviewer,
				NewUserId: newReviewer,
			})
		}

		if len(replacementsForPR) > 0 {
			reassignments = append(reassignments, models.TeamPRReassignment{
				PullRequestId: pr.PullRequestId,
				Replacements:  replacementsForPR,
			})
		}
	}

	return swaps, reassignments, replacementIDs, nil
}

// ListForReviewer возвращает короткие карточки PR, где пользователь назначен ревьюером.
func (prm *PullRequestManager) ListForReviewer(ctx context.Context, userID string) ([]models.PullRequestShort, error) {
	// Получаем все PR, где пользователь числится ревьюером.
	prs, err := prm.repo.FindPullRequestsByReviewer(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to find pull requests for reviewer %s: %w", userID, err)
	}

	// Конвертируем записи в укороченный формат.
	result := make([]models.PullRequestShort, 0, len(prs))
	for _, pr := range prs {
		shortPR := models.PullRequestShort{
			AuthorId:        pr.AuthorId,
			PullRequestId:   pr.PullRequestId,
			PullRequestName: pr.PullRequestName,
			Status:          models.PullRequestShortStatus(pr.Status),
		}
		result = append(result, shortPR)
	}

	return result, nil
}

// AssignmentStats возвращает агрегированную статистику назначений ревьюеров.
func (prm *PullRequestManager) AssignmentStats(ctx context.Context) (*models.AssignmentStats, error) {
	stats, err := prm.repo.GetAssignmentStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get assignment stats: %w", err)
	}
	return stats, nil
}

// convertReqToModel преобразует входной payload в модель PullRequest.
func convertReqToModel(reqData models.PostPullRequestCreateJSONBody) *models.PullRequest {
	var createdAt = time.Now()
	return &models.PullRequest{
		AuthorId:        reqData.AuthorId,
		PullRequestId:   reqData.PullRequestId,
		PullRequestName: reqData.PullRequestName,
		Status:          models.PullRequestStatusOPEN,
		CreatedAt:       &createdAt,
	}
}

type reviewerPool struct {
	queue     []string
	available map[string]struct{}
}

// newReviewerPool создаёт пул доступных ревьюеров без дублей.
func newReviewerPool(ids []string) *reviewerPool {
	available := make(map[string]struct{}, len(ids))
	queue := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, exists := available[id]; exists {
			continue
		}
		available[id] = struct{}{}
		queue = append(queue, id)
	}
	return &reviewerPool{
		queue:     queue,
		available: available,
	}
}

// take выбирает следующего доступного ревьюера, избегая исключений.
func (p *reviewerPool) take(exclude map[string]struct{}) (string, bool) {
	if p == nil || len(p.available) == 0 {
		return "", false
	}
	attempts := len(p.queue)
	for attempts > 0 {
		attempts--
		if len(p.queue) == 0 {
			break
		}
		candidate := p.queue[0]
		p.queue = p.queue[1:]
		if _, ok := p.available[candidate]; !ok {
			continue
		}
		if exclude != nil {
			if _, conflict := exclude[candidate]; conflict {
				p.queue = append(p.queue, candidate)
				continue
			}
		}
		delete(p.available, candidate)
		return candidate, true
	}
	return "", false
}
