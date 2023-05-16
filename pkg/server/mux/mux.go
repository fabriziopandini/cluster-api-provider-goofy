package mux

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/pkg/errors"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	// considering 1 API and 3 etcd members for each cluster, the range below allows 2k clusters

	minPort = 20000
	maxPort = 28000
)

type Mux struct {
	host      string
	minPort   int // TODO: move port management to a port range type
	maxPort   int
	portIndex int

	server   http.Server
	services map[string]*Service

	lock sync.RWMutex
}

func NewServer(host string, tlsConfig *tls.Config, handler http.Handler) *Mux {
	return &Mux{
		host:      host,
		minPort:   minPort,
		maxPort:   maxPort,
		portIndex: minPort,
		server: http.Server{
			Handler:   handler,
			TLSConfig: tlsConfig,
		},
		services: map[string]*Service{},
		lock:     sync.RWMutex{},
	}
}

func (s *Mux) AddService() (*Service, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	port, err := s.getFreePortNoLock()
	if err != nil {
		return nil, err
	}

	service := &Service{
		host: s.host,
		port: port,
	}
	if _, ok := s.services[service.Address()]; ok {
		return nil, errors.Errorf("Service with Address %s already exists", service.Address())
	}
	s.services[service.Address()] = service

	return service, nil
}

func (s *Mux) StartService(address string) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	service, ok := s.services[address]
	if !ok {
		return errors.Errorf("Service with Address %s does not exists", address)
	}

	if service.listener != nil {
		return nil
	}

	listener, err := net.Listen("tcp", service.HostPort())
	if err != nil {
		return errors.Wrapf(err, "failed to create listener for Service with Address %s", address)
	}
	service.listener = listener

	var startErr error
	startCh := make(chan struct{})
	go func() {
		startCh <- struct{}{}
		if err := s.server.ServeTLS(service.listener, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			startErr = err
		}
	}()

	select {
	case <-startCh:
		time.Sleep(250 * time.Microsecond)
	}
	return startErr
}

func (s *Mux) Shutdown(ctx context.Context) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	// NOTE: this closes all the listeners
	return s.server.Shutdown(ctx)
}

func (s *Mux) getFreePortNoLock() (int, error) {
	port := s.portIndex
	if port > s.maxPort {
		return -1, errors.Errorf("no more free ports in the %d-%d range", minPort, maxPort)
	}
	s.portIndex++

	return port, nil
}

type Service struct {
	host string
	port int

	listener net.Listener
}

func (s *Service) Host() string {
	return s.host
}

func (s *Service) Port() int {
	return s.port
}

func (s *Service) Address() string {
	return fmt.Sprintf("https://%s", s.HostPort())
}

func (s *Service) HostPort() string {
	return net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))
}
