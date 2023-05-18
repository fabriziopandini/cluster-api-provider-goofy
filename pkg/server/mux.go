package server

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	cmanager "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/manager"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server/api"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server/etcd"
	"github.com/pkg/errors"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"k8s.io/apimachinery/pkg/util/sets"
	"net"
	"net/http"
	"sigs.k8s.io/cluster-api/util/certs"
	"strings"
	"sync"
	"time"
)

const (
	// This range allow for 4k clusters, which is 4 times the goal we have in mind for
	// the first iteration of stress tests

	minPort = 20000
	maxPort = 24000
)

// WorkloadClustersMux implement a server that handles request for multiple workload clusters.
// Each workload clusters will get its own listener, serving on a dedicated port, eg.
// wkl-cluster-1 >> :20000, wkl-cluster-2 >> :20001 etc.
// Each workload clusters will act both as API server and as etcd for the cluster; the
// WorkloadClustersMux is also responsible for handling certificates for each of the above use cases.
type WorkloadClustersMux struct {
	host      string
	minPort   int // TODO: move port management to a port range type
	maxPort   int
	portIndex int

	manager cmanager.Manager // TODO: figure out if we can have a smaller interface (GetResourceGroup, GetSchema)

	server                   http.Server
	workloadClusterListeners map[string]*WorkloadClusterListener
	indexByHostPort          map[string]string

	lock sync.RWMutex
}

// NewWorkloadClustersMux returns a WorkloadClustersMux that handles request for multiple workload clusters.
func NewWorkloadClustersMux(manager cmanager.Manager, host string) *WorkloadClustersMux {
	mux := &WorkloadClustersMux{
		host:                     host,
		minPort:                  minPort,
		maxPort:                  maxPort,
		portIndex:                minPort,
		manager:                  manager,
		workloadClusterListeners: map[string]*WorkloadClusterListener{},
		indexByHostPort:          map[string]string{},
		lock:                     sync.RWMutex{},
	}

	mux.server = http.Server{
		// Use an handler that can serve either API server calls or etcd calls.
		Handler: mux.mixedHandler(),
		// Use a TLS config that select certificates for a specific cluster depending on
		// the request being processed (API server and etcd have different certificates).
		TLSConfig: &tls.Config{
			GetCertificate: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
				return mux.getCertificate(info)
			},
		},
	}

	return mux
}

// mixedHandler returns an handler that can serve either API server calls or etcd calls.
func (s *WorkloadClustersMux) mixedHandler() http.Handler {
	// Prepare a function that can identify which workloadCluster/resourceGroup a
	// request targets to.
	resourceGroupResolver := func(host string) (string, error) {
		s.lock.RLock()
		defer s.lock.RUnlock()
		wclName, ok := s.indexByHostPort[host]
		if !ok {
			return "", errors.Errorf("failed to get workloadClusterListener for host %s", host)
		}
		return wclName, nil
	}

	// build the handlers for API server and etcd.
	apiHandler := api.NewAPIServerHandler(s.manager, resourceGroupResolver)
	etcdHandler := etcd.NewEtcdServerHandler(s.manager, resourceGroupResolver)

	// Creates the mixed handler combining the two above depending on
	// the type of request being processed
	mixedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("content-type"), "application/grpc") {
			etcdHandler.ServeHTTP(w, r)
			return
		}
		apiHandler.ServeHTTP(w, r)
	})

	return h2c.NewHandler(mixedHandler, &http2.Server{})
}

// getCertificate selects certificates for a specific cluster depending on the request being processed
// (API server and etcd have different certificates).
func (s *WorkloadClustersMux) getCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	// Identify which workloadCluster/resourceGroup a request targets to.
	hostPort := info.Conn.LocalAddr().String()
	wclName, ok := s.indexByHostPort[hostPort]
	if !ok {

	}

	// Gets the listener config for the target workloadCluster.
	wcl, ok := s.workloadClusterListeners[wclName]
	if !ok {

	}

	// If the request targets a specific etcd member, use the corresponding server certificates
	// NOTE: the port forward call to etcd sets the server name to the name of the targeted etcd pod,
	// which is also the name of the corresponding etcd member.
	if wcl.etcdMembers.Has(info.ServerName) {
		return wcl.etcdServingCertificates[info.ServerName], nil
	}

	// Otherwise we assume the request targets the API server.
	return wcl.apiServerServingCertificate, nil
}

// InitWorkloadClusterListener initialize a WorkloadClusterListener by reserving a port for it.
// Note: The listener will be started when the first API server will be added.
func (s *WorkloadClustersMux) InitWorkloadClusterListener(wclName string) (*WorkloadClusterListener, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	port, err := s.getFreePortNoLock()
	if err != nil {
		return nil, err
	}

	wcl := &WorkloadClusterListener{
		scheme:                  s.manager.GetScheme(),
		host:                    s.host,
		port:                    port,
		apiServers:              sets.New[string](),
		etcdMembers:             sets.New[string](),
		etcdServingCertificates: map[string]*tls.Certificate{},
	}
	s.workloadClusterListeners[wclName] = wcl
	s.indexByHostPort[wcl.HostPort()] = wclName

	return wcl, nil
}

// AddAPIServer mimics adding an API server instance behind the WorkloadClusterListener.
// When the first API server instance is added the serving certificates and the admin certificate
// for tests are generated, and the listener is started.
func (s *WorkloadClustersMux) AddAPIServer(wclName, podName string, caCert *x509.Certificate, caKey *rsa.PrivateKey) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	wcl, ok := s.workloadClusterListeners[wclName]
	if !ok {
		return errors.Errorf("workloadClusterListener with name %s must be initialized before adding an APIserver", wclName)
	}
	wcl.apiServers.Insert(podName)

	// TODO: check if cert/key are already set, they should match
	wcl.apiServerCaCertificate = caCert
	wcl.apiServerCaKey = caKey

	// Generate Serving certificates for the API server instance
	// NOTE: There is only a server certificate for the all the API server instances (kubeadm
	// instead creates one for each API server pod, but we don't need this because we are
	// always accessing API servers via a unique endpoint)
	if wcl.apiServerServingCertificate == nil {
		config := apiServerCertificateConfig(wcl.host)
		cert, key, err := newCertAndKey(caCert, caKey, config)
		if err != nil {
			return errors.Wrapf(err, "failed to create serving certificate for API server %s", podName)
		}

		certificate, err := tls.X509KeyPair(certs.EncodeCertPEM(cert), certs.EncodePrivateKeyPEM(key))
		if err != nil {
			return errors.Wrapf(err, "failed to create X509KeyPair API server %s", podName)
		}
		wcl.apiServerServingCertificate = &certificate
	}

	// Generate admin certificates to be used for accessing the API server.
	// NOTE: this is used for tests because CAPI creates its own.
	if wcl.adminCertificate == nil {
		config := adminClientCertificateConfig()
		cert, key, err := newCertAndKey(caCert, caKey, config)
		if err != nil {
			return errors.Wrapf(err, "failed to create admin certificate for API server %s", podName)
		}

		wcl.adminCertificate = cert
		wcl.adminKey = key
	}

	// Start the listener for the API server.
	// NOTE: There is only one listener for all the API server instances; the same listener will act
	// as a port forward target too.
	if wcl.listener != nil {
		return nil
	}

	l, err := net.Listen("tcp", wcl.HostPort())
	if err != nil {
		return errors.Wrapf(err, "failed to start WorkloadClusterListener %s, %s", wclName, wcl.HostPort())
	}
	wcl.listener = l

	var startErr error
	startCh := make(chan struct{})
	go func() {
		startCh <- struct{}{}
		if err := s.server.ServeTLS(wcl.listener, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			startErr = err
		}
	}()

	select {
	case <-startCh:
		time.Sleep(250 * time.Microsecond)
	}
	return startErr
}

// AddEtcdMember mimics adding an etcd Member behind the WorkloadClusterListener;
// every etcd member gets a dedicated serving certificate, so it will be possible to serve port forward requests
// to a specific etcd pod/member.
func (s *WorkloadClustersMux) AddEtcdMember(wclName, podName string, caCert *x509.Certificate, caKey *rsa.PrivateKey) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	wcl, ok := s.workloadClusterListeners[wclName]
	if !ok {
		return errors.Errorf("workloadClusterListener with name %s must be initialized before adding an APIserver", wclName)
	}
	wcl.etcdMembers.Insert(podName)

	// Generate Serving certificates for the etcdMember
	if _, ok := wcl.etcdServingCertificates[podName]; !ok {
		config := etcdServerCertificateConfig(podName, wcl.host)
		cert, key, err := newCertAndKey(caCert, caKey, config)
		if err != nil {
			return errors.Wrapf(err, "failed to create serving certificate for etcd member %s", podName)
		}

		certificate, err := tls.X509KeyPair(certs.EncodeCertPEM(cert), certs.EncodePrivateKeyPEM(key))
		if err != nil {

		}
		wcl.etcdServingCertificates[podName] = &certificate
	}

	return nil
}

func (s *WorkloadClustersMux) Shutdown(ctx context.Context) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	// NOTE: this closes all the listeners
	return s.server.Shutdown(ctx)
}

func (s *WorkloadClustersMux) getFreePortNoLock() (int, error) {
	port := s.portIndex
	if port > s.maxPort {
		return -1, errors.Errorf("no more free ports in the %d-%d range", minPort, maxPort)
	}
	s.portIndex++

	// TODO: check the port is actually free. If not try the next one

	return port, nil
}
