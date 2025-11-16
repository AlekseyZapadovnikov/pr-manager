package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/AlekseyZapadovnikov/pr-manager/internal/domain"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/models"
)

// SavePullRequest сохраняет или обновляет Pull Request и закреплённых ревьюеров в одной транзакции.
func (s *Storage) SavePullRequest(ctx context.Context, pr *models.PullRequest) (err error) {
	if pr == nil {
		return fmt.Errorf("pr is nil")
	}

	// РџСЂРѕСЃС‚Р°СЏ РІР°Р»РёРґР°С†РёСЏ: РЅРµ Р±РѕР»РµРµ 2 СЂРµРІСЊСЋРµСЂРѕРІ
	if len(pr.AssignedReviewers) > 2 {
		return fmt.Errorf("assigned reviewers count must be <= 2")
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

	const upsertPR = `
	INSERT INTO pull_requests (
		pull_request_id, pull_request_name, author_id, status, created_at, merged_at
	) VALUES ($1, $2, $3, $4, $5, $6)
	ON CONFLICT (pull_request_id) DO UPDATE
	SET pull_request_name = EXCLUDED.pull_request_name,
		author_id = EXCLUDED.author_id,
		status = EXCLUDED.status,
		created_at = EXCLUDED.created_at,
		merged_at = EXCLUDED.merged_at
`

	// РїРµСЂРµРґР°С‘Рј *time.Time вЂ” nil РєРѕСЂСЂРµРєС‚РЅРѕ РїСЂРµРІСЂР°С‰Р°РµС‚СЃСЏ РІ NULL
	_, err = tx.Exec(ctx, upsertPR,
		pr.PullRequestId,
		pr.PullRequestName,
		pr.AuthorId,
		string(pr.Status),
		pr.CreatedAt,
		pr.MergedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert pull_requests: %w", err)
	}

	// РћР±РЅРѕРІР»СЏРµРј СЂРµРІСЊСЋРµСЂРѕРІ: РїСЂРѕС‰Рµ СѓРґР°Р»РёС‚СЊ СЃС‚Р°СЂС‹Рµ Рё РІСЃС‚Р°РІРёС‚СЊ РЅРѕРІС‹Рµ
	const deleteReviewers = `DELETE FROM pull_request_reviewers WHERE pull_request_id = $1`
	if _, err := tx.Exec(ctx, deleteReviewers, pr.PullRequestId); err != nil {
		return fmt.Errorf("delete pull_request_reviewers: %w", err)
	}

	const insertReviewer = `INSERT INTO pull_request_reviewers (pull_request_id, user_id) VALUES ($1, $2)`
	// Р”РµРґСѓРїР»РёС†РёСЂСѓРµРј РЅР° РІСЃСЏРєРёР№ СЃР»СѓС‡Р°Р№
	seen := make(map[string]struct{}, len(pr.AssignedReviewers))
	for _, r := range pr.AssignedReviewers {
		if r == "" {
			continue // РїСЂРѕРїСѓСЃРєР°РµРј РїСѓСЃС‚С‹Рµ id
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		if _, err := tx.Exec(ctx, insertReviewer, pr.PullRequestId, r); err != nil {
			return fmt.Errorf("insert pull_request_reviewer (%s): %w", r, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	committed = true
	return nil
}

// GetPullRequest возвращает Pull Request по идентификатору вместе со списком ревьюеров.
func (s *Storage) GetPullRequest(ctx context.Context, prID string) (*models.PullRequest, error) {
	const qPR = `
	SELECT pull_request_id, pull_request_name, author_id, status, created_at, merged_at
	FROM pull_requests
	WHERE pull_request_id = $1
	`

	rows, err := s.pool.Query(ctx, qPR, prID)
	if err != nil {
		return nil, fmt.Errorf("query pull_requests: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		// РЅРµС‚ СЃС‚СЂРѕРєРё
		return nil, domain.NewNotFoundError(fmt.Sprintf("pull request %s", prID))
	}

	var (
		id      string
		name    string
		author  string
		status  string
		created *time.Time
		merged  *time.Time
	)
	if err := rows.Scan(&id, &name, &author, &status, &created, &merged); err != nil {
		return nil, fmt.Errorf("scan pull_requests: %w", err)
	}

	// РџРѕР»СѓС‡Р°РµРј СЂРµРІСЊСЋРµСЂРѕРІ (РјРѕР¶РµС‚ Р±С‹С‚СЊ 0)
	const qReviewers = `SELECT user_id FROM pull_request_reviewers WHERE pull_request_id = $1 ORDER BY user_id`
	rrows, err := s.pool.Query(ctx, qReviewers, prID)
	if err != nil {
		return nil, fmt.Errorf("query pull_request_reviewers: %w", err)
	}
	defer rrows.Close()

	var reviewers []string
	for rrows.Next() {
		var uid string
		if err := rrows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan reviewer: %w", err)
		}
		reviewers = append(reviewers, uid)
	}

	pr := &models.PullRequest{
		AssignedReviewers: reviewers,
		AuthorId:          author,
		CreatedAt:         created,
		MergedAt:          merged,
		PullRequestId:     id,
		PullRequestName:   name,
		Status:            models.PullRequestStatus(status),
	}
	return pr, nil
}

// FindPullRequestsByReviewer находит все PR, назначенные конкретному ревьюеру.
func (s *Storage) FindPullRequestsByReviewer(ctx context.Context, reviewerID string) ([]*models.PullRequest, error) {
	const q = `
SELECT 
    p.pull_request_id,
    p.pull_request_name,
    p.author_id,
    p.status,
    p.created_at,
    p.merged_at,
    COALESCE(array_agg(r.user_id ORDER BY r.user_id), ARRAY[]::text[]) AS reviewers
FROM pull_requests p
JOIN pull_request_reviewers r ON p.pull_request_id = r.pull_request_id
WHERE r.user_id = $1
GROUP BY p.pull_request_id, p.pull_request_name, p.author_id, p.status, p.created_at, p.merged_at
ORDER BY p.created_at DESC NULLS LAST
`

	rows, err := s.pool.Query(ctx, q, reviewerID)
	if err != nil {
		return nil, fmt.Errorf("query find by reviewer: %w", err)
	}
	defer rows.Close()

	var result []*models.PullRequest
	for rows.Next() {
		var (
			id        string
			name      string
			author    string
			status    string
			created   *time.Time
			merged    *time.Time
			reviewers []string
		)

		if err := rows.Scan(&id, &name, &author, &status, &created, &merged, &reviewers); err != nil {
			return nil, fmt.Errorf("scan find by reviewer: %w", err)
		}

		pr := &models.PullRequest{
			AssignedReviewers: reviewers,
			AuthorId:          author,
			CreatedAt:         created,
			MergedAt:          merged,
			PullRequestId:     id,
			PullRequestName:   name,
			Status:            models.PullRequestStatus(status),
		}
		result = append(result, pr)
	}

	// Р’РѕР·РјРѕР¶РЅРѕ, rows.Err() СЃРѕРґРµСЂР¶РёС‚ РѕС€РёР±РєСѓ
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return result, nil
}

// GetAssignmentStats рассчитывает агрегированную статистику распределения ревью.
func (s *Storage) GetAssignmentStats(ctx context.Context) (*models.AssignmentStats, error) {
	const qUsers = `
SELECT 
    r.user_id,
    COALESCE(u.username, ''),
    COUNT(*) AS assignments
FROM pull_request_reviewers r
LEFT JOIN users u ON u.user_id = r.user_id
GROUP BY r.user_id, u.username
ORDER BY assignments DESC, r.user_id
`

	userRows, err := s.pool.Query(ctx, qUsers)
	if err != nil {
		return nil, fmt.Errorf("query user assignment stats: %w", err)
	}
	defer userRows.Close()

	stats := &models.AssignmentStats{}
	for userRows.Next() {
		var (
			userID      string
			username    string
			assignments int64
		)
		if scanErr := userRows.Scan(&userID, &username, &assignments); scanErr != nil {
			return nil, fmt.Errorf("scan user assignment stats: %w", scanErr)
		}
		stats.ByUser = append(stats.ByUser, models.UserAssignmentStat{
			UserId:      userID,
			Username:    username,
			Assignments: int(assignments),
		})
	}
	if err = userRows.Err(); err != nil {
		return nil, fmt.Errorf("user assignment stats rows: %w", err)
	}

	const qPRs = `
SELECT 
    p.pull_request_id,
    p.pull_request_name,
    COUNT(r.user_id) AS reviewer_count
FROM pull_requests p
LEFT JOIN pull_request_reviewers r ON r.pull_request_id = p.pull_request_id
GROUP BY p.pull_request_id, p.pull_request_name
ORDER BY reviewer_count DESC, p.pull_request_id
`

	prRows, err := s.pool.Query(ctx, qPRs)
	if err != nil {
		return nil, fmt.Errorf("query pr assignment stats: %w", err)
	}
	defer prRows.Close()

	for prRows.Next() {
		var (
			prID          string
			prName        string
			reviewerCount int64
		)
		if err := prRows.Scan(&prID, &prName, &reviewerCount); err != nil {
			return nil, fmt.Errorf("scan pr assignment stats: %w", err)
		}
		stats.ByPullRequest = append(stats.ByPullRequest, models.PullRequestAssignmentStat{
			PullRequestId:   prID,
			PullRequestName: prName,
			ReviewerCount:   int(reviewerCount),
		})
	}
	if err := prRows.Err(); err != nil {
		return nil, fmt.Errorf("pr assignment stats rows: %w", err)
	}

	return stats, nil
}

// FindOpenPullRequestsByReviewers ищет открытые PR с участием любых указанных ревьюеров.
func (s *Storage) FindOpenPullRequestsByReviewers(ctx context.Context, reviewerIDs []string) ([]*models.PullRequest, error) {
	if len(reviewerIDs) == 0 {
		return nil, nil
	}

	unique := make(map[string]struct{}, len(reviewerIDs))
	for _, id := range reviewerIDs {
		if id == "" {
			continue
		}
		unique[id] = struct{}{}
	}
	if len(unique) == 0 {
		return nil, nil
	}

	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}

	const q = `
SELECT 
    p.pull_request_id,
    p.pull_request_name,
    p.author_id,
    p.status,
    p.created_at,
    p.merged_at,
    COALESCE(array_agg(r.user_id ORDER BY r.user_id), ARRAY[]::text[]) AS reviewers
FROM pull_requests p
JOIN pull_request_reviewers r ON r.pull_request_id = p.pull_request_id
WHERE p.status = 'OPEN'
  AND EXISTS (
        SELECT 1
        FROM pull_request_reviewers tr
        WHERE tr.pull_request_id = p.pull_request_id
          AND tr.user_id = ANY($1)
    )
GROUP BY p.pull_request_id, p.pull_request_name, p.author_id, p.status, p.created_at, p.merged_at
ORDER BY p.created_at DESC NULLS LAST
`

	rows, err := s.pool.Query(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("query open pull requests by reviewers: %w", err)
	}
	defer rows.Close()

	var prs []*models.PullRequest
	for rows.Next() {
		var (
			id        string
			name      string
			author    string
			status    string
			created   *time.Time
			merged    *time.Time
			reviewers []string
		)
		if err := rows.Scan(&id, &name, &author, &status, &created, &merged, &reviewers); err != nil {
			return nil, fmt.Errorf("scan open pull requests by reviewers: %w", err)
		}

		pr := &models.PullRequest{
			PullRequestId:     id,
			PullRequestName:   name,
			AuthorId:          author,
			Status:            models.PullRequestStatus(status),
			CreatedAt:         created,
			MergedAt:          merged,
			AssignedReviewers: reviewers,
		}
		prs = append(prs, pr)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows open pull requests by reviewers: %w", err)
	}

	return prs, nil
}

// ApplyBulkTeamReviewerSwaps в одной транзакции заменяет ревьюеров и деактивирует пользователей.
func (s *Storage) ApplyBulkTeamReviewerSwaps(ctx context.Context, swaps []models.ReviewerSwap, usersToDeactivate []string) (err error) {
	if len(swaps) == 0 && len(usersToDeactivate) == 0 {
		return nil
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

	if len(swaps) > 0 {
		if err := s.applyReviewerSwapsTx(ctx, tx, swaps); err != nil {
			return err
		}
	}

	if len(usersToDeactivate) > 0 {
		if err := s.deactivateUsersTx(ctx, tx, usersToDeactivate); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	committed = true
	return nil
}

// applyReviewerSwapsTx заменяет ревьюеров в рамках переданной транзакции.
func (s *Storage) applyReviewerSwapsTx(ctx context.Context, tx pgx.Tx, swaps []models.ReviewerSwap) error {
	const (
		deleteSQL = `DELETE FROM pull_request_reviewers WHERE pull_request_id = $1 AND user_id = $2`
		insertSQL = `INSERT INTO pull_request_reviewers (pull_request_id, user_id) VALUES ($1, $2)`
	)

	for _, swap := range swaps {
		if swap.PullRequestId == "" || swap.OldUserId == "" || swap.NewUserId == "" {
			return fmt.Errorf("invalid reviewer swap payload: %+v", swap)
		}
		if _, err := tx.Exec(ctx, deleteSQL, swap.PullRequestId, swap.OldUserId); err != nil {
			return fmt.Errorf("delete reviewer %s for pr %s: %w", swap.OldUserId, swap.PullRequestId, err)
		}
		if _, err := tx.Exec(ctx, insertSQL, swap.PullRequestId, swap.NewUserId); err != nil {
			return fmt.Errorf("insert reviewer %s for pr %s: %w", swap.NewUserId, swap.PullRequestId, err)
		}
	}
	return nil
}

// deactivateUsersTx помечает пользователей неактивными в рамках транзакции.
func (s *Storage) deactivateUsersTx(ctx context.Context, tx pgx.Tx, users []string) error {
	uniq := make(map[string]struct{}, len(users))
	for _, id := range users {
		if id == "" {
			continue
		}
		uniq[id] = struct{}{}
	}
	if len(uniq) == 0 {
		return nil
	}
	ids := make([]string, 0, len(uniq))
	for id := range uniq {
		ids = append(ids, id)
	}
	const updateSQL = `UPDATE users SET is_active = false WHERE user_id = ANY($1)`
	if _, err := tx.Exec(ctx, updateSQL, ids); err != nil {
		return fmt.Errorf("bulk deactivate users: %w", err)
	}
	return nil
}
