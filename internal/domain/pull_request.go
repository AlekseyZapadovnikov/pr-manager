package domain

import "github.com/AlekseyZapadovnikov/pr-manager/internal/models"

// ReassignResponse описывает результат переназначения ревьюера в доменной модели.
type ReassignResponse struct {
	PR         *models.PullRequest
	ReplacedBy string
}
