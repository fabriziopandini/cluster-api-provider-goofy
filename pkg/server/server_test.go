package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server/api"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server/etcd"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server/mux"
	proxy2 "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server/portforward/proxy"
	"github.com/fabriziopandini/cluster-api-provider-goofy/resources/pki"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"strings"
	"testing"
	"time"
)

var scheme = runtime.NewScheme()

func init() {
	_ = metav1.AddMetaToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
}

func TestServer(t *testing.T) {
	serverCert, err := tls.X509KeyPair(pki.APIServerCertificateData(), pki.APIServerKeyData())
	require.NoError(t, err)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
	}

	apiHandler := api.NewAPIServerHandler(scheme)
	etcHandler := etcd.NewEtcdServerHandler()

	mixedHandler := newMixedHandler(apiHandler, etcHandler)

	server := mux.NewServer("127.0.0.1", tlsConfig, mixedHandler)

	service1, err := server.AddService()
	require.NoError(t, err)

	err = server.StartService(service1.Address())
	require.NoError(t, err)

	c, _, err := getClient(t, service1)
	require.NoError(t, err)

	nl := &corev1.NodeList{}
	err = c.List(context.TODO(), nl)
	require.NoError(t, err)
}

func TestServer2(t *testing.T) {
	serverCert, err := tls.X509KeyPair(pki.APIServerCertificateData(), pki.APIServerKeyData())
	require.NoError(t, err)

	tlsConfig := &tls.Config{
		// Certificates: []tls.Certificate{serverCert},
		GetCertificate: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
			fmt.Printf("local addr %s\n", info.Conn.LocalAddr())
			return &serverCert, nil
		},
	}

	apiHandler := api.NewAPIServerHandler(scheme)
	etcHandler := etcd.NewEtcdServerHandler()

	mixedHandler := newMixedHandler(apiHandler, etcHandler)

	server := mux.NewServer("127.0.0.1", tlsConfig, mixedHandler)

	service1, err := server.AddService()
	require.NoError(t, err)

	err = server.StartService(service1.Address())
	require.NoError(t, err)

	service2, err := server.AddService()
	require.NoError(t, err)

	err = server.StartService(service2.Address())
	require.NoError(t, err)

	_, restConfig, err := getClient(t, service1)
	require.NoError(t, err)

	clientCert, err := tls.X509KeyPair(pki.AdminCertificateData(), pki.AdminKeyData())
	require.NoError(t, err)

	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(pki.CACertificateData())

	p := proxy2.Proxy{
		Kind:       "pods",
		Namespace:  metav1.NamespaceSystem,
		KubeConfig: restConfig,
		Port:       service2.Port(),
	}

	dialer, err := proxy2.NewDialer(p)
	require.NoError(t, err)

	etcdClient1, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"kubernetes"},
		DialTimeout: 2 * time.Second,

		DialOptions: []grpc.DialOption{
			grpc.WithBlock(), // block until the underlying connection is up
			grpc.WithContextDialer(dialer.DialContextWithAddr),
		},
		TLS: &tls.Config{
			RootCAs:      caPool,
			Certificates: []tls.Certificate{clientCert},
			MinVersion:   tls.VersionTLS12,
		},
	})
	require.NoError(t, err)

	_, err = etcdClient1.MemberList(context.Background())
	require.NoError(t, err)

	err = etcdClient1.Close()
	require.NoError(t, err)
}

func newMixedHandler(httpHand http.Handler, grpcHandler http.Handler) http.Handler {
	mixedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("content-type"), "application/grpc") {
			grpcHandler.ServeHTTP(w, r)
			return
		}
		httpHand.ServeHTTP(w, r)
	})

	return h2c.NewHandler(mixedHandler, &http2.Server{})
}

// TODO: move into API, split into RestConfig and Client
func getClient(t *testing.T, service *mux.Service) (client.Client, *rest.Config, error) {
	// Get a client to the server
	k := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"goofy": {
				Server:                   fmt.Sprintf("https://localhost:%d/apiserver/test1", service.Port()),
				CertificateAuthorityData: pki.CACertificateData(),
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"goofy": {
				Username:              "goofy",
				ClientCertificateData: pki.AdminCertificateData(),
				ClientKeyData:         pki.AdminKeyData(),
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"goofy": {
				Cluster:  "goofy",
				AuthInfo: "goofy",
			},
		},
		CurrentContext: "goofy",
	}

	b, err := clientcmd.Write(k)
	require.NoError(t, err)

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(b)
	require.NoError(t, err)

	mapper, err := apiutil.NewDynamicRESTMapper(restConfig)
	require.NoError(t, err)

	c, err := client.New(restConfig, client.Options{Scheme: scheme, Mapper: mapper})
	require.NoError(t, err)
	return c, restConfig, err
}
