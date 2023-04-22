package server

import (
	"context"
	"crypto/tls"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server/proxy"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"io"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"log"
	"net"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sync/atomic"
	"testing"
)

var scheme = runtime.NewScheme()

func init() {
	_ = metav1.AddMetaToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
}

func TestServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	s, err := New(scheme)
	require.NoError(t, err)

	err = s.Start(ctx)
	require.NoError(t, err)

	k := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"goofy": {
				Server: "http://localhost:8080/clusters/test1",
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"goofy": {
				Username: "goofy",
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

	// list

	nl := &corev1.NodeList{}
	err = c.List(context.TODO(), nl)
	require.NoError(t, err)

	// get

	n := &corev1.Node{}
	err = c.Get(context.TODO(), client.ObjectKey{Name: "foo"}, n)
	require.NoError(t, err)

	// create

	// patch
	// note: strategjc merge patch will behave like traditional patch (ok for the use cases saw so far)
	//       ssa patch will behave like traditional patch or something similar.

	n2 := n.DeepCopy()
	n2.Annotations = map[string]string{"foo": "bar"}
	err = c.Patch(context.TODO(), n2, client.MergeFrom(n))
	require.NoError(t, err)

	n3 := n2.DeepCopy()
	taints := []corev1.Taint{}
	for _, taint := range n.Spec.Taints {
		if taint.Key == "foo" {
			continue
		}
		taints = append(taints, taint)
	}
	n3.Spec.Taints = taints
	err = c.Patch(context.TODO(), n3, client.StrategicMergeFrom(n2))
	require.NoError(t, err)

	/*
		n4 := n.DeepCopy()
		patchOptions := []client.PatchOption{
			client.ForceOwnership,
			client.FieldOwner("obj-manager"),
			client.DryRunAll,
		}
		err = c.Patch(ctx, n4, client.Apply, patchOptions...)
		require.NoError(t, err)

		patchOptions = []client.PatchOption{
			client.ForceOwnership,
			client.FieldOwner("obj-manager"),
		}
		err = c.Patch(ctx, n4, client.Apply, patchOptions...)
		require.NoError(t, err)
	*/

	// delete

	err = c.Delete(context.TODO(), n)
	require.NoError(t, err)

	// port forward

	p := proxy.Proxy{
		Kind:       "pods",
		Namespace:  metav1.NamespaceSystem,
		KubeConfig: restConfig,
		Port:       1234,
	}

	dialer, err := proxy.NewDialer(p)
	require.NoError(t, err)

	rawConn, err := dialer.DialContextWithAddr(ctx, "foo")
	require.NoError(t, err)
	defer rawConn.Close()

	// Execute a TLS handshake over the connection to the kube-apiserver.
	// xref: roughly same code as in tls.DialWithDialer.
	conn := tls.Client(rawConn, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // Intentionally not verifying the server cert here.
	err = conn.HandshakeContext(ctx)
	require.NoError(t, err)
	defer conn.Close()

	conn.Close()

	cancel()
}

const HeaderSpdy31 = "SPDY/3.1"

func TestPortForward(t *testing.T) {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	// back end server, the server which ultimately answer to the request
	router := mux.NewRouter()
	router.PathPrefix("/clusters/test1/api/v1/namespaces/kube-system/pods/foo/portforward").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		w.Header().Add(httpstream.HeaderConnection, httpstream.HeaderUpgrade)
		w.Header().Add(httpstream.HeaderUpgrade, HeaderSpdy31)

		w.WriteHeader(http.StatusSwitchingProtocols)

		w.Write([]byte("OK,something !"))
	})
	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	// portforward server
	lb := newLB(t, "127.0.0.1:8080")
	defer lb.ln.Close()
	stopCh := make(chan struct{})
	go lb.serve(stopCh)

	// port forward
	k := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"goofy": {
				Server: "http://localhost:8081/clusters/test1",
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"goofy": {
				Username: "goofy",
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

	p := proxy.Proxy{
		Kind:       "pods",
		Namespace:  metav1.NamespaceSystem,
		KubeConfig: restConfig,
		Port:       8080,
	}

	dialer, err := proxy.NewDialer(p)
	require.NoError(t, err)

	rawConn, err := dialer.DialContextWithAddr(ctx, "foo")
	require.NoError(t, err)
	defer rawConn.Close()

	// Execute a TLS handshake over the connection to the kube-apiserver.
	// xref: roughly same code as in tls.DialWithDialer.
	conn := tls.Client(rawConn, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // Intentionally not verifying the server cert here.
	err = conn.HandshakeContext(ctx)
	require.NoError(t, err)
	defer conn.Close()

	conn.Close()

	cancel()

}

type tcpLB struct {
	t         *testing.T
	ln        net.Listener
	serverURL string
	dials     int32
}

func (lb *tcpLB) handleConnection(in net.Conn, stopCh chan struct{}) {
	out, err := net.Dial("tcp", lb.serverURL)
	if err != nil {
		lb.t.Log(err)
		return
	}
	go io.Copy(out, in)
	go io.Copy(in, out)
	<-stopCh
	if err := out.Close(); err != nil {
		lb.t.Fatalf("failed to close connection: %v", err)
	}
}

func (lb *tcpLB) serve(stopCh chan struct{}) {
	conn, err := lb.ln.Accept()
	if err != nil {
		lb.t.Fatalf("failed to accept: %v", err)
	}
	atomic.AddInt32(&lb.dials, 1)
	go lb.handleConnection(conn, stopCh)
}

func newLB(t *testing.T, serverURL string) *tcpLB {
	ln, err := net.Listen("tcp", "127.0.0.1:8081")
	if err != nil {
		t.Fatalf("failed to bind: %v", err)
	}
	lb := tcpLB{
		serverURL: serverURL,
		ln:        ln,
		t:         t,
	}
	return &lb
}
