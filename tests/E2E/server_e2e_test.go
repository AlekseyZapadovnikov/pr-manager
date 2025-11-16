package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AlekseyZapadovnikov/pr-manager/conf"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/domain"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/models"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/service"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/web"
)

func TestE2E_PRManager(t *testing.T) {
	suite := newE2ESuite(t)
	suite.mustHealth()

	const teamName = "backend-e2e"
	team := models.Team{
		TeamName: teamName,
		Members: []models.TeamMember{
			{UserId: "dev-1", Username: "Alice", IsActive: true},
			{UserId: "dev-2", Username: "Bob", IsActive: true},
			{UserId: "dev-3", Username: "Carol", IsActive: true},
			{UserId: "dev-4", Username: "Dave", IsActive: true},
		},
	}

	createdTeam := suite.mustAddTeam(team)
	require.Equal(t, team.TeamName, createdTeam.TeamName)
	require.Len(t, createdTeam.Members, len(team.Members))

	fetchedTeam := suite.mustGetTeam(team.TeamName)
	require.Equal(t, team.TeamName, fetchedTeam.TeamName)

	prOne := suite.mustCreatePullRequest(models.PostPullRequestCreateJSONBody{
		AuthorId:        "dev-1",
		PullRequestId:   "pr-1001",
		PullRequestName: "Refactor assignment logic",
	})
	require.NotNil(t, prOne)
	require.Len(t, prOne.AssignedReviewers, 2)

	firstReviewer := prOne.AssignedReviewers[0]
	reviews := suite.mustGetUserReviews(firstReviewer)
	suite.requirePRListed(reviews.PullRequests, prOne.PullRequestId)

	reassignResp := suite.mustReassign(prOne.PullRequestId, firstReviewer)
	require.Equal(t, prOne.PullRequestId, reassignResp.PR.PullRequestId)
	require.NotEmpty(t, reassignResp.ReplacedBy)
	require.NotEqual(t, firstReviewer, reassignResp.ReplacedBy)

	mergeResp := suite.mustMerge(prOne.PullRequestId)
	require.Equal(t, prOne.PullRequestId, mergeResp.PR.PullRequestId)
	require.Equal(t, models.PullRequestStatusMERGED, mergeResp.PR.Status)
	require.NotNil(t, mergeResp.PR.MergedAt)

	stats := suite.mustGetAssignmentStats()
	require.NotEmpty(t, stats.ByPullRequest)

	prTwo := suite.mustCreatePullRequest(models.PostPullRequestCreateJSONBody{
		AuthorId:        "dev-2",
		PullRequestId:   "pr-2002",
		PullRequestName: "Add telemetry hooks",
	})
	require.NotNil(t, prTwo)
	require.Len(t, prTwo.AssignedReviewers, 2)
	targetReviewer := prTwo.AssignedReviewers[0]

	deactivateResp := suite.mustDeactivateTeamMembers(team.TeamName, []string{targetReviewer})
	require.Equal(t, team.TeamName, deactivateResp.Result.TeamName)
	require.Contains(t, deactivateResp.Result.Deactivated, targetReviewer)
	require.NotEmpty(t, deactivateResp.Result.Reassignments)
	require.Equal(t, prTwo.PullRequestId, deactivateResp.Result.Reassignments[0].PullRequestId)

	reviewsAfterDeactivate := suite.mustGetUserReviews(targetReviewer)
	suite.requirePRNotListed(reviewsAfterDeactivate.PullRequests, prTwo.PullRequestId)

	updatedUser := suite.mustSetUserActivity(targetReviewer, true)
	require.True(t, updatedUser.IsActive)
	require.Equal(t, targetReviewer, updatedUser.UserId)
}

type e2eSuite struct {
	t       *testing.T
	server  *web.Server
	storage *memoryStorage
	baseURL string
	client  *http.Client
	errCh   chan error
}

func newE2ESuite(t *testing.T) *e2eSuite {
	t.Helper()

	storage := newMemoryStorage()
	userManager := service.NewUserManager(storage)
	prManager := (&service.PullRequestManager{}).NewPullRequestService(storage, userManager)

	cfg := conf.HttpServConf{
		Host: "127.0.0.1",
		Port: freePort(t),
	}

	server := web.New(cfg, prManager, userManager)
	suite := &e2eSuite{
		t:       t,
		server:  server,
		storage: storage,
		baseURL: fmt.Sprintf("http://%s", server.Address),
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
		errCh: make(chan error, 1),
	}
	suite.startServer()
	suite.waitForReady()

	t.Cleanup(func() {
		suite.shutdown()
	})

	return suite
}

func (s *e2eSuite) startServer() {
	go func() {
		err := s.server.Start()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.errCh <- err
			return
		}
		s.errCh <- nil
	}()
}

func (s *e2eSuite) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(s.t, s.server.Shutdown(ctx))
	err := <-s.errCh
	require.NoError(s.t, err)
}

func (s *e2eSuite) waitForReady() {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := s.client.Get(s.url("/health"))
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.t.Fatalf("server at %s did not become ready", s.baseURL)
}

func (s *e2eSuite) url(path string) string {
	return fmt.Sprintf("%s%s", s.baseURL, path)
}

func (s *e2eSuite) mustHealth() {
	resp, err := s.client.Get(s.url("/health"))
	require.NoError(s.t, err)
	defer resp.Body.Close()
	require.Equal(s.t, http.StatusOK, resp.StatusCode)
}

func (s *e2eSuite) mustAddTeam(team models.Team) models.Team {
	resp := s.doJSON(http.MethodPost, "/team/add", team)
	require.Equal(s.t, http.StatusCreated, resp.StatusCode)
	var body struct {
		Team *models.Team `json:"team"`
	}
	decodeJSON(s.t, resp, &body)
	require.NotNil(s.t, body.Team)
	return *body.Team
}

func (s *e2eSuite) mustGetTeam(teamName string) models.Team {
	resp, err := s.client.Get(s.url("/team/get?team_name=" + url.QueryEscape(teamName)))
	require.NoError(s.t, err)
	require.Equal(s.t, http.StatusOK, resp.StatusCode)
	var team models.Team
	decodeJSON(s.t, resp, &team)
	return team
}

func (s *e2eSuite) mustCreatePullRequest(payload models.PostPullRequestCreateJSONBody) *models.PullRequest {
	resp := s.doJSON(http.MethodPost, "/pullRequest/create", payload)
	require.Equal(s.t, http.StatusCreated, resp.StatusCode)
	var body struct {
		PR *models.PullRequest `json:"pr"`
	}
	decodeJSON(s.t, resp, &body)
	require.NotNil(s.t, body.PR)
	return body.PR
}

func (s *e2eSuite) mustMerge(prID string) *prResponse {
	resp := s.doJSON(http.MethodPost, "/pullRequest/merge", models.PostPullRequestMergeJSONBody{
		PullRequestId: prID,
	})
	require.Equal(s.t, http.StatusOK, resp.StatusCode)
	var body prResponse
	decodeJSON(s.t, resp, &body)
	require.NotNil(s.t, body.PR)
	return &body
}

func (s *e2eSuite) mustReassign(prID, oldReviewer string) *reassignResponse {
	resp := s.doJSON(http.MethodPost, "/pullRequest/reassign", models.PostPullRequestReassignJSONBody{
		OldUserId:     oldReviewer,
		PullRequestId: prID,
	})
	require.Equal(s.t, http.StatusOK, resp.StatusCode)
	var body reassignResponse
	decodeJSON(s.t, resp, &body)
	require.NotNil(s.t, body.PR)
	return &body
}

func (s *e2eSuite) mustGetUserReviews(userID string) userReviewsResponse {
	resp, err := s.client.Get(s.url("/users/getReview?user_id=" + url.QueryEscape(userID)))
	require.NoError(s.t, err)
	require.Equal(s.t, http.StatusOK, resp.StatusCode)
	var body userReviewsResponse
	decodeJSON(s.t, resp, &body)
	return body
}

func (s *e2eSuite) mustGetAssignmentStats() *models.AssignmentStats {
	resp, err := s.client.Get(s.url("/stats/assignments"))
	require.NoError(s.t, err)
	require.Equal(s.t, http.StatusOK, resp.StatusCode)
	var stats models.AssignmentStats
	decodeJSON(s.t, resp, &stats)
	return &stats
}

func (s *e2eSuite) mustDeactivateTeamMembers(teamName string, userIDs []string) teamDeactivateResponse {
	payload := models.TeamBulkDeactivateRequest{
		TeamName: teamName,
		UserIDs:  userIDs,
	}
	resp := s.doJSON(http.MethodPost, "/team/deactivateUsers", payload)
	require.Equal(s.t, http.StatusOK, resp.StatusCode)
	var body teamDeactivateResponse
	decodeJSON(s.t, resp, &body)
	require.NotNil(s.t, body.Result)
	return body
}

func (s *e2eSuite) mustSetUserActivity(userID string, active bool) *models.User {
	resp := s.doJSON(http.MethodPost, "/users/setIsActive", models.PostUsersSetIsActiveJSONBody{
		UserId:   userID,
		IsActive: active,
	})
	require.Equal(s.t, http.StatusOK, resp.StatusCode)
	var body struct {
		User *models.User `json:"user"`
	}
	decodeJSON(s.t, resp, &body)
	require.NotNil(s.t, body.User)
	return body.User
}

func (s *e2eSuite) requirePRListed(prs []models.PullRequestShort, prID string) {
	s.t.Helper()
	for _, pr := range prs {
		if pr.PullRequestId == prID {
			return
		}
	}
	s.t.Fatalf("pull request %s not found in reviewer list", prID)
}

func (s *e2eSuite) requirePRNotListed(prs []models.PullRequestShort, prID string) {
	s.t.Helper()
	for _, pr := range prs {
		if pr.PullRequestId == prID {
			s.t.Fatalf("pull request %s should not be in reviewer list", prID)
		}
	}
}

func (s *e2eSuite) doJSON(method, path string, payload interface{}) *http.Response {
	s.t.Helper()
	var body *bytes.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		require.NoError(s.t, err)
		body = bytes.NewReader(data)
	} else {
		body = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, s.url(path), body)
	require.NoError(s.t, err)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.client.Do(req)
	require.NoError(s.t, err)
	return resp
}

func decodeJSON(tb testing.TB, resp *http.Response, v interface{}) {
	tb.Helper()
	defer resp.Body.Close()
	require.NoError(tb, json.NewDecoder(resp.Body).Decode(v))
}

func freePort(tb testing.TB) string {
	tb.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(tb, err)
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)
	return strconv.Itoa(addr.Port)
}

type prResponse struct {
	PR *models.PullRequest `json:"pr"`
}

type reassignResponse struct {
	PR         *models.PullRequest `json:"pr"`
	ReplacedBy string              `json:"replaced_by"`
}

type userReviewsResponse struct {
	UserId       string                    `json:"user_id"`
	PullRequests []models.PullRequestShort `json:"pull_requests"`
}

type teamDeactivateResponse struct {
	Result *models.TeamBulkDeactivateResult `json:"result"`
}

var (
	_ service.PullRequestRepository = (*memoryStorage)(nil)
	_ service.UserTeamRepository    = (*memoryStorage)(nil)
)

type memoryStorage struct {
	mu    sync.RWMutex
	prs   map[string]*models.PullRequest
	users map[string]*models.User
	teams map[string]struct{}
}

func newMemoryStorage() *memoryStorage {
	return &memoryStorage{
		prs:   make(map[string]*models.PullRequest),
		users: make(map[string]*models.User),
		teams: make(map[string]struct{}),
	}
}

// --- PullRequestRepository implementation ---

func (m *memoryStorage) SavePullRequest(_ context.Context, pr *models.PullRequest) error {
	if pr == nil {
		return fmt.Errorf("pull request is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prs[pr.PullRequestId] = clonePullRequest(pr)
	return nil
}

func (m *memoryStorage) GetPullRequest(_ context.Context, prID string) (*models.PullRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pr, ok := m.prs[prID]
	if !ok {
		return nil, domain.NewNotFoundError(fmt.Sprintf("pull request %s", prID))
	}
	return clonePullRequest(pr), nil
}

func (m *memoryStorage) FindPullRequestsByReviewer(_ context.Context, reviewerID string) ([]*models.PullRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*models.PullRequest
	for _, pr := range m.prs {
		if containsString(pr.AssignedReviewers, reviewerID) {
			result = append(result, clonePullRequest(pr))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].PullRequestId < result[j].PullRequestId
	})
	return result, nil
}

func (m *memoryStorage) GetAssignmentStats(_ context.Context) (*models.AssignmentStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := &models.AssignmentStats{}
	userCounts := make(map[string]int)

	for _, pr := range m.prs {
		reviewerSet := make(map[string]struct{})
		for _, reviewer := range pr.AssignedReviewers {
			if reviewer == "" {
				continue
			}
			reviewerSet[reviewer] = struct{}{}
			userCounts[reviewer]++
		}
		stats.ByPullRequest = append(stats.ByPullRequest, models.PullRequestAssignmentStat{
			PullRequestId:   pr.PullRequestId,
			PullRequestName: pr.PullRequestName,
			ReviewerCount:   len(reviewerSet),
		})
	}

	for userID, count := range userCounts {
		username := ""
		if user, ok := m.users[userID]; ok {
			username = user.Username
		}
		stats.ByUser = append(stats.ByUser, models.UserAssignmentStat{
			UserId:      userID,
			Username:    username,
			Assignments: count,
		})
	}

	sort.Slice(stats.ByUser, func(i, j int) bool {
		if stats.ByUser[i].Assignments == stats.ByUser[j].Assignments {
			return stats.ByUser[i].UserId < stats.ByUser[j].UserId
		}
		return stats.ByUser[i].Assignments > stats.ByUser[j].Assignments
	})
	sort.Slice(stats.ByPullRequest, func(i, j int) bool {
		if stats.ByPullRequest[i].ReviewerCount == stats.ByPullRequest[j].ReviewerCount {
			return stats.ByPullRequest[i].PullRequestId < stats.ByPullRequest[j].PullRequestId
		}
		return stats.ByPullRequest[i].ReviewerCount > stats.ByPullRequest[j].ReviewerCount
	})

	return stats, nil
}

func (m *memoryStorage) FindOpenPullRequestsByReviewers(_ context.Context, reviewerIDs []string) ([]*models.PullRequest, error) {
	targets := uniqueStrings(reviewerIDs)
	if len(targets) == 0 {
		return nil, nil
	}
	targetSet := make(map[string]struct{}, len(targets))
	for _, id := range targets {
		targetSet[id] = struct{}{}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*models.PullRequest
	for _, pr := range m.prs {
		if pr.Status != models.PullRequestStatusOPEN {
			continue
		}
		for _, reviewer := range pr.AssignedReviewers {
			if _, ok := targetSet[reviewer]; ok {
				result = append(result, clonePullRequest(pr))
				break
			}
		}
	}
	return result, nil
}

func (m *memoryStorage) ApplyBulkTeamReviewerSwaps(_ context.Context, swaps []models.ReviewerSwap, usersToDeactivate []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, swap := range swaps {
		pr, ok := m.prs[swap.PullRequestId]
		if !ok {
			return domain.NewNotFoundError(fmt.Sprintf("pull request %s", swap.PullRequestId))
		}
		for i, reviewer := range pr.AssignedReviewers {
			if reviewer == swap.OldUserId {
				pr.AssignedReviewers[i] = swap.NewUserId
			}
		}
	}

	for _, userID := range uniqueStrings(usersToDeactivate) {
		if user, ok := m.users[userID]; ok {
			user.IsActive = false
		}
	}

	return nil
}

// --- UserTeamRepository implementation ---

func (m *memoryStorage) SaveUser(_ context.Context, user *models.User) error {
	if user == nil {
		return fmt.Errorf("user is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.users[user.UserId]; !exists {
		return domain.NewNotFoundError(fmt.Sprintf("user %s", user.UserId))
	}
	m.users[user.UserId] = cloneUser(user)
	return nil
}

func (m *memoryStorage) GetUser(_ context.Context, userID string) (*models.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	user, ok := m.users[userID]
	if !ok {
		return nil, domain.NewNotFoundError(fmt.Sprintf("user %s", userID))
	}
	return cloneUser(user), nil
}

func (m *memoryStorage) GetAllUsersInTeam(_ context.Context, teamID string) ([]*models.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*models.User
	for _, user := range m.users {
		if user.TeamName == teamID {
			result = append(result, cloneUser(user))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UserId < result[j].UserId
	})
	return result, nil
}

func (m *memoryStorage) SaveTeam(_ context.Context, team *models.Team) error {
	if team == nil {
		return fmt.Errorf("team is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.teams[team.TeamName]; exists {
		return domain.NewTeamExistsError(team.TeamName)
	}
	m.teams[team.TeamName] = struct{}{}
	return nil
}

func (m *memoryStorage) CreateTeamWithMembers(_ context.Context, team *models.Team, users []models.User) error {
	if team == nil {
		return fmt.Errorf("team is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.teams[team.TeamName]; exists {
		return domain.NewTeamExistsError(team.TeamName)
	}
	m.teams[team.TeamName] = struct{}{}
	for _, user := range users {
		u := user
		m.users[u.UserId] = &u
	}
	return nil
}

func (m *memoryStorage) GetTeam(ctx context.Context, teamID string) (*models.Team, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, exists := m.teams[teamID]; !exists {
		return nil, domain.NewNotFoundError(fmt.Sprintf("team %s", teamID))
	}
	users, err := m.GetAllUsersInTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	members := make([]models.TeamMember, 0, len(users))
	for _, user := range users {
		members = append(members, models.TeamMember{
			UserId:   user.UserId,
			Username: user.Username,
			IsActive: user.IsActive,
		})
	}
	return &models.Team{
		TeamName: teamID,
		Members:  members,
	}, nil
}

// --- helpers for in-memory storage ---

func clonePullRequest(pr *models.PullRequest) *models.PullRequest {
	if pr == nil {
		return nil
	}
	cp := *pr
	if pr.CreatedAt != nil {
		val := *pr.CreatedAt
		cp.CreatedAt = &val
	}
	if pr.MergedAt != nil {
		val := *pr.MergedAt
		cp.MergedAt = &val
	}
	cp.AssignedReviewers = append([]string(nil), pr.AssignedReviewers...)
	return &cp
}

func cloneUser(user *models.User) *models.User {
	if user == nil {
		return nil
	}
	cp := *user
	return &cp
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, raw := range values {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		unique[id] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for id := range unique {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}
