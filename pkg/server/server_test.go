package server

import (
	"context"
	"crypto/tls"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server/proxy"
	"github.com/fabriziopandini/cluster-api-provider-goofy/resources/pki"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"testing"
)

var scheme = runtime.NewScheme()

func init() {
	_ = metav1.AddMetaToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
}

func TestServer(t *testing.T) {
	// start a test server and get a client
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	c, _, err := createServerAndGetClient(t, ctx)

	// list

	nl := &corev1.NodeList{}
	err = c.List(context.TODO(), nl)
	require.NoError(t, err)

	// get

	n := &corev1.Node{}
	err = c.Get(context.TODO(), client.ObjectKey{Name: "foo"}, n)
	require.NoError(t, err)

	// create

	// TODO: create

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

	cancel()
}

func createServerAndGetClient(t *testing.T, ctx context.Context) (client.Client, *rest.Config, error) {
	s, err := New(scheme)
	require.NoError(t, err)

	err = s.Start(ctx)
	require.NoError(t, err)

	// Get a client to the server
	k := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"goofy": {
				Server:                   "https://localhost:8080/clusters/test1",
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

func TestPortForward(t *testing.T) {
	// start a test server and get a client
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	_, restConfig, err := createServerAndGetClient(t, ctx)

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

	/*
		fmt.Fprintf(rawConn, "GET /clusters/test1/api HTTP/1.0\r\n\r\n")
		buf := make([]byte, 5)
		n, err := rawConn.Read(buf)
		require.NoError(t, err)
		fmt.Println("total size:", n, string(buf))

		fmt.Fprintf(rawConn, "GET /clusters/test1/api HTTP/1.0\r\n\r\n")
		n, err = rawConn.Read(buf)
		require.NoError(t, err)
		fmt.Println("total size:", n, string(buf))

		time.Sleep(5000000 * time.Second)
	*/
}

// client --> APIServer -(portforward)-> APIServerPod

// server (HTTPS APIServer:8080)

// client --> APIServer -(portforward)-> EtcdPod
