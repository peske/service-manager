package service_manager

import (
	"context"
	"fmt"
)

type ServiceStatus int

const (
	ServiceStatusUnknown ServiceStatus = iota
	ServiceCreated
	ServiceRunning
	ServiceShutDown
)

func (s ServiceStatus) String() string {
	switch s {
	case ServiceStatusUnknown:
		return "Unknown"
	case ServiceCreated:
		return "Created"
	case ServiceRunning:
		return "Running"
	case ServiceShutDown:
		return "Shut Down"
	default:
		return fmt.Sprintf("Unknown status %d", int(s))
	}
}

type Service interface {
	// Name returns the service name.
	Name() string

	// Required indicates if the service is required (if it has to run).
	// The returned value indicates what will happen if the service fails:
	// - if `true`, other services will be shut down, and the process will exit.
	// - if `false`, failure will be ignored, and other services will proceed.
	Required() bool

	// Start starts the service.
	// `ctx` is the context;
	// `statusReport` is status changing callback function.
	Start(ctx context.Context, statusReport func(string, ServiceStatus))

	// Shutdown initiates the shutdown of the service.
	Shutdown()
}
