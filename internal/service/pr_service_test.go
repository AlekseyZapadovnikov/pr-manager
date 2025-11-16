package service

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AlekseyZapadovnikov/pr-manager/internal/domain"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/models"
)

const testTeamName = "team-1"

type mockPullRequestRepository struct {
	savePullRequestFn                func(context.Context, *models.PullRequest) error
	getPullRequestFn                 func(context.Context, string) (*models.PullRequest, error)
	findPullRequestsByReviewerFn     func(context.Context, string) ([]*models.PullRequest, error)
	getAssignmentStatsFn             func(context.Context) (*models.AssignmentStats, error)
	findOpenPullRequestsByReviewerFn func(context.Context, []string) ([]*models.PullRequest, error)
	applyBulkTeamReviewerSwapsFn     func(context.Context, []models.ReviewerSwap, []string) error
}

func (m *mockPullRequestRepository) SavePullRequest(ctx context.Context, pr *models.PullRequest) error {
	if m == nil || m.savePullRequestFn == nil {
		return nil
	}
	return m.savePullRequestFn(ctx, pr)
}

func (m *mockPullRequestRepository) GetPullRequest(ctx context.Context, prID string) (*models.PullRequest, error) {
	if m == nil || m.getPullRequestFn == nil {
		return nil, domain.NewNotFoundError("pull request")
	}
	return m.getPullRequestFn(ctx, prID)
}

func (m *mockPullRequestRepository) FindPullRequestsByReviewer(ctx context.Context, reviewerID string) ([]*models.PullRequest, error) {
	if m == nil || m.findPullRequestsByReviewerFn == nil {
		return nil, nil
	}
	return m.findPullRequestsByReviewerFn(ctx, reviewerID)
}

func (m *mockPullRequestRepository) GetAssignmentStats(ctx context.Context) (*models.AssignmentStats, error) {
	if m == nil || m.getAssignmentStatsFn == nil {
		return nil, nil
	}
	return m.getAssignmentStatsFn(ctx)
}

func (m *mockPullRequestRepository) FindOpenPullRequestsByReviewers(ctx context.Context, ids []string) ([]*models.PullRequest, error) {
	if m == nil || m.findOpenPullRequestsByReviewerFn == nil {
		return nil, nil
	}
	return m.findOpenPullRequestsByReviewerFn(ctx, ids)
}

func (m *mockPullRequestRepository) ApplyBulkTeamReviewerSwaps(ctx context.Context, swaps []models.ReviewerSwap, users []string) error {
	if m == nil || m.applyBulkTeamReviewerSwapsFn == nil {
		return nil
	}
	return m.applyBulkTeamReviewerSwapsFn(ctx, swaps, users)
}

type mockUserService struct {
	assignReviewersFn         func(string) []string
	setActivityFn             func([]string, bool) error
	getUserTeamFn             func(string) (string, error)
	findReplacementReviewerFn func(string, []string) (string, error)
	getTeamFn                 func(context.Context, string) (*models.Team, error)
	syncUsersActivityFn       func([]string, bool)
}

func (m *mockUserService) AssignRewiers(teamID string) []string {
	if m == nil || m.assignReviewersFn == nil {
		return nil
	}
	return m.assignReviewersFn(teamID)
}

func (m *mockUserService) SetActivity(ids []string, status bool) error {
	if m == nil || m.setActivityFn == nil {
		return nil
	}
	return m.setActivityFn(ids, status)
}

func (m *mockUserService) GetUserTeam(userID string) (string, error) {
	if m == nil || m.getUserTeamFn == nil {
		return "", domain.NewNotFoundError("user")
	}
	return m.getUserTeamFn(userID)
}

func (m *mockUserService) FindReplacementReviewer(teamName string, excludeUserIDs []string) (string, error) {
	if m == nil || m.findReplacementReviewerFn == nil {
		return "", domain.ErrNoCandidate
	}
	return m.findReplacementReviewerFn(teamName, excludeUserIDs)
}

func (m *mockUserService) GetTeam(ctx context.Context, teamName string) (*models.Team, error) {
	if m == nil || m.getTeamFn == nil {
		return nil, domain.NewNotFoundError("team")
	}
	return m.getTeamFn(ctx, teamName)
}

func (m *mockUserService) SyncUsersActivity(ids []string, status bool) {
	if m == nil || m.syncUsersActivityFn == nil {
		return
	}
	m.syncUsersActivityFn(ids, status)
}

func TestPullRequestManager_CreatePullRequestSuccess(t *testing.T) {
	ctx := context.Background()
	var persisted *models.PullRequest
	repo := &mockPullRequestRepository{
		savePullRequestFn: func(ctx context.Context, pr *models.PullRequest) error {
			persisted = pr
			return nil
		},
	}
	var wg sync.WaitGroup
	wg.Add(1)
	userSvc := &mockUserService{
		getUserTeamFn: func(userID string) (string, error) {
			if userID != "author-1" {
				t.Fatalf("unexpected author id %s", userID)
			}
			return testTeamName, nil
		},
		assignReviewersFn: func(teamID string) []string {
			if teamID != testTeamName {
				t.Fatalf("AssignRewiers called with wrong team %s", teamID)
			}
			return []string{"rev-1", "rev-2"}
		},
		setActivityFn: func(ids []string, status bool) error {
			defer wg.Done()
			if status {
				t.Fatalf(" reviewers should be marked inactive")
			}
			if len(ids) != 2 {
				t.Fatalf("expected two reviewers, got %v", ids)
			}
			return nil
		},
	}

	manager := &PullRequestManager{repo: repo, UserService: userSvc}
	req := models.PostPullRequestCreateJSONBody{
		AuthorId:        "author-1",
		PullRequestId:   "pr-1",
		PullRequestName: "My PR",
	}
	pr, err := manager.CreatePullRequest(ctx, req)
	if err != nil {
		t.Fatalf("CreatePullRequest returned unexpected error: %v", err)
	}
	if pr.Status != models.PullRequestStatusOPEN {
		t.Fatalf("expected PR created with OPEN status")
	}
	if pr.CreatedAt == nil {
		t.Fatalf("CreatePullRequest should set CreatedAt")
	}
	if len(pr.AssignedReviewers) != 2 {
		t.Fatalf("expected assigned reviewers propagated, got %v", pr.AssignedReviewers)
	}
	wg.Wait()
	if persisted != pr {
		t.Fatalf("CreatePullRequest did not save resulting PR")
	}
}

func TestPullRequestManager_CreatePullRequestErrors(t *testing.T) {
	t.Run("user service failure", func(t *testing.T) {
		userSvc := &mockUserService{
			getUserTeamFn: func(string) (string, error) {
				return "", errors.New("no team")
			},
		}
		manager := &PullRequestManager{repo: &mockPullRequestRepository{}, UserService: userSvc}
		_, err := manager.CreatePullRequest(context.Background(), models.PostPullRequestCreateJSONBody{AuthorId: "a"})
		if err == nil {
			t.Fatalf("expected error when user service fails")
		}
	})

	t.Run("repo save failure", func(t *testing.T) {
		repo := &mockPullRequestRepository{
			savePullRequestFn: func(context.Context, *models.PullRequest) error {
				return errors.New("db down")
			},
		}
		userSvc := &mockUserService{
			getUserTeamFn: func(string) (string, error) {
				return testTeamName, nil
			},
			assignReviewersFn: func(string) []string {
				return []string{"r1"}
			},
			setActivityFn: func([]string, bool) error {
				return nil
			},
		}
		manager := &PullRequestManager{repo: repo, UserService: userSvc}
		if _, err := manager.CreatePullRequest(context.Background(), models.PostPullRequestCreateJSONBody{AuthorId: "a"}); err == nil {
			t.Fatalf("expected repo save error")
		}
	})
}

func TestPullRequestManager_MergeSuccess(t *testing.T) {
	ctx := context.Background()
	var saved bool
	repo := &mockPullRequestRepository{
		getPullRequestFn: func(context.Context, string) (*models.PullRequest, error) {
			return &models.PullRequest{
				PullRequestId:     "pr-1",
				Status:            models.PullRequestStatusOPEN,
				AssignedReviewers: []string{"rev-1"},
			}, nil
		},
		savePullRequestFn: func(context.Context, *models.PullRequest) error {
			saved = true
			return nil
		},
	}
	var wg sync.WaitGroup
	wg.Add(1)
	userSvc := &mockUserService{
		setActivityFn: func(ids []string, status bool) error {
			defer wg.Done()
			if !status {
				t.Fatalf("reviewers should be marked active on merge")
			}
			if len(ids) != 1 || ids[0] != "rev-1" {
				t.Fatalf("unexpected reviewer ids %v", ids)
			}
			return nil
		},
	}

	manager := &PullRequestManager{repo: repo, UserService: userSvc}
	payload := models.PostPullRequestMergeJSONBody{PullRequestId: "pr-1"}
	pr, err := manager.Merge(ctx, payload)
	if err != nil {
		t.Fatalf("Merge returned unexpected error: %v", err)
	}
	if pr.Status != models.PullRequestStatusMERGED || pr.MergedAt == nil {
		t.Fatalf("Merge should mark PR as merged")
	}
	wg.Wait()
	if !saved {
		t.Fatalf("Merge should save merged PR")
	}
}

func TestPullRequestManager_MergeErrors(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		repo := &mockPullRequestRepository{
			getPullRequestFn: func(context.Context, string) (*models.PullRequest, error) {
				return nil, domain.NewNotFoundError("pull request")
			},
		}
		manager := &PullRequestManager{repo: repo, UserService: &mockUserService{}}
		_, err := manager.Merge(context.Background(), models.PostPullRequestMergeJSONBody{PullRequestId: "pr"})
		if err == nil || !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("expected not found error, got %v", err)
		}
	})

	t.Run("already merged", func(t *testing.T) {
		repo := &mockPullRequestRepository{
			getPullRequestFn: func(context.Context, string) (*models.PullRequest, error) {
				return &models.PullRequest{
					PullRequestId: "pr",
					Status:        models.PullRequestStatusMERGED,
				}, nil
			},
			savePullRequestFn: func(context.Context, *models.PullRequest) error {
				t.Fatalf("save should not be called for already merged PR")
				return nil
			},
		}
		manager := &PullRequestManager{repo: repo, UserService: &mockUserService{}}
		pr, err := manager.Merge(context.Background(), models.PostPullRequestMergeJSONBody{PullRequestId: "pr"})
		if err != nil {
			t.Fatalf("expected nil error for idempotent merge, got %v", err)
		}
		if pr.Status != models.PullRequestStatusMERGED {
			t.Fatalf("expected merged status")
		}
	})
}

func TestPullRequestManager_ReassignSuccess(t *testing.T) {
	ctx := context.Background()
	repo := &mockPullRequestRepository{
		getPullRequestFn: func(context.Context, string) (*models.PullRequest, error) {
			return &models.PullRequest{
				PullRequestId:     "pr-1",
				Status:            models.PullRequestStatusOPEN,
				AssignedReviewers: []string{"old", "keep"},
			}, nil
		},
		savePullRequestFn: func(context.Context, *models.PullRequest) error {
			return nil
		},
	}
	var wg sync.WaitGroup
	wg.Add(2)
	statusCalls := make(map[string]bool)
	var mu sync.Mutex
	userSvc := &mockUserService{
		getUserTeamFn: func(userID string) (string, error) {
			if userID != "old" {
				t.Fatalf("expected GetUserTeam called with old reviewer")
			}
			return testTeamName, nil
		},
		findReplacementReviewerFn: func(team string, exclude []string) (string, error) {
			if team != testTeamName {
				t.Fatalf("unexpected team %s", team)
			}
			contains := func(target string) bool {
				for _, el := range exclude {
					if el == target {
						return true
					}
				}
				return false
			}
			if !contains("old") || !contains("keep") {
				t.Fatalf("older reviewers should be excluded")
			}
			return "new", nil
		},
		setActivityFn: func(ids []string, status bool) error {
			defer wg.Done()
			if len(ids) != 1 {
				t.Fatalf("expected per-user activity update, got %v", ids)
			}
			mu.Lock()
			statusCalls[ids[0]] = status
			mu.Unlock()
			return nil
		},
		assignReviewersFn: func(string) []string { return nil },
	}

	manager := &PullRequestManager{repo: repo, UserService: userSvc}
	resp, err := manager.Reassign(ctx, "old", "pr-1")
	if err != nil {
		t.Fatalf("Reassign returned unexpected error: %v", err)
	}
	if resp.ReplacedBy != "new" {
		t.Fatalf("expected new reviewer, got %s", resp.ReplacedBy)
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if !statusCalls["old"] || statusCalls["new"] {
		t.Fatalf("expected old reviewer reactivated and new reviewer deactivated, got %v", statusCalls)
	}
}

func TestPullRequestManager_ReassignErrors(t *testing.T) {
	t.Run("old reviewer not assigned", func(t *testing.T) {
		repo := &mockPullRequestRepository{
			getPullRequestFn: func(context.Context, string) (*models.PullRequest, error) {
				return &models.PullRequest{
					PullRequestId:     "pr",
					Status:            models.PullRequestStatusOPEN,
					AssignedReviewers: []string{"other"},
				}, nil
			},
		}
		manager := &PullRequestManager{repo: repo, UserService: &mockUserService{}}
		_, err := manager.Reassign(context.Background(), "missing", "pr")
		if err == nil || !errors.Is(err, domain.ErrNotAssigned) {
			t.Fatalf("expected not assigned error, got %v", err)
		}
	})

	t.Run("pr merged", func(t *testing.T) {
		repo := &mockPullRequestRepository{
			getPullRequestFn: func(context.Context, string) (*models.PullRequest, error) {
				return &models.PullRequest{
					PullRequestId:     "pr",
					Status:            models.PullRequestStatusMERGED,
					AssignedReviewers: []string{"old"},
				}, nil
			},
		}
		manager := &PullRequestManager{repo: repo, UserService: &mockUserService{}}
		_, err := manager.Reassign(context.Background(), "old", "pr")
		if err == nil || !errors.Is(err, domain.ErrPRMerged) {
			t.Fatalf("expected merged error, got %v", err)
		}
	})

	t.Run("no replacement candidate", func(t *testing.T) {
		repo := &mockPullRequestRepository{
			getPullRequestFn: func(context.Context, string) (*models.PullRequest, error) {
				return &models.PullRequest{
					PullRequestId:     "pr",
					Status:            models.PullRequestStatusOPEN,
					AssignedReviewers: []string{"old"},
				}, nil
			},
		}
		userSvc := &mockUserService{
			getUserTeamFn: func(string) (string, error) {
				return testTeamName, nil
			},
			findReplacementReviewerFn: func(string, []string) (string, error) {
				return "", domain.ErrNoCandidate
			},
		}
		manager := &PullRequestManager{repo: repo, UserService: userSvc}
		_, err := manager.Reassign(context.Background(), "old", "pr")
		if err == nil || !errors.Is(err, domain.ErrNoCandidate) {
			t.Fatalf("expected no candidate error, got %v", err)
		}
	})
}

func TestPullRequestManager_AssignmentStats(t *testing.T) {
	want := &models.AssignmentStats{
		ByUser: []models.UserAssignmentStat{
			{UserId: "u1", Username: "Alice", Assignments: 2},
		},
		ByPullRequest: []models.PullRequestAssignmentStat{
			{PullRequestId: "pr1", PullRequestName: "Docs", ReviewerCount: 1},
		},
	}
	repo := &mockPullRequestRepository{
		getAssignmentStatsFn: func(context.Context) (*models.AssignmentStats, error) {
			return want, nil
		},
	}
	manager := &PullRequestManager{repo: repo, UserService: &mockUserService{}}
	got, err := manager.AssignmentStats(context.Background())
	if err != nil {
		t.Fatalf("AssignmentStats returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected stats: %+v", got)
	}
}

func TestPullRequestManager_AssignmentStatsError(t *testing.T) {
	repo := &mockPullRequestRepository{
		getAssignmentStatsFn: func(context.Context) (*models.AssignmentStats, error) {
			return nil, errors.New("boom")
		},
	}
	manager := &PullRequestManager{repo: repo, UserService: &mockUserService{}}
	if _, err := manager.AssignmentStats(context.Background()); err == nil || err.Error() != "failed to get assignment stats: boom" {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestPullRequestManager_ListForReviewer(t *testing.T) {
	repo := &mockPullRequestRepository{
		findPullRequestsByReviewerFn: func(context.Context, string) ([]*models.PullRequest, error) {
			return []*models.PullRequest{
				{
					AuthorId:        "a1",
					PullRequestId:   "pr1",
					PullRequestName: "PR 1",
					Status:          models.PullRequestStatusOPEN,
				},
				{
					AuthorId:        "a2",
					PullRequestId:   "pr2",
					PullRequestName: "PR 2",
					Status:          models.PullRequestStatusMERGED,
				},
			}, nil
		},
	}
	manager := &PullRequestManager{repo: repo, UserService: &mockUserService{}}
	res, err := manager.ListForReviewer(context.Background(), "rev")
	if err != nil {
		t.Fatalf("ListForReviewer returned error: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected two short PRs, got %d", len(res))
	}
	if res[0].PullRequestName != "PR 1" || res[1].Status != models.PullRequestShortStatusMERGED {
		t.Fatalf("unexpected conversion result: %+v", res)
	}
}

func TestPullRequestManager_BulkDeactivateTeamMembers(t *testing.T) {
	ctx := context.Background()

	t.Run("success with replacements", func(t *testing.T) {
		repo := &mockPullRequestRepository{
			findOpenPullRequestsByReviewerFn: func(ctx context.Context, ids []string) ([]*models.PullRequest, error) {
				require.ElementsMatch(t, []string{"u1"}, ids)
				return []*models.PullRequest{
					{
						PullRequestId:     "pr-1",
						Status:            models.PullRequestStatusOPEN,
						AssignedReviewers: []string{"u1", "u4"},
					},
				}, nil
			},
			applyBulkTeamReviewerSwapsFn: func(ctx context.Context, swaps []models.ReviewerSwap, users []string) error {
				require.Equal(t, []models.ReviewerSwap{{PullRequestId: "pr-1", OldUserId: "u1", NewUserId: "u2"}}, swaps)
				require.ElementsMatch(t, []string{"u1", "u2"}, users)
				return nil
			},
		}
		var synced [][]string
		userSvc := &mockUserService{
			getTeamFn: func(ctx context.Context, teamName string) (*models.Team, error) {
				require.Equal(t, "backend", teamName)
				return &models.Team{
					TeamName: "backend",
					Members: []models.TeamMember{
						{UserId: "u1", IsActive: true},
						{UserId: "u2", IsActive: true},
						{UserId: "u4", IsActive: false},
					},
				}, nil
			},
			syncUsersActivityFn: func(ids []string, status bool) {
				require.False(t, status)
				synced = append(synced, ids)
			},
		}

		prm := &PullRequestManager{repo: repo, UserService: userSvc}
		result, err := prm.BulkDeactivateTeamMembers(ctx, "backend", []string{"u1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		require.Equal(t, "backend", result.TeamName)
		require.Equal(t, []string{"u1"}, result.Deactivated)
		require.Len(t, result.Reassignments, 1)
		require.Equal(t, "pr-1", result.Reassignments[0].PullRequestId)
		require.Equal(t, "u2", result.Reassignments[0].Replacements[0].NewUserId)
		require.Len(t, synced, 1)
		require.ElementsMatch(t, []string{"u1", "u2"}, synced[0])
	})

	t.Run("deactivate without open prs", func(t *testing.T) {
		repo := &mockPullRequestRepository{
			findOpenPullRequestsByReviewerFn: func(context.Context, []string) ([]*models.PullRequest, error) {
				return nil, nil
			},
			applyBulkTeamReviewerSwapsFn: func(ctx context.Context, swaps []models.ReviewerSwap, users []string) error {
				require.Len(t, swaps, 0)
				require.ElementsMatch(t, []string{"u1"}, users)
				return nil
			},
		}
		userSvc := &mockUserService{
			getTeamFn: func(ctx context.Context, teamName string) (*models.Team, error) {
				return &models.Team{
					TeamName: "backend",
					Members:  []models.TeamMember{{UserId: "u1", IsActive: true}},
				}, nil
			},
		}
		prm := &PullRequestManager{repo: repo, UserService: userSvc}
		if _, err := prm.BulkDeactivateTeamMembers(ctx, "backend", []string{"u1"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("user not in team", func(t *testing.T) {
		repo := &mockPullRequestRepository{}
		userSvc := &mockUserService{
			getTeamFn: func(ctx context.Context, teamName string) (*models.Team, error) {
				return &models.Team{
					TeamName: "backend",
					Members:  []models.TeamMember{{UserId: "u2", IsActive: true}},
				}, nil
			},
		}
		prm := &PullRequestManager{repo: repo, UserService: userSvc}
		_, err := prm.BulkDeactivateTeamMembers(ctx, "backend", []string{"u1"})
		if err == nil || !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("expected not found error, got %v", err)
		}
	})

	t.Run("no replacements", func(t *testing.T) {
		repo := &mockPullRequestRepository{
			findOpenPullRequestsByReviewerFn: func(context.Context, []string) ([]*models.PullRequest, error) {
				return []*models.PullRequest{
					{
						PullRequestId:     "pr-1",
						Status:            models.PullRequestStatusOPEN,
						AssignedReviewers: []string{"u1"},
					},
				}, nil
			},
		}
		userSvc := &mockUserService{
			getTeamFn: func(ctx context.Context, teamName string) (*models.Team, error) {
				return &models.Team{
					TeamName: "backend",
					Members: []models.TeamMember{
						{UserId: "u1", IsActive: true},
						{UserId: "u2", IsActive: false},
					},
				}, nil
			},
		}
		prm := &PullRequestManager{repo: repo, UserService: userSvc}
		_, err := prm.BulkDeactivateTeamMembers(ctx, "backend", []string{"u1"})
		if err == nil || !errors.Is(err, domain.ErrNoCandidate) {
			t.Fatalf("expected no candidate error, got %v", err)
		}
	})
}
