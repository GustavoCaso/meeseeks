//go:build linux

package login

import (
	"context"

	"github.com/GustavoCaso/meeseeks/internal/logger"
)

// TODO
type linuxService struct {
	logger *logger.Logger
}

func getPlatformService(logger *logger.Logger) Service {
	return &linuxService{
		logger: logger,
	}
}

func (d *linuxService) Create(context.Context, ServiceConfig) (Defintion, error) {
	return Defintion(""), nil
}

func (d *linuxService) Enable(context.Context, Defintion) error {
	return nil
}

func (d *linuxService) Disable(context.Context) error {
	return nil
}

func (d *linuxService) Status(context.Context) (ServiceStatus, error) {
	return ServiceStatus{}, nil
}
