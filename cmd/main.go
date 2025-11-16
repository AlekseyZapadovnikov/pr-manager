package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/AlekseyZapadovnikov/pr-manager/conf"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/repository"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/service"
	"github.com/AlekseyZapadovnikov/pr-manager/internal/web"
)

// main конфигурирует сервис, поднимает хранилище, сервисы и HTTP-сервер, а затем управляет их жизненным циклом.
func main() {
	// Берём путь до конфигурации из окружения либо используем значение по умолчанию.
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "./conf/config.json"
	}

	// Загружаем конфигурацию.
	config := conf.MustLoad(cfgPath)
	slog.Info("Configuration loaded successfully", "config_path", cfgPath)
	slog.Info("Database configuration", "host", config.DBConf.Host, "port", config.DBConf.Port, "user", config.DBConf.User, "database", config.DBConf.Name)

	fmt.Println("DB Config:", config.DBConf.Name, config.DBConf.User, config.DBConf.Host, config.DBConf.Port)
	// Создаём подключение к базе данных.
	ctx := context.Background()
	DBase, err := repository.NewStorage(ctx, &config.DBConf)
	if err != nil {
		slog.Error("Database initialization failed", "error", err)
		os.Exit(1)
	}
	slog.Info("Database storage initialized successfully")

	// Создаём менеджер пользователей (реализация UserTeamService).
	userManager := service.NewUserManager(DBase)
	slog.Info("User manager created successfully")

	// Создаём менеджер Pull Request (реализация PullRequestService).
	prManager := &service.PullRequestManager{}
	prManager = prManager.NewPullRequestService(DBase, userManager)
	slog.Info("Pull request manager created successfully")

	// Поднимаем HTTP-сервер.
	server := web.New(config.HTTPServConf, prManager, userManager)
	slog.Info("HTTP server created successfully", "address", server.Address)

	// Запускаем сервер в отдельной горутине.
	go func() {
		if err := server.Start(); err != nil && err.Error() != "http: Server closed" {
			slog.Error("Server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	slog.Info("PR Manager service started successfully", "address", server.Address)

	// Ожидаем сигнал остановки для плавного завершения работы.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("Shutting down server...")

	// Выполняем корректное завершение сервера с тайм-аутом.
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("Server exited properly")
}
