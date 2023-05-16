package etcd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server/mux"
	"github.com/fabriziopandini/cluster-api-provider-goofy/resources/pki"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	pb "go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestEtcd1(t *testing.T) {
	grpcServ := grpc.NewServer()
	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/", home)

	mySvc := &clusterServerService{}
	pb.RegisterClusterServer(grpcServ, mySvc)

	mixedHandler := newHTTPandGRPCMux(httpMux, grpcServ)
	http2Server := &http2.Server{}
	http1Server := &http.Server{Handler: h2c.NewHandler(mixedHandler, http2Server)}
	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		panic(err)
	}

	go func() {
		err = http1Server.Serve(lis)
		if errors.Is(err, http.ErrServerClosed) {
			fmt.Println("server closed")
		} else if err != nil {
			panic(err)
		}
	}()

	etcdClient, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"http://127.0.0.1:8080"},
		DialTimeout: 2 * time.Second,
		/*
			DialOptions: []grpc.DialOption{
				grpc.WithBlock(), // block until the underlying connection is up
				grpc.WithContextDialer(dialer.DialContextWithAddr),
			},
		*/
		TLS: &tls.Config{InsecureSkipVerify: true},
	})
	require.NoError(t, err)

	_, err = etcdClient.MemberList(context.Background())
	require.NoError(t, err)
}

func TestEtcd1TLS(t *testing.T) {
	grpcServ := grpc.NewServer()
	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/", home)

	mySvc := &clusterServerService{}
	pb.RegisterClusterServer(grpcServ, mySvc)

	mixedHandler := newHTTPandGRPCMux(httpMux, grpcServ)
	http2Server := &http2.Server{}

	serverCert, err := tls.X509KeyPair(pki.APIServerCertificateData(), pki.APIServerKeyData())
	require.NoError(t, err)

	http1Server := &http.Server{
		Handler: h2c.NewHandler(mixedHandler, http2Server),
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{serverCert},
		},
	}
	lis, err := net.Listen("tcp", ":8080")
	require.NoError(t, err)

	go func() {
		err = http1Server.ServeTLS(lis, "", "")
		require.NoError(t, err)
	}()

	clientCert, err := tls.X509KeyPair(pki.AdminCertificateData(), pki.AdminKeyData())
	require.NoError(t, err)

	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(pki.CACertificateData())

	etcdClient, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"https://127.0.0.1:8080"},
		DialTimeout: 2 * time.Second,
		/*
			DialOptions: []grpc.DialOption{
				grpc.WithBlock(), // block until the underlying connection is up
				grpc.WithContextDialer(dialer.DialContextWithAddr),
			},
		*/
		TLS: &tls.Config{
			RootCAs:      caPool,
			Certificates: []tls.Certificate{clientCert},
			MinVersion:   tls.VersionTLS12,
		},
	})
	require.NoError(t, err)

	_, err = etcdClient.MemberList(context.Background())
	require.NoError(t, err)
}

func home(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "hello from http handler!\n")
}

func newHTTPandGRPCMux(httpHand http.Handler, grpcHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("content-type"), "application/grpc") {
			grpcHandler.ServeHTTP(w, r)
			return
		}
		httpHand.ServeHTTP(w, r)
	})
}

func TestEtcd2TLS(t *testing.T) {
	// server

	grpcServ := grpc.NewServer()
	mySvc := &clusterServerService{}
	pb.RegisterClusterServer(grpcServ, mySvc)

	serverCert, err := tls.X509KeyPair(pki.APIServerCertificateData(), pki.APIServerKeyData())
	require.NoError(t, err)
	http1Server := &http.Server{
		Handler: grpcServ,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{serverCert},
		},
	}

	lis1, err := net.Listen("tcp", ":8081")
	require.NoError(t, err)
	go func() {
		err = http1Server.ServeTLS(lis1, "", "")
		require.NoError(t, err)
	}()

	// client

	clientCert, err := tls.X509KeyPair(pki.AdminCertificateData(), pki.AdminKeyData())
	require.NoError(t, err)

	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(pki.CACertificateData())

	etcdClient1, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"https://127.0.0.1:8081"},
		DialTimeout: 2 * time.Second,
		/*
			DialOptions: []grpc.DialOption{
				grpc.WithBlock(), // block until the underlying connection is up
				grpc.WithContextDialer(dialer.DialContextWithAddr),
			},
		*/
		TLS: &tls.Config{
			RootCAs:      caPool,
			Certificates: []tls.Certificate{clientCert},
			MinVersion:   tls.VersionTLS12,
		},
	})
	require.NoError(t, err)

	_, err = etcdClient1.MemberList(context.Background())
	require.NoError(t, err)

	lis2, err := net.Listen("tcp", ":8082")
	require.NoError(t, err)
	go func() {
		err = http1Server.ServeTLS(lis2, "", "")
		require.NoError(t, err)
	}()

	etcdClient2, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"https://127.0.0.1:8082"},
		DialTimeout: 2 * time.Second,
		/*
			DialOptions: []grpc.DialOption{
				grpc.WithBlock(), // block until the underlying connection is up
				grpc.WithContextDialer(dialer.DialContextWithAddr),
			},
		*/
		TLS: &tls.Config{
			RootCAs:      caPool,
			Certificates: []tls.Certificate{clientCert},
			MinVersion:   tls.VersionTLS12,
		},
	})
	require.NoError(t, err)

	_, err = etcdClient2.MemberList(context.Background())
	require.NoError(t, err)
}

func TestEtcd3TLS(t *testing.T) {
	// server

	grpcServ := grpc.NewServer()
	mySvc := &clusterServerService{}
	pb.RegisterClusterServer(grpcServ, mySvc)

	serverCert, err := tls.X509KeyPair(pki.APIServerCertificateData(), pki.APIServerKeyData())
	require.NoError(t, err)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
	}

	server := mux.NewServer("127.0.0.1", tlsConfig, grpcServ)

	// service 1 + test

	clientCert, err := tls.X509KeyPair(pki.AdminCertificateData(), pki.AdminKeyData())
	require.NoError(t, err)

	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(pki.CACertificateData())

	for i := 1; i <= 10; i++ {
		service1, err := server.AddService()
		require.NoError(t, err)

		err = server.StartService(service1.Address())
		require.NoError(t, err)

		etcdClient1, err := clientv3.New(clientv3.Config{
			Endpoints:   []string{service1.Address()},
			DialTimeout: 2 * time.Second,
			/*
				DialOptions: []grpc.DialOption{
					grpc.WithBlock(), // block until the underlying connection is up
					grpc.WithContextDialer(dialer.DialContextWithAddr),
				},
			*/
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

	err = server.Shutdown(context.Background())
	require.NoError(t, err)
}

// TODO: what if the listner get closed (looking for a way to release ports)
