package models

// TeamBulkDeactivateRequest описывает запрос на массовую деактивацию членов команды.
type TeamBulkDeactivateRequest struct {
	TeamName string   `json:"team_name"`
	UserIDs  []string `json:"user_ids"`
}

// TeamBulkDeactivateResult содержит результат массовой деактивации.
type TeamBulkDeactivateResult struct {
	TeamName      string               `json:"team_name"`
	Deactivated   []string             `json:"deactivated"`
	Reassignments []TeamPRReassignment `json:"reassignments"`
}

// TeamPRReassignment хранит информацию о каждой замене ревьюера для PR.
type TeamPRReassignment struct {
	PullRequestId string                `json:"pull_request_id"`
	Replacements  []ReviewerReplacement `json:"replacements"`
}

// ReviewerReplacement описывает одну замену ревьюера.
type ReviewerReplacement struct {
	OldUserId string `json:"old_user_id"`
	NewUserId string `json:"new_user_id"`
}

// ReviewerSwap используется репозиториями и сервисами для транзакционных обновлений.
type ReviewerSwap struct {
	PullRequestId string
	OldUserId     string
	NewUserId     string
}
