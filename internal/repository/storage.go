package repository

import (
	"context"
	"fmt"

	"github.com/AlekseyZapadovnikov/pr-manager/conf"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBPool описывает минимальный интерфейс пула подключений к PostgreSQL.
type DBPool interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (pgx.Tx, error)
	Close()
}

// Storage инкапсулирует пул подключений и предоставляет его репозиториям.
type Storage struct {
	pool DBPool
}

// NewStorage создаёт пул подключений к PostgreSQL и проверяет соединение.
func NewStorage(ctx context.Context, cfg *conf.DbConf) (*Storage, error) {
	connStr := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.Name,
	)

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	// РџСЂРѕРІРµСЂСЏРµРј РїРѕРґРєР»СЋС‡РµРЅРёРµ
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Storage{pool: pool}, nil
}

// Close закрывает пул подключений, когда он больше не нужен.
func (s *Storage) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}
