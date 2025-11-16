package models

// Team описывает сущность команды.
type Team struct {
	Members  []TeamMember `json:"members"`
	TeamName string       `json:"team_name"`
}

// TeamNameQuery задаёт тип имени команды.
type TeamNameQuery = string

// GetTeamGetParams описывает параметры запроса получения команды.
type GetTeamGetParams struct {
	// TeamName Уникальное имя команды
	TeamName TeamNameQuery `form:"team_name" json:"team_name"`
}

// TeamMember описывает участника команды.
type TeamMember struct {
	IsActive bool   `json:"is_active"`
	UserId   string `json:"user_id"`
	Username string `json:"username"`
}

// ConvertUserToTeamMember формирует представление участника команды из сущности пользователя.
func ConvertUserToTeamMember(user User) TeamMember {
	return TeamMember{
		IsActive: user.IsActive,
		UserId:   user.UserId,
		Username: user.Username,
	}
}
