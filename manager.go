package service_manager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
)

type ServiceManager struct {
	statusCallback func(string, ServiceStatus)
	timeout        time.Duration
	logger         zerolog.Logger
	ctx            context.Context
	ctxCancel      func()
	sigterm        chan os.Signal

	mu                  sync.Mutex
	services            map[string]Service
	shutdown            chan bool
	shutdownInitiatedBy string
}

// NewServiceManager creates and returns a new ServiceManager instance.
func NewServiceManager(statusCallback func(string, ServiceStatus), logger zerolog.Logger,
	shutdownTimeout time.Duration) *ServiceManager {
	sm := &ServiceManager{
		statusCallback: statusCallback,
		timeout:        shutdownTimeout,
		logger:         logger,
		sigterm:        make(chan os.Signal),
		services:       make(map[string]Service),
	}

	// Create cancellable context:
	sm.ctx, sm.ctxCancel = context.WithCancel(context.Background())

	// Subscribe for SIGTERM notification, and listen:
	signal.Notify(sm.sigterm, os.Interrupt, syscall.SIGTERM)
	go sm.sigtermListener()

	sm.logger.Info().Msg("ServiceManager created.")
	return sm
}

// Context returns the main context used by the manager.
func (s *ServiceManager) Context() context.Context {
	return s.ctx
}

// RegisterAndStart registers and starts a new service.
// The service will be started in a separate thread, so this
// method will not block.
func (s *ServiceManager) RegisterAndStart(service Service) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.shutdown != nil {
		return errors.New("ServiceManager is shutdown")
	}

	name := service.Name()
	if name == "" {
		return errors.New("service name is empty")
	}

	if s.services[name] != nil {
		return fmt.Errorf("service with name '%s' already exists", service.Name())
	}

	s.services[name] = service
	s.logger.Info().Msgf("Service '%s' added.", name)

	go service.Start(s.ctx, s.statCb)
	return nil
}

// Run blocks the execution until underlying services are running.
func (s *ServiceManager) Run() error {
	// Wait until the services are running:
	<-s.ctx.Done()

	// Wait until all the services are shut down or the timeout period elapses:
	to := time.NewTimer(s.timeout)
	select {
	case <-to.C:
		s.logger.Warn().Msg("Merciful shutdown timeout expired.")
	case <-s.shutdown:
		to.Stop()
		s.logger.Info().Msg("All the services shutdown mercifully.")
	}

	s.mu.Lock()
	by := s.shutdownInitiatedBy
	rem := len(s.services)
	s.mu.Unlock()

	if by == "" {
		os.Exit(130)
	}

	if rem < 1 {
		return nil
	}
	return fmt.Errorf("%d services not shutdown", rem)
}

func (s *ServiceManager) sigtermListener() {
	select {
	case <-s.sigterm:
		// SIGTERM received, so handle and exit.
		s.serviceShutdown("")
	case <-s.ctx.Done():
		// Something else caused the shutdown, so just exit.
	}
}

func (s *ServiceManager) statCb(name string, status ServiceStatus) {
	s.logger.Info().Msgf("Service '%s' status '%s'", name, status.String())
	if status == ServiceShutDown {
		go s.serviceShutdown(name)
	}
	if s.statusCallback != nil {
		s.statusCallback(name, status)
	}
}

func (s *ServiceManager) serviceShutdown(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sd := false
	if name == "" {
		sd = true
	} else if svc := s.services[name]; svc != nil {
		delete(s.services, name)
		sd = svc.Required() || len(s.services) < 1
	} else {
		s.logger.Warn().Msgf("Service '%s' not found.")
		return
	}

	if s.shutdown != nil {
		// The shutdown procedure is already initiated.
		if len(s.services) == 0 {
			s.shutdown <- true
		}
		return
	}

	if !sd {
		// Should not initiate the shutdown procedure.
		return
	}

	s.shutdownInitiatedBy = name
	if name == "" {
		s.logger.Info().Msg("SIGINT initiated the shutdown procedure.")
	} else {
		s.logger.Info().Msgf("Service '%s' initiated the shutdown procedure.", name)
	}

	s.shutdown = make(chan bool, 1)
	s.ctxCancel()

	if len(s.services) < 1 {
		s.shutdown <- true
	} else {
		for _, v := range s.services {
			go v.Shutdown()
		}
	}
}
