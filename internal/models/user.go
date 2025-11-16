package models

// User описывает сущность пользователя.
type User struct {
	IsActive bool   `json:"is_active"`
	TeamName string `json:"team_name"`
	UserId   string `json:"user_id"`
	Username string `json:"username"`
}

// UserIdQuery задаёт тип идентификатора пользователя.
type UserIdQuery = string

// GetUsersGetReviewParams описывает параметры запроса получения ревью.
type GetUsersGetReviewParams struct {
	// UserId Идентификатор пользователя
	UserId UserIdQuery `form:"user_id" json:"user_id"`
}

// PostUsersSetIsActiveJSONBody описывает тело запроса на изменение активности пользователя.
type PostUsersSetIsActiveJSONBody struct {
	IsActive bool   `json:"is_active"`
	UserId   string `json:"user_id"`
}

// ConvertTmToUser преобразует участника команды в сущность пользователя, добавляя название команды.
func ConvertTmToUser(tm TeamMember, teamName string) User {
	return User{
		IsActive: tm.IsActive,
		TeamName: teamName,
		UserId:   tm.UserId,
		Username: tm.Username,
	}
}
