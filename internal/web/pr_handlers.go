package web

import (
	"encoding/json"
	"net/http"

	"github.com/AlekseyZapadovnikov/pr-manager/internal/models"
)

type prResp struct {
	PR *models.PullRequest `json:"pr"`
}

// handlePRCreate принимает JSON-запрос создания PR и проксирует его в сервис.
func (s *Server) handlePRCreate(w http.ResponseWriter, r *http.Request) {
	var p models.PostPullRequestCreateJSONBody
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid json payload")
		return
	}
	if p.PullRequestId == "" || p.PullRequestName == "" || p.AuthorId == "" {
		writeError(w, http.StatusBadRequest, "MISSING_PARAM", "pull_request_id, pull_request_name and author_id are required")
		return
	}

	ctx := r.Context()
	pr, err := s.prService.CreatePullRequest(ctx, p)
	if err != nil {
		status, code, msg := mapDomainError(err)
		writeError(w, status, code, msg)
		return
	}

	writeJSON(w, http.StatusCreated, prResp{PR: pr})
}

// handlePRMerge подтверждает слияние PR и возвращает обновлённые данные.
func (s *Server) handlePRMerge(w http.ResponseWriter, r *http.Request) {
	var p models.PostPullRequestMergeJSONBody
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid json payload")
		return
	}
	if p.PullRequestId == "" {
		writeError(w, http.StatusBadRequest, "MISSING_PARAM", "pull_request_id is required")
		return
	}

	ctx := r.Context()
	pr, err := s.prService.Merge(ctx, p)
	if err != nil {
		status, code, msg := mapDomainError(err)
		writeError(w, status, code, msg)
		return
	}

	writeJSON(w, http.StatusOK, prResp{PR: pr})
}

type reassignResponse struct {
	PR         *models.PullRequest `json:"pr"`
	ReplacedBy string              `json:"replaced_by"`
}

// handlePRReassign переназначает ревьюеров PR и сообщает замену.
func (s *Server) handlePRReassign(w http.ResponseWriter, r *http.Request) {
	var p models.PostPullRequestReassignJSONBody
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid json payload")
		return
	}
	prId, oldUserId := p.PullRequestId, p.OldUserId
	if prId == "" || oldUserId == "" {
		writeError(w, http.StatusBadRequest, "MISSING_PARAM", "pull_request_id and old_user_id are required")
		return
	}

	ctx := r.Context()
	res, err := s.prService.Reassign(ctx, oldUserId, prId)
	if err != nil {
		status, code, msg := mapDomainError(err)
		writeError(w, status, code, msg)
		return
	}

	writeJSON(w, http.StatusOK, reassignResponse{
		PR:         res.PR,
		ReplacedBy: res.ReplacedBy,
	})
}

type getUserReviewsResp struct {
	UserId       string                    `json:"user_id"`
	PullRequests []models.PullRequestShort `json:"pull_requests"`
}
