package models

// ErrorResponse описывает структуру стандартного ответа с ошибкой.
type ErrorResponse struct {
	Error struct {
		Code    ErrorResponseErrorCode `json:"code"`
		Message string                 `json:"message"`
	} `json:"error"`
}

// ErrorResponseErrorCode задаёт возможные значения кода ошибки.
type ErrorResponseErrorCode string
