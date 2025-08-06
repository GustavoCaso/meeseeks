//go:build linux

package login

// TODO
type linuxService struct{}

func getPlatformService() Service {
	return &linuxService{}
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
