package web

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/AlekseyZapadovnikov/pr-manager/conf"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/domain"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)


type Server struct {
	Address string
	server  *http.Server

	router          *chi.Mux
	prService       PullRequestService
	userTeamService UserTeamService
}

// New конструирует HTTP-сервер на базе chi и регистрирует все маршруты.
func New(cfg conf.HttpServConf, pr PullRequestService, user UserTeamService) *Server {
	servAdres := cfg.GetAddress()
	mux := chi.NewMux()
	srv := &Server{
		Address:         servAdres,
		router:          mux,
		prService:       pr,
		userTeamService: user,
	}
	srv.server = &http.Server{
		Addr:    servAdres,
		Handler: mux,
	}

	srv.setupRoutes()

	return srv
}

// Start запускает HTTP-сервер и блокирует поток до остановки.
func (s *Server) Start() error {
	slog.Info("server starting", "address", s.server.Addr)
	return s.server.ListenAndServe()
}

// setupRoutes настраивает middleware, статику и HTTP-маршруты.
func (s *Server) setupRoutes() {
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)

	// Обслуживаем статические файлы.
	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "./internal/web/static"
	}
	s.router.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

	// Отдаём index.html для корневого пути.
	s.router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, staticDir+"/index.html")
	})

	// Простейший health-check.
	s.router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Маршруты управления командами.
	s.router.Post("/team/add", s.handleTeamAdd)
	s.router.Get("/team/get", s.handleTeamGet)
	s.router.Post("/team/deactivateUsers", s.handleTeamDeactivate)

	// Маршруты управления пользователями.
	s.router.Post("/users/setIsActive", s.handleSetUserActivity)
	s.router.Get("/users/getReview", s.handleGetUserReviews)

	// Маршруты для Pull Request.
	s.router.Post("/pullRequest/create", s.handlePRCreate)
	s.router.Post("/pullRequest/merge", s.handlePRMerge)
	s.router.Post("/pullRequest/reassign", s.handlePRReassign)

	// Маршрут статистики.
	s.router.Get("/stats/assignments", s.handleAssignmentStats)
}

// Shutdown останавливает HTTP-сервер с таймаутом на корректное завершение.
func (s *Server) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

// ---------- утилитарные функции ----------

// writeJSON сериализует структуру в JSON-ответ с нужным статусом.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// mapDomainError переводит доменные ошибки в HTTP-статусы и коды ответа.
func mapDomainError(err error) (status int, code, msg string) {
	if err == nil {
		return http.StatusOK, "", ""
	}

	switch {
	case errors.Is(err, domain.ErrTeamExists):
		return http.StatusBadRequest, "TEAM_EXISTS", err.Error()
	case errors.Is(err, domain.ErrPRExists):
		return http.StatusConflict, "PR_EXISTS", err.Error()
	case errors.Is(err, domain.ErrPRMerged):
		return http.StatusConflict, "PR_MERGED", err.Error()
	case errors.Is(err, domain.ErrNotAssigned):
		return http.StatusConflict, "NOT_ASSIGNED", err.Error()
	case errors.Is(err, domain.ErrNoCandidate):
		return http.StatusConflict, "NO_CANDIDATE", err.Error()
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, "NOT_FOUND", err.Error()
	case errors.Is(err, domain.ErrUnauthorized):
		return http.StatusUnauthorized, "UNAUTHORIZED", err.Error()
	default:
		slog.Warn("unmapped domain error", "err", err.Error())
		return http.StatusInternalServerError, "INTERNAL_ERROR", err.Error()
	}
}
