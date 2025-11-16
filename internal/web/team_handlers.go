package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/AlekseyZapadovnikov/pr-manager/internal/models"
)

type teamAddResponse struct {
	Team *models.Team `json:"team"`
}

type teamDeactivateResponse struct {
	Result *models.TeamBulkDeactivateResult `json:"result"`
}

// handleTeamAdd создаёт команду и сохраняет участников.
func (s *Server) handleTeamAdd(w http.ResponseWriter, r *http.Request) {
	var team models.Team
	fmt.Println(r.Body)
	if err := json.NewDecoder(r.Body).Decode(&team); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid json payload")
		return
	}

	ctx := r.Context()
	err := s.userTeamService.AddTeam(ctx, team)
	if err != nil {
		status, code, msg := mapDomainError(err)
		writeError(w, status, code, msg)
		return
	}

	resp := map[string]*models.Team{"team": &team}
	writeJSON(w, http.StatusCreated, resp)
}

// handleTeamGet возвращает команду по её имени.
func (s *Server) handleTeamGet(w http.ResponseWriter, r *http.Request) {
	teamName := r.URL.Query().Get("team_name")
	if teamName == "" {
		writeError(w, http.StatusBadRequest, "MISSING_PARAM", "team_name is required")
		return
	}

	ctx := r.Context()
	team, err := s.userTeamService.GetTeam(ctx, teamName)
	if err != nil {
		status, code, msg := mapDomainError(err)
		writeError(w, status, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, team)
}

// handleTeamDeactivate массово деактивирует участников команды.
func (s *Server) handleTeamDeactivate(w http.ResponseWriter, r *http.Request) {
	var req models.TeamBulkDeactivateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid json payload")
		return
	}

	req.TeamName = strings.TrimSpace(req.TeamName)
	if req.TeamName == "" {
		writeError(w, http.StatusBadRequest, "MISSING_PARAM", "team_name is required")
		return
	}

	filtered := make([]string, 0, len(req.UserIDs))
	for _, raw := range req.UserIDs {
		id := strings.TrimSpace(raw)
		if id != "" {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		writeError(w, http.StatusBadRequest, "MISSING_PARAM", "at least one user_id is required")
		return
	}

	ctx := r.Context()
	result, err := s.prService.BulkDeactivateTeamMembers(ctx, req.TeamName, filtered)
	if err != nil {
		status, code, msg := mapDomainError(err)
		writeError(w, status, code, msg)
		return
	}

	writeJSON(w, http.StatusOK, teamDeactivateResponse{Result: result})
}
