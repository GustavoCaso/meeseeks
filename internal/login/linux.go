//go:build linux

package login

import "github.com/GustavoCaso/meeseeks/internal/logger"

// TODO
type linuxService struct {
	logger *logger.Logger
}

func getPlatformService(logger *logger.Logger) Service {
	return &linuxService{
		logger: logger,
	}
}

func (d *linuxService) Create(ServiceConfig) (Defintion, error) {
	return Defintion(""), nil
}

func (d *linuxService) Enable(Defintion) error {
	return nil
}

func (d *linuxService) Disable() error {
	return nil
}

func (d *linuxService) Status() (ServiceStatus, error) {
	return ServiceStatus{}, nil
}
