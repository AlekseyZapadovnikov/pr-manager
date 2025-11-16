-- Таблица команд
CREATE TABLE teams (
    team_name TEXT PRIMARY KEY
);

-- Таблица пользователей
CREATE TABLE users (
    user_id   TEXT PRIMARY KEY,
    username  TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    team_name TEXT REFERENCES teams(team_name) ON DELETE SET NULL
);

-- Таблица Pull Request'ов
CREATE TABLE pull_requests (
    pull_request_id   TEXT PRIMARY KEY,
    pull_request_name TEXT NOT NULL,
    author_id         TEXT NOT NULL REFERENCES users(user_id) ON DELETE RESTRICT,
    status            TEXT NOT NULL,
    created_at        TIMESTAMPTZ,
    merged_at         TIMESTAMPTZ
);

-- Назначенные ревьюеры (0–2 на PR)
CREATE TABLE pull_request_reviewers (
    pull_request_id TEXT NOT NULL REFERENCES pull_requests(pull_request_id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL REFERENCES users(user_id) ON DELETE RESTRICT,
    PRIMARY KEY (pull_request_id, user_id)
);