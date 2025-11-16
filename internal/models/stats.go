package models

// AssignmentStats содержит счётчики назначений по пользователям и PR.
type AssignmentStats struct {
	ByUser        []UserAssignmentStat        `json:"by_user"`
	ByPullRequest []PullRequestAssignmentStat `json:"by_pull_request"`
}

// UserAssignmentStat показывает, сколько активных назначений у конкретного пользователя.
type UserAssignmentStat struct {
	UserId      string `json:"user_id"`
	Username    string `json:"username"`
	Assignments int    `json:"assignments"`
}

// PullRequestAssignmentStat показывает, сколько ревьюеров закреплено за PR.
type PullRequestAssignmentStat struct {
	PullRequestId   string `json:"pull_request_id"`
	PullRequestName string `json:"pull_request_name"`
	ReviewerCount   int    `json:"reviewer_count"`
}
