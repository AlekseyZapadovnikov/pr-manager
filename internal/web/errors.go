package web

import "net/http"

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeError формирует стандартный JSON с кодом и сообщением об ошибке.
func writeError(w http.ResponseWriter, status int, code, message string) {
	resp := errorResponse{
		Error: errorBody{
			Code:    code,
			Message: message,
		},
	}
	writeJSON(w, status, resp)
}

// Возможные значения кода ошибки.
const (
	NOCANDIDATE ErrorResponseErrorCode = "NO_CANDIDATE"
	NOTASSIGNED ErrorResponseErrorCode = "NOT_ASSIGNED"
	NOTFOUND    ErrorResponseErrorCode = "NOT_FOUND"
	PREXISTS    ErrorResponseErrorCode = "PR_EXISTS"
	PRMERGED    ErrorResponseErrorCode = "PR_MERGED"
	TEAMEXISTS  ErrorResponseErrorCode = "TEAM_EXISTS"
)

// ErrorResponseErrorCode описывает код ошибки в ответе.
type ErrorResponseErrorCode string
