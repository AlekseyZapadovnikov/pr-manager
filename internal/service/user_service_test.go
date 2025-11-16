package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AlekseyZapadovnikov/pr-manager/internal/domain"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/models"
)

type mockUserTeamRepository struct {
	saveUserFn              func(context.Context, *models.User) error
	getUserFn               func(context.Context, string) (*models.User, error)
	getAllUsersInTeamFn     func(context.Context, string) ([]*models.User, error)
	saveTeamFn              func(context.Context, *models.Team) error
	getTeamFn               func(context.Context, string) (*models.Team, error)
	createTeamWithMembersFn func(context.Context, *models.Team, []models.User) error
}

func (m *mockUserTeamRepository) SaveUser(ctx context.Context, user *models.User) error {
	if m == nil || m.saveUserFn == nil {
		return nil
	}
	return m.saveUserFn(ctx, user)
}

func (m *mockUserTeamRepository) GetUser(ctx context.Context, userID string) (*models.User, error) {
	if m == nil || m.getUserFn == nil {
		return nil, domain.NewNotFoundError("user")
	}
	return m.getUserFn(ctx, userID)
}

func (m *mockUserTeamRepository) GetAllUsersInTeam(ctx context.Context, teamID string) ([]*models.User, error) {
	if m == nil || m.getAllUsersInTeamFn == nil {
		return nil, nil
	}
	return m.getAllUsersInTeamFn(ctx, teamID)
}

func (m *mockUserTeamRepository) SaveTeam(ctx context.Context, team *models.Team) error {
	if m == nil || m.saveTeamFn == nil {
		return nil
	}
	return m.saveTeamFn(ctx, team)
}

func (m *mockUserTeamRepository) GetTeam(ctx context.Context, teamID string) (*models.Team, error) {
	if m == nil || m.getTeamFn == nil {
		return nil, domain.NewNotFoundError("team")
	}
	return m.getTeamFn(ctx, teamID)
}

func (m *mockUserTeamRepository) CreateTeamWithMembers(ctx context.Context, team *models.Team, users []models.User) error {
	if m == nil || m.createTeamWithMembersFn == nil {
		return nil
	}
	return m.createTeamWithMembersFn(ctx, team, users)
}

func TestUserManager_PrimeCacheUser(t *testing.T) {
	ctx := context.Background()
	expectedUser := &models.User{UserId: "user-1", TeamName: "alpha", Username: "alpha-1", IsActive: true}
	repo := &mockUserTeamRepository{
		getUserFn: func(context.Context, string) (*models.User, error) {
			return expectedUser, nil
		},
	}

	manager := NewUserManager(repo)
	if err := manager.PrimeCacheUser(ctx, expectedUser.UserId); err != nil {
		t.Fatalf("PrimeCacheUser returned unexpected error: %v", err)
	}

	if got := manager.users[expectedUser.UserId]; got != expectedUser {
		t.Fatalf("PrimeCacheUser did not cache repo user")
	}
}

func TestUserManager_PrimeCacheUserRequiresRepo(t *testing.T) {
	manager := NewUserManager(nil)
	err := manager.PrimeCacheUser(context.Background(), "unknown")
	if err == nil || !strings.Contains(err.Error(), "repository is not configured") {
		t.Fatalf("expected configuration error, got %v", err)
	}
}

func TestUserManager_AddTeamPersistsAndCaches(t *testing.T) {
	ctx := context.Background()
	team := models.Team{
		TeamName: "alpha",
		Members: []models.TeamMember{
			{UserId: "u1", Username: "user-one", IsActive: true},
			{UserId: "u2", Username: "user-two", IsActive: false},
		},
	}
	var persisted bool
	repo := &mockUserTeamRepository{
		createTeamWithMembersFn: func(c context.Context, gotTeam *models.Team, users []models.User) error {
			if gotTeam.TeamName != team.TeamName {
				t.Fatalf("CreateTeamWithMembers received wrong team: %v", gotTeam.TeamName)
			}
			if len(users) != len(team.Members) {
				t.Fatalf("expected %d users passed to repo, got %d", len(team.Members), len(users))
			}
			persisted = true
			return nil
		},
	}

	manager := NewUserManager(repo)
	if err := manager.AddTeam(ctx, team); err != nil {
		t.Fatalf("AddTeam returned unexpected error: %v", err)
	}

	if !persisted {
		t.Fatalf("expected repository CreateTeamWithMembers to be called")
	}

	for _, member := range team.Members {
		if _, ok := manager.users[member.UserId]; !ok {
			t.Fatalf("user %s was not cached", member.UserId)
		}
	}
}

func TestUserManager_AddTeamRejectsDuplicates(t *testing.T) {
	manager := NewUserManager(nil)
	team := models.Team{
		TeamName: "dups",
		Members: []models.TeamMember{
			{UserId: "dup", Username: "dup", IsActive: true},
			{UserId: "dup", Username: "dup2", IsActive: true},
		},
	}

	err := manager.AddTeam(context.Background(), team)
	if err == nil || !strings.Contains(err.Error(), "duplicate user_id") {
		t.Fatalf("expected duplicate user error, got %v", err)
	}
}

func TestUserManager_GetTeamPrefersRepository(t *testing.T) {
	expected := &models.Team{TeamName: "alpha"}
	repo := &mockUserTeamRepository{
		getTeamFn: func(context.Context, string) (*models.Team, error) {
			return expected, nil
		},
	}
	manager := NewUserManager(repo)

	got, err := manager.GetTeam(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("GetTeam returned unexpected error: %v", err)
	}
	if got != expected {
		t.Fatalf("GetTeam did not return repository data")
	}
}

func TestUserManager_GetTeamFallsBackToCache(t *testing.T) {
	repo := &mockUserTeamRepository{
		getTeamFn: func(context.Context, string) (*models.Team, error) {
			return nil, domain.NewNotFoundError("team")
		},
	}

	manager := NewUserManager(repo)
	manager.users["u1"] = &models.User{UserId: "u1", Username: "one", TeamName: "beta", IsActive: true}
	manager.users["u2"] = &models.User{UserId: "u2", Username: "two", TeamName: "other", IsActive: true}

	got, err := manager.GetTeam(context.Background(), "beta")
	if err != nil {
		t.Fatalf("GetTeam returned unexpected error: %v", err)
	}
	if got.TeamName != "beta" || len(got.Members) != 1 || got.Members[0].UserId != "u1" {
		t.Fatalf("GetTeam returned unexpected cache data: %+v", got)
	}
}

func TestUserManager_GetTeamNotFound(t *testing.T) {
	manager := NewUserManager(nil)
	_, err := manager.GetTeam(context.Background(), "ghost")
	if err == nil || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestUserManager_GetUserTeamPaths(t *testing.T) {
	t.Run("cached user", func(t *testing.T) {
		manager := NewUserManager(nil)
		manager.users["u1"] = &models.User{UserId: "u1", TeamName: "alpha"}

		team, err := manager.GetUserTeam("u1")
		if err != nil || team != "alpha" {
			t.Fatalf("expected cached team alpha, got %s (err=%v)", team, err)
		}
	})

	t.Run("loaded from repo", func(t *testing.T) {
		repo := &mockUserTeamRepository{
			getUserFn: func(context.Context, string) (*models.User, error) {
				return &models.User{UserId: "u2", TeamName: "beta"}, nil
			},
		}
		manager := NewUserManager(repo)

		team, err := manager.GetUserTeam("u2")
		if err != nil || team != "beta" {
			t.Fatalf("expected repo team beta, got %s (err=%v)", team, err)
		}
		if _, cached := manager.users["u2"]; !cached {
			t.Fatalf("user should be cached after repo lookup")
		}
	})

	t.Run("repo not configured", func(t *testing.T) {
		manager := NewUserManager(nil)
		if _, err := manager.GetUserTeam("missing"); err == nil || !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("expected not found when repo absent, got %v", err)
		}
	})

	t.Run("repo returns not found", func(t *testing.T) {
		repo := &mockUserTeamRepository{
			getUserFn: func(context.Context, string) (*models.User, error) {
				return nil, domain.NewNotFoundError("user")
			},
		}
		manager := NewUserManager(repo)
		if _, err := manager.GetUserTeam("ghost"); err == nil || !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("expected not found, got %v", err)
		}
	})
}

func TestUserManager_SetUserActivityPersistsAndRollsBack(t *testing.T) {
	user := &models.User{UserId: "u1", TeamName: "alpha", IsActive: true}
	saved := false
	repo := &mockUserTeamRepository{
		saveUserFn: func(c context.Context, got *models.User) error {
			if got.UserId != user.UserId {
				t.Fatalf("SaveUser called with wrong user %s", got.UserId)
			}
			if got.IsActive {
				t.Fatalf("expected SaveUser to persist inactive status")
			}
			saved = true
			return nil
		},
	}

	manager := NewUserManager(repo)
	manager.users[user.UserId] = user

	updated, err := manager.SetUserActivity(user.UserId, false)
	if err != nil {
		t.Fatalf("SetUserActivity returned unexpected error: %v", err)
	}
	if updated.IsActive || !saved {
		t.Fatalf("SetUserActivity did not persist change")
	}

	repoFail := &mockUserTeamRepository{
		saveUserFn: func(context.Context, *models.User) error {
			return errors.New("boom")
		},
	}
	managerFail := NewUserManager(repoFail)
	managerFail.users[user.UserId] = &models.User{UserId: user.UserId, TeamName: "alpha", IsActive: true}

	if _, err := managerFail.SetUserActivity(user.UserId, false); err == nil {
		t.Fatalf("expected error from repo failure")
	}
	if !managerFail.users[user.UserId].IsActive {
		t.Fatalf("user state should roll back on repo failure")
	}
}

func TestUserManager_FindReplacementReviewer(t *testing.T) {
	manager := NewUserManager(nil)
	manager.users["u1"] = &models.User{UserId: "u1", TeamName: "alpha", IsActive: true}
	manager.users["u2"] = &models.User{UserId: "u2", TeamName: "alpha", IsActive: false}
	manager.users["u3"] = &models.User{UserId: "u3", TeamName: "beta", IsActive: true}

	repl, err := manager.FindReplacementReviewer("alpha", []string{"u2"})
	if err != nil || repl != "u1" {
		t.Fatalf("expected u1 replacement, got %s (err=%v)", repl, err)
	}

	if _, err := manager.FindReplacementReviewer("alpha", []string{"u1", "u2"}); !errors.Is(err, domain.ErrNoCandidate) {
		t.Fatalf("expected no candidate error, got %v", err)
	}
}

func TestUserManager_AssignRewiersLimitsTwo(t *testing.T) {
	manager := NewUserManager(nil)
	manager.users["u1"] = &models.User{UserId: "u1", TeamName: "alpha", IsActive: true}
	manager.users["u2"] = &models.User{UserId: "u2", TeamName: "alpha", IsActive: true}
	manager.users["u3"] = &models.User{UserId: "u3", TeamName: "alpha", IsActive: true}
	manager.users["u4"] = &models.User{UserId: "u4", TeamName: "alpha", IsActive: false}
	manager.users["u5"] = &models.User{UserId: "u5", TeamName: "beta", IsActive: true}

	reviewers := manager.AssignRewiers("alpha")
	if len(reviewers) != 2 {
		t.Fatalf("expected exactly 2 reviewers, got %v", reviewers)
	}
	for _, id := range reviewers {
		user := manager.users[id]
		if user == nil || user.TeamName != "alpha" || !user.IsActive {
			t.Fatalf("AssignRewiers returned invalid reviewer %s", id)
		}
	}
}

func TestUserManager_SetActivityUpdatesStatuses(t *testing.T) {
	manager := NewUserManager(nil)
	manager.users["u1"] = &models.User{UserId: "u1", TeamName: "alpha", IsActive: true}

	if err := manager.SetActivity([]string{"u1", "missing"}, false); err != nil {
		t.Fatalf("SetActivity returned unexpected error: %v", err)
	}

	if manager.users["u1"].IsActive {
		t.Fatalf("SetActivity did not update cached user")
	}
}

func TestUserManager_PrimeCacheRepoError(t *testing.T) {
	repo := &mockUserTeamRepository{
		getUserFn: func(context.Context, string) (*models.User, error) {
			return nil, errors.New("db down")
		},
	}
	manager := NewUserManager(repo)
	if err := manager.PrimeCacheUser(context.Background(), "u1"); err == nil {
		t.Fatalf("expected error when repo get user fails")
	}
}
