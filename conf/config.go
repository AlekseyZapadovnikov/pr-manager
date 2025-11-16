package conf

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"

	"github.com/go-playground/validator/v10"
)

var (
	configValidator = newConfigValidator()
	numberRegex     = regexp.MustCompile(`^\d+$`)
)

type Config struct {
	HTTPServConf HttpServConf `json:"httpServer" validate:"required"`
	DBConf       DbConf       `json:"dataBase" validate:"required"`
}

type HttpServConf struct {
	Host    string `json:"host" validate:"required"`
	Port    string `json:"port" validate:"required,min=1,max=65535"`
	BaseURL string `json:"baseURL"`
}

// GetAddress возвращает строку host:port для запуска HTTP-сервера.
func (s *HttpServConf) GetAddress() string {
	return fmt.Sprintf("%s:%s", s.Host, s.Port)
}

type DbConf struct {
	Host     string `json:"host" validate:"required"`
	Port     string `json:"port" validate:"required,is-number"`
	User     string `json:"user" validate:"required"`
	Password string `json:"password" validate:"required"`
	Name     string `json:"name" validate:"required"`
}

// MustLoad читает файл конфигурации, применяет значения из окружения и валидирует структуру.
func MustLoad(path string) *Config {
	data, err := os.ReadFile(path)
	if err != nil {
		panic("could not read config file: " + err.Error())
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		panic("could not parse config file: " + err.Error())
	}

	applyEnvOverrides(&cfg)

	if err := configValidator.Struct(cfg); err != nil {
		panic("invalid config: " + err.Error())
	}

	return &cfg
}

// applyEnvOverrides подменяет поля конфигурации значениями из переменных окружения.
func applyEnvOverrides(cfg *Config) {
	override := func(key string, target *string) {
		if val := os.Getenv(key); val != "" {
			*target = val
		}
	}

	override("HTTP_HOST", &cfg.HTTPServConf.Host)
	override("HTTP_PORT", &cfg.HTTPServConf.Port)
	override("HTTP_BASE_URL", &cfg.HTTPServConf.BaseURL)

	override("DB_HOST", &cfg.DBConf.Host)
	override("DB_PORT", &cfg.DBConf.Port)
	override("DB_USER", &cfg.DBConf.User)
	override("DB_PASSWORD", &cfg.DBConf.Password)
	override("DB_NAME", &cfg.DBConf.Name)
}

// newConfigValidator настраивает валидатор и регистрирует пользовательские проверки.
func newConfigValidator() *validator.Validate {
	v := validator.New()
	if err := v.RegisterValidation("is-number", func(fl validator.FieldLevel) bool {
		return numberRegex.MatchString(fl.Field().String())
	}); err != nil {
		panic("failed to register is-number validation: " + err.Error())
	}
	return v
}
