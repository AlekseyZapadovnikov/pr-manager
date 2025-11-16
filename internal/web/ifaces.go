package web

import (
	"context"

	"github.com/AlekseyZapadovnikov/pr-manager/internal/domain"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/models"
)

// PullRequestService описывает операции над Pull Request, которые нужны HTTP-слою.
type PullRequestService interface {
	CreatePullRequest(ctx context.Context, payload models.PostPullRequestCreateJSONBody) (*models.PullRequest, error)
	Merge(ctx context.Context, payload models.PostPullRequestMergeJSONBody) (*models.PullRequest, error)
	Reassign(ctx context.Context, oldUsId, prId string) (*domain.ReassignResponse, error)
	ListForReviewer(ctx context.Context, userID string) ([]models.PullRequestShort, error)
	AssignmentStats(ctx context.Context) (*models.AssignmentStats, error)
	BulkDeactivateTeamMembers(ctx context.Context, teamName string, userIDs []string) (*models.TeamBulkDeactivateResult, error)
}

// UserTeamService объединяет операции с командами и пользователями за одним интерфейсом.
type UserTeamService interface {
	TeamService
	SetUserActivity(userID string, isActive bool) (*models.User, error)
}

// TeamService описывает базовые операции управления командами.
type TeamService interface {
	AddTeam(ctx context.Context, team models.Team) error
	GetTeam(ctx context.Context, teamName string) (*models.Team, error)
}
