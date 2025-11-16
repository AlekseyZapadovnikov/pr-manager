package web

import (
	"net/http"

	"github.com/AlekseyZapadovnikov/pr-manager/internal/models"
)

// handleAssignmentStats возвращает агрегированную статистику выдачи ревьюеров.
func (s *Server) handleAssignmentStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats, err := s.prService.AssignmentStats(ctx)
	if err != nil {
		status, code, msg := mapDomainError(err)
		writeError(w, status, code, msg)
		return
	}

	if stats == nil {
		stats = &models.AssignmentStats{}
	}

	writeJSON(w, http.StatusOK, stats)
}
