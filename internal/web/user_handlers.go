package web

import (
	"encoding/json"
	"net/http"

	"github.com/AlekseyZapadovnikov/pr-manager/internal/models"
)

type setUserResp struct {
	User *models.User `json:"user"`
}

// handleSetUserActivity меняет признак активности пользователя.
func (s *Server) handleSetUserActivity(w http.ResponseWriter, r *http.Request) {
	var p models.PostUsersSetIsActiveJSONBody
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid json payload")
		return
	}
	if p.UserId == "" {
		writeError(w, http.StatusBadRequest, "MISSING_PARAM", "user_id is required")
		return
	}

	user, err := s.userTeamService.SetUserActivity(p.UserId, p.IsActive)
	if err != nil {
		status, code, msg := mapDomainError(err)
		writeError(w, status, code, msg)
		return
	}

	writeJSON(w, http.StatusOK, setUserResp{User: user})
}

// handleGetUserReviews возвращает список PR, назначенных ревьюеру.
func (s *Server) handleGetUserReviews(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_PARAM", "user_id is required")
		return
	}

	ctx := r.Context()
	prs, err := s.prService.ListForReviewer(ctx, userID)
	if err != nil {
		status, code, msg := mapDomainError(err)
		writeError(w, status, code, msg)
		return
	}

	writeJSON(w, http.StatusOK, getUserReviewsResp{
		UserId:       userID,
		PullRequests: prs,
	})
}
