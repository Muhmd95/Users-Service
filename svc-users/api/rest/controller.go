package rest

import (
	"svc-users/internal/users"
)

type Controller struct {
	svc *users.Service
}

func NewController(svc *users.Service) *Controller {
	return &Controller{
		svc: svc,
	}
}
