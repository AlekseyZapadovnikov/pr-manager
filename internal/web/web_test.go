package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AlekseyZapadovnikov/pr-manager/conf"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/domain"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/models"
	"github.com/stretchr/testify/require"
)

func TestNewServerRegistersRoutes(t *testing.T) {
	cfg := conf.HttpServConf{Host: "127.0.0.1", Port: "9999", BaseURL: "/api"}

	srv := New(cfg, &fakePRService{}, &fakeUserTeamService{})

	require.Equal(t, cfg.GetAddress(), srv.Address)
	require.NotNil(t, srv.router)
	require.NotNil(t, srv.server)
	require.Equal(t, srv.router, srv.server.Handler)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Equal(t, "ok", resp["status"])
}

func TestWriteJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	payload := map[string]string{"status": "ok", "message": "<tag>"}

	writeJSON(rr, http.StatusAccepted, payload)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var decoded map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &decoded))
	require.Equal(t, payload, decoded)
}

func TestWriteError(t *testing.T) {
	rr := httptest.NewRecorder()

	writeError(rr, http.StatusBadRequest, "CODE", "message")

	assertErrorResponse(t, rr, http.StatusBadRequest, "CODE", "message")
}

func TestMapDomainError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "nil", err: nil, status: http.StatusOK, code: ""},
		{name: "team exists", err: domain.ErrTeamExists, status: http.StatusBadRequest, code: "TEAM_EXISTS"},
		{name: "pr exists", err: domain.ErrPRExists, status: http.StatusConflict, code: "PR_EXISTS"},
		{name: "pr merged", err: domain.ErrPRMerged, status: http.StatusConflict, code: "PR_MERGED"},
		{name: "not assigned", err: domain.ErrNotAssigned, status: http.StatusConflict, code: "NOT_ASSIGNED"},
		{name: "no candidate", err: domain.ErrNoCandidate, status: http.StatusConflict, code: "NO_CANDIDATE"},
		{name: "not found", err: domain.ErrNotFound, status: http.StatusNotFound, code: "NOT_FOUND"},
		{name: "unauthorized", err: domain.ErrUnauthorized, status: http.StatusUnauthorized, code: "UNAUTHORIZED"},
		{name: "default", err: errors.New("boom"), status: http.StatusInternalServerError, code: "INTERNAL_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, code, msg := mapDomainError(tt.err)
			require.Equal(t, tt.status, status)
			require.Equal(t, tt.code, code)
			if tt.err == nil {
				require.Empty(t, msg)
			} else {
				require.Equal(t, tt.err.Error(), msg)
			}
		})
	}
}

func TestHandleTeamAdd(t *testing.T) {
	team := models.Team{
		TeamName: "backend",
		Members: []models.TeamMember{
			{UserId: "user-1", Username: "Alice", IsActive: true},
		},
	}

	t.Run("success", func(t *testing.T) {
		srv := newBareServer(nil, &fakeUserTeamService{
			addFn: func(ctx context.Context, got models.Team) error {
				require.Equal(t, team, got)
				return nil
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/team/add", mustJSONReader(t, team))
		rr := httptest.NewRecorder()
		srv.handleTeamAdd(rr, req)

		require.Equal(t, http.StatusCreated, rr.Code)
		var resp map[string]models.Team
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
		require.Equal(t, team, resp["team"])
	})

	t.Run("invalid payload", func(t *testing.T) {
		srv := newBareServer(nil, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/team/add", strings.NewReader("{bad json"))
		rr := httptest.NewRecorder()

		srv.handleTeamAdd(rr, req)

		assertErrorResponse(t, rr, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid json payload")
	})

	t.Run("domain error", func(t *testing.T) {
		srv := newBareServer(nil, &fakeUserTeamService{
			addFn: func(ctx context.Context, got models.Team) error {
				return domain.ErrTeamExists
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/team/add", mustJSONReader(t, team))
		rr := httptest.NewRecorder()

		srv.handleTeamAdd(rr, req)

		assertErrorResponse(t, rr, http.StatusBadRequest, "TEAM_EXISTS", domain.ErrTeamExists.Error())
	})
}

func TestHandleTeamGet(t *testing.T) {
	team := &models.Team{TeamName: "backend"}

	t.Run("missing name", func(t *testing.T) {
		srv := newBareServer(nil, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodGet, "/team/get", nil)
		rr := httptest.NewRecorder()

		srv.handleTeamGet(rr, req)

		assertErrorResponse(t, rr, http.StatusBadRequest, "MISSING_PARAM", "team_name is required")
	})

	t.Run("domain error", func(t *testing.T) {
		srv := newBareServer(nil, &fakeUserTeamService{
			getFn: func(ctx context.Context, name string) (*models.Team, error) {
				require.Equal(t, "backend", name)
				return nil, domain.ErrNotFound
			},
		})
		req := httptest.NewRequest(http.MethodGet, "/team/get?team_name=backend", nil)
		rr := httptest.NewRecorder()

		srv.handleTeamGet(rr, req)

		assertErrorResponse(t, rr, http.StatusNotFound, "NOT_FOUND", domain.ErrNotFound.Error())
	})

	t.Run("success", func(t *testing.T) {
		srv := newBareServer(nil, &fakeUserTeamService{
			getFn: func(ctx context.Context, name string) (*models.Team, error) {
				require.Equal(t, "backend", name)
				return team, nil
			},
		})
		req := httptest.NewRequest(http.MethodGet, "/team/get?team_name=backend", nil)
		rr := httptest.NewRecorder()

		srv.handleTeamGet(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var resp models.Team
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
		require.Equal(t, *team, resp)
	})
}

func TestHandleTeamDeactivate(t *testing.T) {
	payload := models.TeamBulkDeactivateRequest{
		TeamName: "backend",
		UserIDs:  []string{"u1", "u2"},
	}

	t.Run("invalid payload", func(t *testing.T) {
		srv := newBareServer(&fakePRService{}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/team/deactivateUsers", strings.NewReader("{bad json"))
		rr := httptest.NewRecorder()

		srv.handleTeamDeactivate(rr, req)

		assertErrorResponse(t, rr, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid json payload")
	})

	t.Run("missing team name", func(t *testing.T) {
		reqPayload := payload
		reqPayload.TeamName = ""
		srv := newBareServer(&fakePRService{}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/team/deactivateUsers", mustJSONReader(t, reqPayload))
		rr := httptest.NewRecorder()

		srv.handleTeamDeactivate(rr, req)

		assertErrorResponse(t, rr, http.StatusBadRequest, "MISSING_PARAM", "team_name is required")
	})

	t.Run("missing users", func(t *testing.T) {
		reqPayload := payload
		reqPayload.UserIDs = []string{}
		srv := newBareServer(&fakePRService{}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/team/deactivateUsers", mustJSONReader(t, reqPayload))
		rr := httptest.NewRecorder()

		srv.handleTeamDeactivate(rr, req)

		assertErrorResponse(t, rr, http.StatusBadRequest, "MISSING_PARAM", "at least one user_id is required")
	})

	t.Run("domain error", func(t *testing.T) {
		srv := newBareServer(&fakePRService{
			bulkDeactivateFn: func(ctx context.Context, teamName string, userIDs []string) (*models.TeamBulkDeactivateResult, error) {
				require.Equal(t, payload.TeamName, teamName)
				require.Equal(t, []string{"u1", "u2"}, userIDs)
				return nil, domain.ErrNoCandidate
			},
		}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/team/deactivateUsers", mustJSONReader(t, payload))
		rr := httptest.NewRecorder()

		srv.handleTeamDeactivate(rr, req)

		assertErrorResponse(t, rr, http.StatusConflict, "NO_CANDIDATE", domain.ErrNoCandidate.Error())
	})

	t.Run("success", func(t *testing.T) {
		result := &models.TeamBulkDeactivateResult{
			TeamName:    payload.TeamName,
			Deactivated: []string{"u1"},
			Reassignments: []models.TeamPRReassignment{
				{
					PullRequestId: "pr-1",
					Replacements: []models.ReviewerReplacement{
						{OldUserId: "u1", NewUserId: "u3"},
					},
				},
			},
		}
		srv := newBareServer(&fakePRService{
			bulkDeactivateFn: func(ctx context.Context, teamName string, userIDs []string) (*models.TeamBulkDeactivateResult, error) {
				require.Equal(t, payload.TeamName, teamName)
				require.Equal(t, []string{"u1", "u2"}, userIDs)
				return result, nil
			},
		}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/team/deactivateUsers", mustJSONReader(t, payload))
		rr := httptest.NewRecorder()

		srv.handleTeamDeactivate(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var resp teamDeactivateResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
		require.Equal(t, result, resp.Result)
	})
}

func TestHandleSetUserActivity(t *testing.T) {
	payload := models.PostUsersSetIsActiveJSONBody{UserId: "user-1", IsActive: true}
	user := &models.User{UserId: "user-1", Username: "Alice", IsActive: true}

	t.Run("invalid payload", func(t *testing.T) {
		srv := newBareServer(nil, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/users/setIsActive", strings.NewReader("{bad json"))
		rr := httptest.NewRecorder()

		srv.handleSetUserActivity(rr, req)

		assertErrorResponse(t, rr, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid json payload")
	})

	t.Run("missing user", func(t *testing.T) {
		srv := newBareServer(nil, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/users/setIsActive", mustJSONReader(t, models.PostUsersSetIsActiveJSONBody{IsActive: true}))
		rr := httptest.NewRecorder()

		srv.handleSetUserActivity(rr, req)

		assertErrorResponse(t, rr, http.StatusBadRequest, "MISSING_PARAM", "user_id is required")
	})

	t.Run("domain error", func(t *testing.T) {
		srv := newBareServer(nil, &fakeUserTeamService{
			setFn: func(userID string, isActive bool) (*models.User, error) {
				require.Equal(t, payload.UserId, userID)
				require.Equal(t, payload.IsActive, isActive)
				return nil, domain.ErrUnauthorized
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/users/setIsActive", mustJSONReader(t, payload))
		rr := httptest.NewRecorder()

		srv.handleSetUserActivity(rr, req)

		assertErrorResponse(t, rr, http.StatusUnauthorized, "UNAUTHORIZED", domain.ErrUnauthorized.Error())
	})

	t.Run("success", func(t *testing.T) {
		srv := newBareServer(nil, &fakeUserTeamService{
			setFn: func(userID string, isActive bool) (*models.User, error) {
				require.Equal(t, payload.UserId, userID)
				require.Equal(t, payload.IsActive, isActive)
				return user, nil
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/users/setIsActive", mustJSONReader(t, payload))
		rr := httptest.NewRecorder()

		srv.handleSetUserActivity(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var resp setUserResp
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
		require.Equal(t, user, resp.User)
	})
}

func TestHandleGetUserReviews(t *testing.T) {
	reviews := []models.PullRequestShort{
		{PullRequestId: "pr-1", PullRequestName: "Fix", AuthorId: "author-1"},
	}

	t.Run("missing user id", func(t *testing.T) {
		srv := newBareServer(&fakePRService{}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodGet, "/users/getReview", nil)
		rr := httptest.NewRecorder()

		srv.handleGetUserReviews(rr, req)

		assertErrorResponse(t, rr, http.StatusBadRequest, "MISSING_PARAM", "user_id is required")
	})

	t.Run("domain error", func(t *testing.T) {
		srv := newBareServer(&fakePRService{
			listFn: func(ctx context.Context, userID string) ([]models.PullRequestShort, error) {
				require.Equal(t, "user-1", userID)
				return nil, domain.ErrNotFound
			},
		}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodGet, "/users/getReview?user_id=user-1", nil)
		rr := httptest.NewRecorder()

		srv.handleGetUserReviews(rr, req)

		assertErrorResponse(t, rr, http.StatusNotFound, "NOT_FOUND", domain.ErrNotFound.Error())
	})

	t.Run("success", func(t *testing.T) {
		srv := newBareServer(&fakePRService{
			listFn: func(ctx context.Context, userID string) ([]models.PullRequestShort, error) {
				require.Equal(t, "user-1", userID)
				return reviews, nil
			},
		}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodGet, "/users/getReview?user_id=user-1", nil)
		rr := httptest.NewRecorder()

		srv.handleGetUserReviews(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var resp getUserReviewsResp
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
		require.Equal(t, "user-1", resp.UserId)
		require.Equal(t, reviews, resp.PullRequests)
	})
}

func TestHandlePRCreate(t *testing.T) {
	payload := models.PostPullRequestCreateJSONBody{
		AuthorId:        "author-1",
		PullRequestId:   "pr-1",
		PullRequestName: "Feature",
	}
	pr := &models.PullRequest{AuthorId: payload.AuthorId, PullRequestId: payload.PullRequestId, PullRequestName: payload.PullRequestName}

	t.Run("invalid payload", func(t *testing.T) {
		srv := newBareServer(&fakePRService{}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/pullRequest/create", strings.NewReader("{bad json"))
		rr := httptest.NewRecorder()

		srv.handlePRCreate(rr, req)

		assertErrorResponse(t, rr, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid json payload")
	})

	t.Run("missing fields", func(t *testing.T) {
		srv := newBareServer(&fakePRService{}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/pullRequest/create", mustJSONReader(t, models.PostPullRequestCreateJSONBody{
			PullRequestId: "pr-1",
		}))
		rr := httptest.NewRecorder()

		srv.handlePRCreate(rr, req)

		assertErrorResponse(t, rr, http.StatusBadRequest, "MISSING_PARAM", "pull_request_id, pull_request_name and author_id are required")
	})

	t.Run("domain error", func(t *testing.T) {
		srv := newBareServer(&fakePRService{
			createFn: func(ctx context.Context, got models.PostPullRequestCreateJSONBody) (*models.PullRequest, error) {
				require.Equal(t, payload, got)
				return nil, domain.ErrPRExists
			},
		}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/pullRequest/create", mustJSONReader(t, payload))
		rr := httptest.NewRecorder()

		srv.handlePRCreate(rr, req)

		assertErrorResponse(t, rr, http.StatusConflict, "PR_EXISTS", domain.ErrPRExists.Error())
	})

	t.Run("success", func(t *testing.T) {
		srv := newBareServer(&fakePRService{
			createFn: func(ctx context.Context, got models.PostPullRequestCreateJSONBody) (*models.PullRequest, error) {
				require.Equal(t, payload, got)
				return pr, nil
			},
		}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/pullRequest/create", mustJSONReader(t, payload))
		rr := httptest.NewRecorder()

		srv.handlePRCreate(rr, req)

		require.Equal(t, http.StatusCreated, rr.Code)
		var resp prResp
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
		require.Equal(t, pr, resp.PR)
	})
}

func TestHandlePRMerge(t *testing.T) {
	payload := models.PostPullRequestMergeJSONBody{PullRequestId: "pr-1"}
	pr := &models.PullRequest{PullRequestId: payload.PullRequestId, PullRequestName: "Feature", AuthorId: "author-1"}

	t.Run("invalid payload", func(t *testing.T) {
		srv := newBareServer(&fakePRService{}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/pullRequest/merge", strings.NewReader("{bad json"))
		rr := httptest.NewRecorder()

		srv.handlePRMerge(rr, req)

		assertErrorResponse(t, rr, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid json payload")
	})

	t.Run("missing id", func(t *testing.T) {
		srv := newBareServer(&fakePRService{}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/pullRequest/merge", mustJSONReader(t, models.PostPullRequestMergeJSONBody{}))
		rr := httptest.NewRecorder()

		srv.handlePRMerge(rr, req)

		assertErrorResponse(t, rr, http.StatusBadRequest, "MISSING_PARAM", "pull_request_id is required")
	})

	t.Run("domain error", func(t *testing.T) {
		srv := newBareServer(&fakePRService{
			mergeFn: func(ctx context.Context, got models.PostPullRequestMergeJSONBody) (*models.PullRequest, error) {
				require.Equal(t, payload, got)
				return nil, domain.ErrPRMerged
			},
		}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/pullRequest/merge", mustJSONReader(t, payload))
		rr := httptest.NewRecorder()

		srv.handlePRMerge(rr, req)

		assertErrorResponse(t, rr, http.StatusConflict, "PR_MERGED", domain.ErrPRMerged.Error())
	})

	t.Run("success", func(t *testing.T) {
		srv := newBareServer(&fakePRService{
			mergeFn: func(ctx context.Context, got models.PostPullRequestMergeJSONBody) (*models.PullRequest, error) {
				require.Equal(t, payload, got)
				return pr, nil
			},
		}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/pullRequest/merge", mustJSONReader(t, payload))
		rr := httptest.NewRecorder()

		srv.handlePRMerge(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var resp prResp
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
		require.Equal(t, pr, resp.PR)
	})
}

func TestHandlePRReassign(t *testing.T) {
	payload := models.PostPullRequestReassignJSONBody{PullRequestId: "pr-1", OldUserId: "user-old"}
	pr := &models.PullRequest{PullRequestId: payload.PullRequestId, PullRequestName: "Feature"}
	response := &domain.ReassignResponse{PR: pr, ReplacedBy: "user-new"}

	t.Run("invalid payload", func(t *testing.T) {
		srv := newBareServer(&fakePRService{}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/pullRequest/reassign", strings.NewReader("{bad json"))
		rr := httptest.NewRecorder()

		srv.handlePRReassign(rr, req)

		assertErrorResponse(t, rr, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid json payload")
	})

	t.Run("missing fields", func(t *testing.T) {
		srv := newBareServer(&fakePRService{}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/pullRequest/reassign", mustJSONReader(t, models.PostPullRequestReassignJSONBody{}))
		rr := httptest.NewRecorder()

		srv.handlePRReassign(rr, req)

		assertErrorResponse(t, rr, http.StatusBadRequest, "MISSING_PARAM", "pull_request_id and old_user_id are required")
	})

	t.Run("domain error", func(t *testing.T) {
		srv := newBareServer(&fakePRService{
			reassignFn: func(ctx context.Context, oldUserID, prID string) (*domain.ReassignResponse, error) {
				require.Equal(t, payload.OldUserId, oldUserID)
				require.Equal(t, payload.PullRequestId, prID)
				return nil, domain.ErrNoCandidate
			},
		}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/pullRequest/reassign", mustJSONReader(t, payload))
		rr := httptest.NewRecorder()

		srv.handlePRReassign(rr, req)

		assertErrorResponse(t, rr, http.StatusConflict, "NO_CANDIDATE", domain.ErrNoCandidate.Error())
	})

	t.Run("success", func(t *testing.T) {
		srv := newBareServer(&fakePRService{
			reassignFn: func(ctx context.Context, oldUserID, prID string) (*domain.ReassignResponse, error) {
				require.Equal(t, payload.OldUserId, oldUserID)
				require.Equal(t, payload.PullRequestId, prID)
				return response, nil
			},
		}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodPost, "/pullRequest/reassign", mustJSONReader(t, payload))
		rr := httptest.NewRecorder()

		srv.handlePRReassign(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var resp reassignResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
		require.Equal(t, response.PR, resp.PR)
		require.Equal(t, response.ReplacedBy, resp.ReplacedBy)
	})
}

func TestHandleAssignmentStats(t *testing.T) {
	stats := &models.AssignmentStats{
		ByUser: []models.UserAssignmentStat{
			{UserId: "u1", Username: "Alice", Assignments: 3},
		},
		ByPullRequest: []models.PullRequestAssignmentStat{
			{PullRequestId: "pr1", PullRequestName: "Docs", ReviewerCount: 2},
		},
	}

	t.Run("success", func(t *testing.T) {
		srv := newBareServer(&fakePRService{
			assignmentStatsFn: func(ctx context.Context) (*models.AssignmentStats, error) {
				return stats, nil
			},
		}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodGet, "/stats/assignments", nil)
		rr := httptest.NewRecorder()

		srv.handleAssignmentStats(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var resp models.AssignmentStats
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
		require.Equal(t, stats.ByUser, resp.ByUser)
		require.Equal(t, stats.ByPullRequest, resp.ByPullRequest)
	})

	t.Run("error", func(t *testing.T) {
		srv := newBareServer(&fakePRService{
			assignmentStatsFn: func(ctx context.Context) (*models.AssignmentStats, error) {
				return nil, errors.New("boom")
			},
		}, &fakeUserTeamService{})
		req := httptest.NewRequest(http.MethodGet, "/stats/assignments", nil)
		rr := httptest.NewRecorder()

		srv.handleAssignmentStats(rr, req)

		assertErrorResponse(t, rr, http.StatusInternalServerError, "INTERNAL_ERROR", "boom")
	})
}

// --- helpers ----------------------------------------------------------------

type fakePRService struct {
	createFn          func(ctx context.Context, payload models.PostPullRequestCreateJSONBody) (*models.PullRequest, error)
	mergeFn           func(ctx context.Context, payload models.PostPullRequestMergeJSONBody) (*models.PullRequest, error)
	reassignFn        func(ctx context.Context, oldUserID, prID string) (*domain.ReassignResponse, error)
	listFn            func(ctx context.Context, userID string) ([]models.PullRequestShort, error)
	assignmentStatsFn func(ctx context.Context) (*models.AssignmentStats, error)
	bulkDeactivateFn  func(ctx context.Context, teamName string, userIDs []string) (*models.TeamBulkDeactivateResult, error)
}

func (f *fakePRService) CreatePullRequest(ctx context.Context, payload models.PostPullRequestCreateJSONBody) (*models.PullRequest, error) {
	if f != nil && f.createFn != nil {
		return f.createFn(ctx, payload)
	}
	return nil, nil
}

func (f *fakePRService) Merge(ctx context.Context, payload models.PostPullRequestMergeJSONBody) (*models.PullRequest, error) {
	if f != nil && f.mergeFn != nil {
		return f.mergeFn(ctx, payload)
	}
	return nil, nil
}

func (f *fakePRService) Reassign(ctx context.Context, oldUsId, prId string) (*domain.ReassignResponse, error) {
	if f != nil && f.reassignFn != nil {
		return f.reassignFn(ctx, oldUsId, prId)
	}
	return nil, nil
}

func (f *fakePRService) ListForReviewer(ctx context.Context, userID string) ([]models.PullRequestShort, error) {
	if f != nil && f.listFn != nil {
		return f.listFn(ctx, userID)
	}
	return nil, nil
}

func (f *fakePRService) AssignmentStats(ctx context.Context) (*models.AssignmentStats, error) {
	if f != nil && f.assignmentStatsFn != nil {
		return f.assignmentStatsFn(ctx)
	}
	return nil, nil
}

func (f *fakePRService) BulkDeactivateTeamMembers(ctx context.Context, teamName string, userIDs []string) (*models.TeamBulkDeactivateResult, error) {
	if f != nil && f.bulkDeactivateFn != nil {
		return f.bulkDeactivateFn(ctx, teamName, userIDs)
	}
	return nil, nil
}

type fakeUserTeamService struct {
	addFn func(ctx context.Context, team models.Team) error
	getFn func(ctx context.Context, teamName string) (*models.Team, error)
	setFn func(userID string, isActive bool) (*models.User, error)
}

func (f *fakeUserTeamService) AddTeam(ctx context.Context, team models.Team) error {
	if f != nil && f.addFn != nil {
		return f.addFn(ctx, team)
	}
	return nil
}

func (f *fakeUserTeamService) GetTeam(ctx context.Context, teamName string) (*models.Team, error) {
	if f != nil && f.getFn != nil {
		return f.getFn(ctx, teamName)
	}
	return nil, nil
}

func (f *fakeUserTeamService) SetUserActivity(userID string, isActive bool) (*models.User, error) {
	if f != nil && f.setFn != nil {
		return f.setFn(userID, isActive)
	}
	return nil, nil
}

func newBareServer(pr PullRequestService, user UserTeamService) *Server {
	return &Server{
		prService:       pr,
		userTeamService: user,
	}
}

func mustJSONReader(tb testing.TB, v interface{}) *bytes.Reader {
	tb.Helper()
	data, err := json.Marshal(v)
	require.NoError(tb, err)
	return bytes.NewReader(data)
}

func assertErrorResponse(t *testing.T, rr *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	require.Equal(t, status, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	var resp errorResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Equal(t, code, resp.Error.Code)
	require.Equal(t, message, resp.Error.Message)
}
