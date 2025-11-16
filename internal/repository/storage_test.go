package repository

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v2"

	"github.com/AlekseyZapadovnikov/pr-manager/internal/domain"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/models"
)

var (
	testCtx            = context.Background()
	pullRequestRowCols = []string{"pull_request_id", "pull_request_name", "author_id", "status", "created_at", "merged_at"}
	teamRowCols        = []string{"team_name"}
	teamMemberRowCols  = []string{"user_id", "username", "is_active", "team_name"}
)

const (
	testPullRequestID = "pr-100"
	testTeamID        = "team-x"
)

func newTestStorage(t *testing.T) (*Storage, pgxmock.PgxPoolIface) {
	t.Helper()

	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgx mock: %v", err)
	}

	storage := &Storage{pool: mock}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("there were unmet expectations: %v", err)
		}
		mock.Close()
	})
	return storage, mock
}

func testPullRequest() *models.PullRequest {
	created := time.Date(2024, time.January, 2, 15, 4, 5, 0, time.UTC)
	return &models.PullRequest{
		PullRequestId:   "pr-1",
		PullRequestName: "add docs",
		AuthorId:        "user-1",
		Status:          models.PullRequestStatusOPEN,
		CreatedAt:       &created,
	}
}

func TestStorage_SavePullRequest(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		s := &Storage{}
		if err := s.SavePullRequest(testCtx, nil); err == nil {
			t.Fatal("expected error for nil pr")
		}
	})

	t.Run("too many reviewers", func(t *testing.T) {
		s := &Storage{}
		pr := testPullRequest()
		pr.AssignedReviewers = []string{"u1", "u2", "u3"}
		if err := s.SavePullRequest(testCtx, pr); err == nil {
			t.Fatal("expected error when reviewers > 2")
		}
	})

	t.Run("begin error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		pr := testPullRequest()
		mock.ExpectBegin().WillReturnError(errors.New("fail begin"))

		if err := s.SavePullRequest(testCtx, pr); err == nil || !regexp.MustCompile("begin tx").MatchString(err.Error()) {
			t.Fatalf("expected begin error, got %v", err)
		}
	})

	t.Run("upsert error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		pr := testPullRequest()
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO pull_requests")).
			WithArgs(pr.PullRequestId, pr.PullRequestName, pr.AuthorId, string(pr.Status), pr.CreatedAt, pr.MergedAt).
			WillReturnError(errors.New("fail insert"))
		mock.ExpectRollback()

		if err := s.SavePullRequest(testCtx, pr); err == nil || !regexp.MustCompile("upsert pull_requests").MatchString(err.Error()) {
			t.Fatalf("expected upsert error, got %v", err)
		}
	})

	t.Run("delete reviewers error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		pr := testPullRequest()
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO pull_requests")).
			WithArgs(pr.PullRequestId, pr.PullRequestName, pr.AuthorId, string(pr.Status), pr.CreatedAt, pr.MergedAt).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec(regexp.QuoteMeta("DELETE FROM pull_request_reviewers")).
			WithArgs(pr.PullRequestId).
			WillReturnError(errors.New("delete failed"))
		mock.ExpectRollback()

		if err := s.SavePullRequest(testCtx, pr); err == nil || !regexp.MustCompile("delete pull_request_reviewers").MatchString(err.Error()) {
			t.Fatalf("expected delete error, got %v", err)
		}
	})

	t.Run("insert reviewer error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		pr := testPullRequest()
		pr.AssignedReviewers = []string{"reviewer-1"}
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO pull_requests")).
			WithArgs(pr.PullRequestId, pr.PullRequestName, pr.AuthorId, string(pr.Status), pr.CreatedAt, pr.MergedAt).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec(regexp.QuoteMeta("DELETE FROM pull_request_reviewers")).
			WithArgs(pr.PullRequestId).
			WillReturnResult(pgxmock.NewResult("DELETE", 1))
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO pull_request_reviewers")).
			WithArgs(pr.PullRequestId, "reviewer-1").
			WillReturnError(errors.New("insert reviewer failed"))
		mock.ExpectRollback()

		if err := s.SavePullRequest(testCtx, pr); err == nil || !regexp.MustCompile("insert pull_request_reviewer").MatchString(err.Error()) {
			t.Fatalf("expected reviewer insert error, got %v", err)
		}
	})

	t.Run("commit error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		pr := testPullRequest()
		pr.AssignedReviewers = []string{"one"}
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO pull_requests")).
			WithArgs(pr.PullRequestId, pr.PullRequestName, pr.AuthorId, string(pr.Status), pr.CreatedAt, pr.MergedAt).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec(regexp.QuoteMeta("DELETE FROM pull_request_reviewers")).
			WithArgs(pr.PullRequestId).
			WillReturnResult(pgxmock.NewResult("DELETE", 1))
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO pull_request_reviewers")).
			WithArgs(pr.PullRequestId, "one").
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectCommit().WillReturnError(errors.New("commit fail"))
		mock.ExpectRollback()

		if err := s.SavePullRequest(testCtx, pr); err == nil || !regexp.MustCompile("commit tx").MatchString(err.Error()) {
			t.Fatalf("expected commit error, got %v", err)
		}
	})

	t.Run("success with unique reviewers", func(t *testing.T) {
		s, mock := newTestStorage(t)
		pr := testPullRequest()
		pr.AssignedReviewers = []string{"first", "second"}

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO pull_requests")).
			WithArgs(pr.PullRequestId, pr.PullRequestName, pr.AuthorId, string(pr.Status), pr.CreatedAt, pr.MergedAt).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec(regexp.QuoteMeta("DELETE FROM pull_request_reviewers")).
			WithArgs(pr.PullRequestId).
			WillReturnResult(pgxmock.NewResult("DELETE", 0))
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO pull_request_reviewers")).
			WithArgs(pr.PullRequestId, "first").
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO pull_request_reviewers")).
			WithArgs(pr.PullRequestId, "second").
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectCommit()

		if err := s.SavePullRequest(testCtx, pr); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success skips duplicates", func(t *testing.T) {
		s, mock := newTestStorage(t)
		pr := testPullRequest()
		pr.AssignedReviewers = []string{"first", "first"}

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO pull_requests")).
			WithArgs(pr.PullRequestId, pr.PullRequestName, pr.AuthorId, string(pr.Status), pr.CreatedAt, pr.MergedAt).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec(regexp.QuoteMeta("DELETE FROM pull_request_reviewers")).
			WithArgs(pr.PullRequestId).
			WillReturnResult(pgxmock.NewResult("DELETE", 0))
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO pull_request_reviewers")).
			WithArgs(pr.PullRequestId, "first").
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectCommit()

		if err := s.SavePullRequest(testCtx, pr); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestStorage_GetPullRequestQueryError(t *testing.T) {
	s, mock := newTestStorage(t)
	mock.ExpectQuery("SELECT\\s+pull_request_id").
		WithArgs(testPullRequestID).
		WillReturnError(errors.New("query fail"))

	if _, err := s.GetPullRequest(testCtx, testPullRequestID); err == nil || !regexp.MustCompile("query pull_requests").MatchString(err.Error()) {
		t.Fatalf("expected query error, got %v", err)
	}
}

func TestStorage_GetPullRequestNotFound(t *testing.T) {
	s, mock := newTestStorage(t)
	mock.ExpectQuery("SELECT\\s+pull_request_id").
		WithArgs(testPullRequestID).
		WillReturnRows(pgxmock.NewRows(pullRequestRowCols))

	_, err := s.GetPullRequest(testCtx, testPullRequestID)
	if err == nil || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestStorage_GetPullRequestScanError(t *testing.T) {
	s, mock := newTestStorage(t)
	rows := pgxmock.NewRows(pullRequestRowCols).
		AddRow(testPullRequestID, 123, "author", "OPEN", nil, nil).
		RowError(0, errors.New("scan fail"))
	mock.ExpectQuery("SELECT\\s+pull_request_id").
		WithArgs(testPullRequestID).
		WillReturnRows(rows)

	if _, err := s.GetPullRequest(testCtx, testPullRequestID); err == nil || !regexp.MustCompile("scan pull_requests").MatchString(err.Error()) {
		t.Fatalf("expected scan error, got %v", err)
	}
}

func TestStorage_GetPullRequestReviewersQueryError(t *testing.T) {
	s, mock := newTestStorage(t)
	now := time.Now().UTC()
	mock.ExpectQuery("SELECT\\s+pull_request_id").
		WithArgs(testPullRequestID).
		WillReturnRows(pgxmock.NewRows(pullRequestRowCols).
			AddRow(testPullRequestID, "name", "author", "OPEN", &now, nil))
	mock.ExpectQuery("SELECT\\s+user_id\\s+FROM\\s+pull_request_reviewers").
		WithArgs(testPullRequestID).
		WillReturnError(errors.New("reviewer query"))

	if _, err := s.GetPullRequest(testCtx, testPullRequestID); err == nil || !regexp.MustCompile("query pull_request_reviewers").MatchString(err.Error()) {
		t.Fatalf("expected reviewer query error, got %v", err)
	}
}

func TestStorage_GetPullRequestReviewersScanError(t *testing.T) {
	s, mock := newTestStorage(t)
	now := time.Now().UTC()
	mock.ExpectQuery("SELECT\\s+pull_request_id").
		WithArgs(testPullRequestID).
		WillReturnRows(pgxmock.NewRows(pullRequestRowCols).
			AddRow(testPullRequestID, "name", "author", "OPEN", &now, nil))
	mock.ExpectQuery("SELECT\\s+user_id\\s+FROM\\s+pull_request_reviewers").
		WithArgs(testPullRequestID).
		WillReturnRows(pgxmock.NewRows([]string{"user_id"}).
			AddRow("user-1").
			RowError(0, errors.New("scan reviewer")))

	if _, err := s.GetPullRequest(testCtx, testPullRequestID); err == nil || !regexp.MustCompile("scan reviewer").MatchString(err.Error()) {
		t.Fatalf("expected reviewer scan error, got %v", err)
	}
}

func TestStorage_GetPullRequestSuccess(t *testing.T) {
	s, mock := newTestStorage(t)
	created := time.Now().UTC()
	merged := created.Add(time.Hour)
	mock.ExpectQuery("SELECT\\s+pull_request_id").
		WithArgs(testPullRequestID).
		WillReturnRows(pgxmock.NewRows(pullRequestRowCols).
			AddRow(testPullRequestID, "name", "author", "OPEN", &created, &merged))
	mock.ExpectQuery("SELECT\\s+user_id\\s+FROM\\s+pull_request_reviewers").
		WithArgs(testPullRequestID).
		WillReturnRows(pgxmock.NewRows([]string{"user_id"}).
			AddRow("a").
			AddRow("b"))

	pr, err := s.GetPullRequest(testCtx, testPullRequestID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr.PullRequestId != testPullRequestID || pr.PullRequestName != "name" || pr.AuthorId != "author" {
		t.Fatalf("unexpected pr data: %+v", pr)
	}
	if len(pr.AssignedReviewers) != 2 || pr.AssignedReviewers[0] != "a" || pr.AssignedReviewers[1] != "b" {
		t.Fatalf("unexpected reviewers: %+v", pr.AssignedReviewers)
	}
	if pr.CreatedAt == nil || pr.MergedAt == nil {
		t.Fatal("expected timestamps to be set")
	}
}

func TestStorage_FindPullRequestsByReviewer(t *testing.T) {
	const reviewerID = "rev-1"
	columns := []string{"pull_request_id", "pull_request_name", "author_id", "status", "created_at", "merged_at", "reviewers"}

	t.Run("query error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		mock.ExpectQuery("SELECT\\s+p\\.pull_request_id").
			WithArgs(reviewerID).
			WillReturnError(errors.New("query fail"))

		if _, err := s.FindPullRequestsByReviewer(testCtx, reviewerID); err == nil || !regexp.MustCompile("query find by reviewer").MatchString(err.Error()) {
			t.Fatalf("expected query error, got %v", err)
		}
	})

	t.Run("scan error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		created := time.Now().UTC()
		var merged *time.Time
		rows := pgxmock.NewRows(columns).
			AddRow("pr", "name", "author", "OPEN", &created, merged, []string{}).
			RowError(0, errors.New("scan fail"))
		mock.ExpectQuery("SELECT\\s+p\\.pull_request_id").
			WithArgs(reviewerID).
			WillReturnRows(rows)

		if _, err := s.FindPullRequestsByReviewer(testCtx, reviewerID); err == nil || !regexp.MustCompile("scan find by reviewer").MatchString(err.Error()) {
			t.Fatalf("expected scan error, got %v", err)
		}
	})

	t.Run("rows error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		created := time.Now().UTC()
		var merged *time.Time
		rows := pgxmock.NewRows(columns).
			AddRow("pr", "name", "author", "OPEN", &created, merged, []string{"a"}).
			RowError(1, errors.New("rows err"))
		mock.ExpectQuery("SELECT\\s+p\\.pull_request_id").
			WithArgs(reviewerID).
			WillReturnRows(rows)

		if _, err := s.FindPullRequestsByReviewer(testCtx, reviewerID); err == nil || !regexp.MustCompile("rows error").MatchString(err.Error()) {
			t.Fatalf("expected rows error, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		s, mock := newTestStorage(t)
		created := time.Now().UTC()
		var merged *time.Time
		rows := pgxmock.NewRows(columns).
			AddRow("pr", "name", "author", "OPEN", &created, merged, []string{"x", "y"})
		mock.ExpectQuery("SELECT\\s+p\\.pull_request_id").
			WithArgs(reviewerID).
			WillReturnRows(rows)

		list, err := s.FindPullRequestsByReviewer(testCtx, reviewerID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(list) != 1 {
			t.Fatalf("expected 1 pr, got %d", len(list))
		}
		if list[0].PullRequestId != "pr" || len(list[0].AssignedReviewers) != 2 {
			t.Fatalf("unexpected pr data: %+v", list[0])
		}
	})
}

func TestStorage_SaveUser(t *testing.T) {
	t.Run("nil user", func(t *testing.T) {
		s := &Storage{}
		if err := s.SaveUser(testCtx, nil); err == nil {
			t.Fatal("expected error for nil user")
		}
	})

	t.Run("exec error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		user := &models.User{UserId: "u", Username: "name", IsActive: true}
		mock.ExpectExec("INSERT\\s+INTO\\s+users").
			WithArgs(user.UserId, user.Username, user.IsActive, user.TeamName).
			WillReturnError(errors.New("exec fail"))

		if err := s.SaveUser(testCtx, user); err == nil || !regexp.MustCompile("upsert user").MatchString(err.Error()) {
			t.Fatalf("expected exec error, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		s, mock := newTestStorage(t)
		user := &models.User{UserId: "u", Username: "name", IsActive: true, TeamName: "team"}
		mock.ExpectExec("INSERT\\s+INTO\\s+users").
			WithArgs(user.UserId, user.Username, user.IsActive, user.TeamName).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		if err := s.SaveUser(testCtx, user); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestStorage_GetUser(t *testing.T) {
	const userID = "user-1"
	columns := []string{"user_id", "username", "is_active", "team_name"}

	t.Run("query error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		mock.ExpectQuery("SELECT\\s+user_id").
			WithArgs(userID).
			WillReturnError(errors.New("query fail"))

		if _, err := s.GetUser(testCtx, userID); err == nil || !regexp.MustCompile("query GetUser").MatchString(err.Error()) {
			t.Fatalf("expected query error, got %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		s, mock := newTestStorage(t)
		mock.ExpectQuery("SELECT\\s+user_id").
			WithArgs(userID).
			WillReturnRows(pgxmock.NewRows(columns))

		_, err := s.GetUser(testCtx, userID)
		if err == nil || !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("expected not found error, got %v", err)
		}
	})

	t.Run("scan error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		rows := pgxmock.NewRows(columns).
			AddRow(userID, "name", true, "team").
			RowError(0, errors.New("scan fail"))
		mock.ExpectQuery("SELECT\\s+user_id").
			WithArgs(userID).
			WillReturnRows(rows)

		if _, err := s.GetUser(testCtx, userID); err == nil || !regexp.MustCompile("scan GetUser").MatchString(err.Error()) {
			t.Fatalf("expected scan error, got %v", err)
		}
	})

	t.Run("success with nil team", func(t *testing.T) {
		s, mock := newTestStorage(t)
		var teamName *string
		rows := pgxmock.NewRows(columns).
			AddRow(userID, "name", true, teamName)
		mock.ExpectQuery("SELECT\\s+user_id").
			WithArgs(userID).
			WillReturnRows(rows)

		user, err := s.GetUser(testCtx, userID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.TeamName != "" {
			t.Fatalf("expected empty team, got %q", user.TeamName)
		}
	})
}

func TestStorage_GetAllUsersInTeam(t *testing.T) {
	const (
		teamID       = "team-1"
		mockTeamName = "team"
	)
	columns := []string{"user_id", "username", "is_active", "team_name"}

	t.Run("query error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		mock.ExpectQuery("SELECT\\s+user_id").
			WithArgs(teamID).
			WillReturnError(errors.New("query fail"))

		if _, err := s.GetAllUsersInTeam(testCtx, teamID); err == nil || !regexp.MustCompile("query getAllUsersInTeam").MatchString(err.Error()) {
			t.Fatalf("expected query error, got %v", err)
		}
	})

	t.Run("scan error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		teamName := mockTeamName
		rows := pgxmock.NewRows(columns).
			AddRow("user", "name", true, &teamName).
			RowError(0, errors.New("scan fail"))
		mock.ExpectQuery("SELECT\\s+user_id").
			WithArgs(teamID).
			WillReturnRows(rows)

		if _, err := s.GetAllUsersInTeam(testCtx, teamID); err == nil || !regexp.MustCompile("scan getAllUsersInTeam").MatchString(err.Error()) {
			t.Fatalf("expected scan error, got %v", err)
		}
	})

	t.Run("rows error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		teamName := mockTeamName
		rows := pgxmock.NewRows(columns).
			AddRow("user", "name", true, &teamName).
			RowError(1, errors.New("rows fail"))
		mock.ExpectQuery("SELECT\\s+user_id").
			WithArgs(teamID).
			WillReturnRows(rows)

		if _, err := s.GetAllUsersInTeam(testCtx, teamID); err == nil || !regexp.MustCompile("rows error getAllUsersInTeam").MatchString(err.Error()) {
			t.Fatalf("expected rows error, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		s, mock := newTestStorage(t)
		teamName := mockTeamName
		rows := pgxmock.NewRows(columns).
			AddRow("user", "name", true, &teamName).
			AddRow("user2", "name2", false, nil)
		mock.ExpectQuery("SELECT\\s+user_id").
			WithArgs(teamID).
			WillReturnRows(rows)

		users, err := s.GetAllUsersInTeam(testCtx, teamID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(users) != 2 || users[1].TeamName != "" {
			t.Fatalf("unexpected users data: %+v", users)
		}
	})
}

func TestStorage_SaveTeam(t *testing.T) {
	t.Run("nil team", func(t *testing.T) {
		s := &Storage{}
		if err := s.SaveTeam(testCtx, nil); err == nil {
			t.Fatal("expected error for nil team")
		}
	})

	t.Run("exec error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		team := &models.Team{TeamName: "devs"}
		mock.ExpectExec("INSERT\\s+INTO\\s+teams").
			WithArgs(team.TeamName).
			WillReturnError(errors.New("exec fail"))

		if err := s.SaveTeam(testCtx, team); err == nil || !regexp.MustCompile("upsert team").MatchString(err.Error()) {
			t.Fatalf("expected exec error, got %v", err)
		}
	})

	t.Run("team exists", func(t *testing.T) {
		s, mock := newTestStorage(t)
		team := &models.Team{TeamName: "devs"}
		mock.ExpectExec("INSERT\\s+INTO\\s+teams").
			WithArgs(team.TeamName).
			WillReturnResult(pgxmock.NewResult("INSERT", 0))

		if err := s.SaveTeam(testCtx, team); err == nil || !errors.Is(err, domain.ErrTeamExists) {
			t.Fatalf("expected team exists error, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		s, mock := newTestStorage(t)
		team := &models.Team{TeamName: "devs"}
		mock.ExpectExec("INSERT\\s+INTO\\s+teams").
			WithArgs(team.TeamName).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		if err := s.SaveTeam(testCtx, team); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestStorage_CreateTeamWithMembers(t *testing.T) {
	team := &models.Team{TeamName: "backend"}
	users := []models.User{
		{UserId: "u1", Username: "name1", IsActive: true, TeamName: "backend"},
		{UserId: "u2", Username: "name2", IsActive: false, TeamName: "backend"},
	}

	t.Run("nil team", func(t *testing.T) {
		s := &Storage{}
		if err := s.CreateTeamWithMembers(testCtx, nil, nil); err == nil {
			t.Fatal("expected error for nil team")
		}
	})

	t.Run("begin error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		mock.ExpectBegin().WillReturnError(errors.New("begin fail"))

		if err := s.CreateTeamWithMembers(testCtx, team, users); err == nil || !regexp.MustCompile("begin tx").MatchString(err.Error()) {
			t.Fatalf("expected begin error, got %v", err)
		}
	})

	t.Run("insert team error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		mock.ExpectBegin()
		mock.ExpectExec("INSERT\\s+INTO\\s+teams").
			WithArgs(team.TeamName).
			WillReturnError(errors.New("insert fail"))
		mock.ExpectRollback()

		if err := s.CreateTeamWithMembers(testCtx, team, users); err == nil || !regexp.MustCompile("insert team").MatchString(err.Error()) {
			t.Fatalf("expected insert error, got %v", err)
		}
	})

	t.Run("team exists", func(t *testing.T) {
		s, mock := newTestStorage(t)
		mock.ExpectBegin()
		mock.ExpectExec("INSERT\\s+INTO\\s+teams").
			WithArgs(team.TeamName).
			WillReturnResult(pgxmock.NewResult("INSERT", 0))
		mock.ExpectRollback()

		if err := s.CreateTeamWithMembers(testCtx, team, users); err == nil || !errors.Is(err, domain.ErrTeamExists) {
			t.Fatalf("expected team exists error, got %v", err)
		}
	})

	t.Run("user upsert error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		mock.ExpectBegin()
		mock.ExpectExec("INSERT\\s+INTO\\s+teams").
			WithArgs(team.TeamName).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec("INSERT\\s+INTO\\s+users").
			WithArgs(users[0].UserId, users[0].Username, users[0].IsActive, users[0].TeamName).
			WillReturnError(errors.New("user fail"))
		mock.ExpectRollback()

		if err := s.CreateTeamWithMembers(testCtx, team, users); err == nil || !regexp.MustCompile("upsert user u1").MatchString(err.Error()) {
			t.Fatalf("expected user upsert error, got %v", err)
		}
	})

	t.Run("commit error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		mock.ExpectBegin()
		mock.ExpectExec("INSERT\\s+INTO\\s+teams").
			WithArgs(team.TeamName).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		for _, user := range users {
			mock.ExpectExec("INSERT\\s+INTO\\s+users").
				WithArgs(user.UserId, user.Username, user.IsActive, user.TeamName).
				WillReturnResult(pgxmock.NewResult("INSERT", 1))
		}
		mock.ExpectCommit().WillReturnError(errors.New("commit fail"))
		mock.ExpectRollback()

		if err := s.CreateTeamWithMembers(testCtx, team, users); err == nil || !regexp.MustCompile("commit tx").MatchString(err.Error()) {
			t.Fatalf("expected commit error, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		s, mock := newTestStorage(t)
		mock.ExpectBegin()
		mock.ExpectExec("INSERT\\s+INTO\\s+teams").
			WithArgs(team.TeamName).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		for _, user := range users {
			mock.ExpectExec("INSERT\\s+INTO\\s+users").
				WithArgs(user.UserId, user.Username, user.IsActive, user.TeamName).
				WillReturnResult(pgxmock.NewResult("INSERT", 1))
		}
		mock.ExpectCommit()

		if err := s.CreateTeamWithMembers(testCtx, team, users); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestStorage_GetTeamQueryError(t *testing.T) {
	s, mock := newTestStorage(t)
	mock.ExpectQuery("SELECT\\s+team_name").
		WithArgs(testTeamID).
		WillReturnError(errors.New("query fail"))

	if _, err := s.GetTeam(testCtx, testTeamID); err == nil || !regexp.MustCompile("query GetTeam").MatchString(err.Error()) {
		t.Fatalf("expected query error, got %v", err)
	}
}

func TestStorage_GetTeamNotFound(t *testing.T) {
	s, mock := newTestStorage(t)
	mock.ExpectQuery("SELECT\\s+team_name").
		WithArgs(testTeamID).
		WillReturnRows(pgxmock.NewRows(teamRowCols))

	_, err := s.GetTeam(testCtx, testTeamID)
	if err == nil || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestStorage_GetTeamScanError(t *testing.T) {
	s, mock := newTestStorage(t)
	rows := pgxmock.NewRows(teamRowCols).
		AddRow(testTeamID).
		RowError(0, errors.New("scan fail"))
	mock.ExpectQuery("SELECT\\s+team_name").
		WithArgs(testTeamID).
		WillReturnRows(rows)

	if _, err := s.GetTeam(testCtx, testTeamID); err == nil || !regexp.MustCompile("scan GetTeam").MatchString(err.Error()) {
		t.Fatalf("expected scan error, got %v", err)
	}
}

func TestStorage_GetTeamMembersQueryError(t *testing.T) {
	s, mock := newTestStorage(t)
	mock.ExpectQuery("SELECT\\s+team_name").
		WithArgs(testTeamID).
		WillReturnRows(pgxmock.NewRows(teamRowCols).AddRow(testTeamID))
	mock.ExpectQuery("SELECT\\s+user_id").
		WithArgs(testTeamID).
		WillReturnError(errors.New("user query fail"))

	if _, err := s.GetTeam(testCtx, testTeamID); err == nil || !regexp.MustCompile("get members for team").MatchString(err.Error()) {
		t.Fatalf("expected members query error, got %v", err)
	}
}

func TestStorage_GetTeamSuccess(t *testing.T) {
	s, mock := newTestStorage(t)
	mock.ExpectQuery("SELECT\\s+team_name").
		WithArgs(testTeamID).
		WillReturnRows(pgxmock.NewRows(teamRowCols).AddRow(testTeamID))
	teamName1 := testTeamID
	teamName2 := testTeamID
	mock.ExpectQuery("SELECT\\s+user_id").
		WithArgs(testTeamID).
		WillReturnRows(pgxmock.NewRows(teamMemberRowCols).
			AddRow("u1", "name1", true, &teamName1).
			AddRow("u2", "name2", false, &teamName2))

	team, err := s.GetTeam(testCtx, testTeamID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if team.TeamName != testTeamID || len(team.Members) != 2 {
		t.Fatalf("unexpected team data: %+v", team)
	}
	if team.Members[0].UserId != "u1" || !team.Members[0].IsActive || team.Members[0].Username != "name1" {
		t.Fatalf("unexpected member data: %+v", team.Members[0])
	}
	if team.Members[1].UserId != "u2" || team.Members[1].IsActive {
		t.Fatalf("unexpected member data: %+v", team.Members[1])
	}
}

func TestStorage_GetAssignmentStats(t *testing.T) {
	userCols := []string{"user_id", "username", "assignments"}
	prCols := []string{"pull_request_id", "pull_request_name", "reviewer_count"}

	t.Run("user query error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		mock.ExpectQuery("SELECT\\s+r\\.user_id").WillReturnError(errors.New("boom"))

		if _, err := s.GetAssignmentStats(testCtx); err == nil || !regexp.MustCompile("query user assignment stats").MatchString(err.Error()) {
			t.Fatalf("expected user query error, got %v", err)
		}
	})

	t.Run("user scan error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		rows := pgxmock.NewRows(userCols).
			AddRow("user-1", 123, int64(5))
		mock.ExpectQuery("SELECT\\s+r\\.user_id").WillReturnRows(rows)

		if _, err := s.GetAssignmentStats(testCtx); err == nil || !regexp.MustCompile("scan user assignment stats").MatchString(err.Error()) {
			t.Fatalf("expected scan error, got %v", err)
		}
	})

	t.Run("pr query error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		userRows := pgxmock.NewRows(userCols).
			AddRow("user-1", "Alice", int64(2))
		mock.ExpectQuery("SELECT\\s+r\\.user_id").WillReturnRows(userRows)
		mock.ExpectQuery("SELECT\\s+p\\.pull_request_id").WillReturnError(errors.New("boom"))

		if _, err := s.GetAssignmentStats(testCtx); err == nil || !regexp.MustCompile("query pr assignment stats").MatchString(err.Error()) {
			t.Fatalf("expected pr query error, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		s, mock := newTestStorage(t)
		userRows := pgxmock.NewRows(userCols).
			AddRow("user-1", "Alice", int64(2)).
			AddRow("user-2", "Bob", int64(1))
		mock.ExpectQuery("SELECT\\s+r\\.user_id").WillReturnRows(userRows)

		prRows := pgxmock.NewRows(prCols).
			AddRow("pr-1", "Docs", int64(2)).
			AddRow("pr-2", "API", int64(1))
		mock.ExpectQuery("SELECT\\s+p\\.pull_request_id").WillReturnRows(prRows)

		stats, err := s.GetAssignmentStats(testCtx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(stats.ByUser) != 2 || stats.ByUser[0].UserId != "user-1" || stats.ByUser[0].Assignments != 2 {
			t.Fatalf("unexpected user stats: %+v", stats.ByUser)
		}
		if len(stats.ByPullRequest) != 2 || stats.ByPullRequest[1].PullRequestName != "API" || stats.ByPullRequest[1].ReviewerCount != 1 {
			t.Fatalf("unexpected pr stats: %+v", stats.ByPullRequest)
		}
	})
}

func TestStorage_FindOpenPullRequestsByReviewers(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		s := &Storage{}
		prs, err := s.FindOpenPullRequestsByReviewers(testCtx, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if prs != nil {
			t.Fatalf("expected nil result, got %v", prs)
		}
	})

	t.Run("query error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		mock.ExpectQuery(regexp.QuoteMeta("SELECT ")).
			WithArgs(pgxmock.AnyArg()).
			WillReturnError(errors.New("boom"))

		_, err := s.FindOpenPullRequestsByReviewers(testCtx, []string{"u1"})
		if err == nil || !regexp.MustCompile("query open pull requests").MatchString(err.Error()) {
			t.Fatalf("expected query error, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		s, mock := newTestStorage(t)
		now := time.Now()
		rows := pgxmock.NewRows([]string{"pull_request_id", "pull_request_name", "author_id", "status", "created_at", "merged_at", "reviewers"}).
			AddRow("pr-1", "add feature", "author-1", "OPEN", &now, nil, []string{"u1", "u2"})
		mock.ExpectQuery(regexp.QuoteMeta("SELECT ")).
			WithArgs(pgxmock.AnyArg()).
			WillReturnRows(rows)

		prs, err := s.FindOpenPullRequestsByReviewers(testCtx, []string{"u1", "u2"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(prs) != 1 || prs[0].PullRequestId != "pr-1" {
			t.Fatalf("unexpected prs: %+v", prs)
		}
	})
}

func TestStorage_ApplyBulkTeamReviewerSwaps(t *testing.T) {
	t.Run("no work", func(t *testing.T) {
		s := &Storage{}
		if err := s.ApplyBulkTeamReviewerSwaps(testCtx, nil, nil); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("delete error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("DELETE FROM pull_request_reviewers")).
			WithArgs("pr-1", "old").
			WillReturnError(errors.New("boom"))
		mock.ExpectRollback()

		err := s.ApplyBulkTeamReviewerSwaps(testCtx, []models.ReviewerSwap{
			{PullRequestId: "pr-1", OldUserId: "old", NewUserId: "new"},
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "delete reviewer") {
			t.Fatalf("expected delete error, got %v", err)
		}
	})

	t.Run("update error", func(t *testing.T) {
		s, mock := newTestStorage(t)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("DELETE FROM pull_request_reviewers")).
			WithArgs("pr-1", "old").
			WillReturnResult(pgxmock.NewResult("DELETE", 1))
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO pull_request_reviewers")).
			WithArgs("pr-1", "new").
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec(regexp.QuoteMeta("UPDATE users SET is_active = false")).
			WithArgs(pgxmock.AnyArg()).
			WillReturnError(errors.New("boom"))
		mock.ExpectRollback()

		err := s.ApplyBulkTeamReviewerSwaps(testCtx, []models.ReviewerSwap{
			{PullRequestId: "pr-1", OldUserId: "old", NewUserId: "new"},
		}, []string{"u1"})
		if err == nil || !strings.Contains(err.Error(), "bulk deactivate users") {
			t.Fatalf("expected update error, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		s, mock := newTestStorage(t)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("DELETE FROM pull_request_reviewers")).
			WithArgs("pr-1", "old").
			WillReturnResult(pgxmock.NewResult("DELETE", 1))
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO pull_request_reviewers")).
			WithArgs("pr-1", "new").
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec(regexp.QuoteMeta("UPDATE users SET is_active = false")).
			WithArgs(pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 2))
		mock.ExpectCommit()

		err := s.ApplyBulkTeamReviewerSwaps(testCtx, []models.ReviewerSwap{
			{PullRequestId: "pr-1", OldUserId: "old", NewUserId: "new"},
		}, []string{"u1", "u2"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestConvertUserToTeamMember(t *testing.T) {
	u := &models.User{
		UserId:   "u",
		Username: "name",
		IsActive: true,
		TeamName: "team",
	}
	member := convertUserToTeamMember(u)
	if member.UserId != u.UserId || member.Username != u.Username || member.IsActive != u.IsActive {
		t.Fatalf("unexpected conversion result: %+v", member)
	}
}

func TestStorage_Close(t *testing.T) {
	s := &Storage{}
	s.Close() // should not panic without pool
}
