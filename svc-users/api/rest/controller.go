package rest

import (
	"svc-users/internal/users"
)

type Controller struct {
	svc           *users.Service
	secureCookies bool
}

func NewController(svc *users.Service, secureCookies bool) *Controller {
	return &Controller{
		svc:           svc,
		secureCookies: secureCookies,
	}
}
