package models

import "time"

// PullRequest описывает модель pull request.
type PullRequest struct {
	// AssignedReviewers идентификаторы ревьюеров (0..2).
	AssignedReviewers []string          `json:"assigned_reviewers"`
	AuthorId          string            `json:"author_id"`
	CreatedAt         *time.Time        `json:"createdAt"`
	MergedAt          *time.Time        `json:"mergedAt"`
	PullRequestId     string            `json:"pull_request_id"`
	PullRequestName   string            `json:"pull_request_name"`
	Status            PullRequestStatus `json:"status"`
}

// PostPullRequestCreateJSONBody описывает тело запроса создания PR.
type PostPullRequestCreateJSONBody struct {
	AuthorId        string `json:"author_id"`
	PullRequestId   string `json:"pull_request_id"`
	PullRequestName string `json:"pull_request_name"`
}

// PostPullRequestMergeJSONBody описывает параметры запроса на Merge.
type PostPullRequestMergeJSONBody struct {
	PullRequestId string `json:"pull_request_id"`
}

// PostPullRequestReassignJSONBody описывает тело запроса на переназначение ревьюера.
type PostPullRequestReassignJSONBody struct {
	OldUserId     string `json:"old_user_id"`
	PullRequestId string `json:"pull_request_id"`
}

// ===== Short pullReq ====================================

// PullRequestShort задаёт укороченное представление PR.
type PullRequestShort struct {
	AuthorId        string                 `json:"author_id"`
	PullRequestId   string                 `json:"pull_request_id"`
	PullRequestName string                 `json:"pull_request_name"`
	Status          PullRequestShortStatus `json:"status"`
}

// PullRequestShortStatus описывает возможные статусы укороченного PR.
type PullRequestShortStatus string

// PullRequestStatus описывает статусы полного PR.
type PullRequestStatus string

// Возможные значения PullRequestStatus.
const (
	PullRequestStatusMERGED PullRequestStatus = "MERGED"
	PullRequestStatusOPEN   PullRequestStatus = "OPEN"
)

// Возможные значения PullRequestShortStatus.
const (
	PullRequestShortStatusMERGED PullRequestShortStatus = "MERGED"
	PullRequestShortStatusOPEN   PullRequestShortStatus = "OPEN"
)
