package server

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	cmanager "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/manager"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server/proxy"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"math/big"
	"math/rand"
	"sigs.k8s.io/cluster-api/util/certs"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
	"time"
)

var (
	ctx    = context.Background()
	scheme = runtime.NewScheme()
)

func init() {
	_ = metav1.AddMetaToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)
}

func TestAPI_corev1_CRUD(t *testing.T) {
	manager := cmanager.New(scheme)

	host := "127.0.0.1"
	wcmux := NewWorkloadClustersMux(manager, host)

	// InfraCluster controller >> when "creating the load balancer"
	wcl1 := "workload-cluster1"

	manager.AddResourceGroup(wcl1)
	cc := manager.GetResourceGroup(wcl1).GetClient()

	// TODO: move this down and create using the handler
	nc := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "foo"},
	}
	cc.Create(ctx, nc)

	listener, err := wcmux.InitWorkloadClusterListener(wcl1)
	require.NoError(t, err)
	require.Equal(t, listener.Host(), host)
	require.NotEmpty(t, listener.Port())

	caCert, caKey, err := newCertificateAuthority()
	require.NoError(t, err)

	// InfraMachine controller >> when "creating the API Server pod"
	apiServerPod1 := "kube-apiserver-1"
	err = wcmux.AddAPIServer(wcl1, apiServerPod1, caCert, caKey)
	require.NoError(t, err)

	etcdCert, etcdKey, err := newCertificateAuthority()
	require.NoError(t, err)

	// InfraMachine controller >> when "creating the Etcd member pod"
	etcdPodMember1 := "etcd-1"
	err = wcmux.AddEtcdMember(wcl1, etcdPodMember1, etcdCert, etcdKey)
	require.NoError(t, err)

	// Test API using a controller runtime client to call IT
	c, err := listener.GetClient()
	require.NoError(t, err)

	// create

	nc2 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "bar"},
	}
	err = c.Create(ctx, nc2)
	require.NoError(t, err)

	// list

	nl := &corev1.NodeList{}
	err = c.List(ctx, nl)
	require.NoError(t, err)

	// get

	n := &corev1.Node{}
	err = c.Get(ctx, client.ObjectKey{Name: "foo"}, n)
	require.NoError(t, err)

	// patch

	n2 := n.DeepCopy()
	n2.Annotations = map[string]string{"foo": "bar"}
	err = c.Patch(ctx, n2, client.MergeFrom(n))
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
	err = c.Patch(ctx, n3, client.StrategicMergeFrom(n2))
	require.NoError(t, err)

	// delete

	err = c.Delete(ctx, n)
	require.NoError(t, err)
}

func TestAPI_rbacv1_CRUD(t *testing.T) {
	manager := cmanager.New(scheme)

	host := "127.0.0.1"
	wcmux := NewWorkloadClustersMux(manager, host)

	// InfraCluster controller >> when "creating the load balancer"
	wcl1 := "workload-cluster1"

	manager.AddResourceGroup(wcl1)
	cc := manager.GetResourceGroup(wcl1).GetClient()

	// TODO: move this down and create using the handler
	nc := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "foo"},
	}
	err := cc.Create(ctx, nc)
	require.NoError(t, err)

	listener, err := wcmux.InitWorkloadClusterListener(wcl1)
	require.NoError(t, err)
	require.Equal(t, listener.Host(), host)
	require.NotEmpty(t, listener.Port())

	caCert, caKey, err := newCertificateAuthority()
	require.NoError(t, err)

	// InfraMachine controller >> when "creating the API Server pod"
	apiServerPod1 := "kube-apiserver-1"
	err = wcmux.AddAPIServer(wcl1, apiServerPod1, caCert, caKey)
	require.NoError(t, err)

	etcdCert, etcdKey, err := newCertificateAuthority()
	require.NoError(t, err)

	// InfraMachine controller >> when "creating the Etcd member pod"
	etcdPodMember1 := "etcd-1"
	err = wcmux.AddEtcdMember(wcl1, etcdPodMember1, etcdCert, etcdKey)
	require.NoError(t, err)

	// Test API using a controller runtime client to call IT
	c, err := listener.GetClient()
	require.NoError(t, err)

	// create

	// TODO: create

	// list

	nl := &rbacv1.ClusterRoleList{}
	err = c.List(ctx, nl)
	require.NoError(t, err)

	// get

	n := &rbacv1.ClusterRole{}
	err = c.Get(ctx, client.ObjectKey{Name: "foo"}, n)
	require.NoError(t, err)

	// patch

	n2 := n.DeepCopy()
	n2.Annotations = map[string]string{"foo": "bar"}
	err = c.Patch(ctx, n2, client.MergeFrom(n))
	require.NoError(t, err)

	// delete

	err = c.Delete(ctx, n)
	require.NoError(t, err)
}

func TestAPI_PortForward(t *testing.T) {
	manager := cmanager.New(scheme)

	host := "127.0.0.1"
	wcmux := NewWorkloadClustersMux(manager, host)

	// InfraCluster controller >> when "creating the load balancer"
	wcl1 := "workload-cluster1"
	listener, err := wcmux.InitWorkloadClusterListener(wcl1)
	require.NoError(t, err)
	require.Equal(t, listener.Host(), host)
	require.NotEmpty(t, listener.Port())

	caCert, caKey, err := newCertificateAuthority()
	require.NoError(t, err)

	// InfraMachine controller >> when "creating the API Server pod"
	apiServerPod1 := "kube-apiserver-1"
	err = wcmux.AddAPIServer(wcl1, apiServerPod1, caCert, caKey)
	require.NoError(t, err)

	etcdCert, etcdKey, err := newCertificateAuthority()
	require.NoError(t, err)

	// InfraMachine controller >> when "creating the Etcd member pod"
	etcdPodMember1 := "etcd-1"
	err = wcmux.AddEtcdMember(wcl1, etcdPodMember1, etcdCert, etcdKey)
	require.NoError(t, err)

	// Setup resource group
	manager.AddResourceGroup("workload-cluster1")

	etcdPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceSystem,
			Name:      etcdPodMember1,
			Labels: map[string]string{
				"component": "etcd",
				"tier":      "control-plane",
			},
			Annotations: map[string]string{
				// TODO: read this from existing etcd pods, if any.
				"etcd.internal.goofy.cluster.x-k8s.io/cluster-id": fmt.Sprintf("%d", rand.Uint32()),
				"etcd.internal.goofy.cluster.x-k8s.io/member-id":  fmt.Sprintf("%d", rand.Uint32()),
				// TODO: set this only if there are no other leaders.
				"etcd.internal.goofy.cluster.x-k8s.io/leader-from": time.Now().Format(time.RFC3339),
			},
		},
	}
	err = manager.GetResourceGroup("workload-cluster1").GetClient().Create(ctx, etcdPod)
	require.NoError(t, err)

	// Test API server TLS handshake via port forward.

	restConfig, err := listener.RESTConfig()
	require.NoError(t, err)

	p1 := proxy.Proxy{
		Kind:       "pods",
		Namespace:  metav1.NamespaceSystem,
		KubeConfig: restConfig,
		Port:       1234,
	}

	dialer1, err := proxy.NewDialer(p1)
	require.NoError(t, err)

	rawConn, err := dialer1.DialContextWithAddr(ctx, "kube-apiserver-foo")
	require.NoError(t, err)
	defer rawConn.Close()

	conn := tls.Client(rawConn, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // Intentionally not verifying the server cert here.
	err = conn.HandshakeContext(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Test Etcd via port forward

	caPool := x509.NewCertPool()
	caPool.AddCert(etcdCert)

	config := apiServerEtcdClientCertificateConfig()
	cert, key, err := newCertAndKey(etcdCert, etcdKey, config)
	require.NoError(t, err)

	clientCert, err := tls.X509KeyPair(certs.EncodeCertPEM(cert), certs.EncodePrivateKeyPEM(key))
	require.NoError(t, err)

	p2 := proxy.Proxy{
		Kind:       "pods",
		Namespace:  metav1.NamespaceSystem,
		KubeConfig: restConfig,
		Port:       2379,
	}

	dialer2, err := proxy.NewDialer(p2)
	require.NoError(t, err)

	etcdClient1, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{etcdPodMember1},
		DialTimeout: 2 * time.Second,

		DialOptions: []grpc.DialOption{
			grpc.WithBlock(), // block until the underlying connection is up
			grpc.WithContextDialer(dialer2.DialContextWithAddr),
		},
		TLS: &tls.Config{
			RootCAs:      caPool,
			Certificates: []tls.Certificate{clientCert},
			MinVersion:   tls.VersionTLS12,
		},
	})
	require.NoError(t, err)

	_, err = etcdClient1.MemberList(ctx)
	require.NoError(t, err)

	err = etcdClient1.Close()
	require.NoError(t, err)
}

// newCertificateAuthority creates new certificate and private key for the certificate authority.
func newCertificateAuthority() (*x509.Certificate, *rsa.PrivateKey, error) {
	key, err := certs.NewPrivateKey()
	if err != nil {
		return nil, nil, err
	}

	c, err := newSelfSignedCACert(key)
	if err != nil {
		return nil, nil, err
	}

	return c, key, nil
}

// newSelfSignedCACert creates a CA certificate.
func newSelfSignedCACert(key *rsa.PrivateKey) (*x509.Certificate, error) {
	cfg := certs.Config{
		CommonName: "kubernetes",
	}

	now := time.Now().UTC()

	tmpl := x509.Certificate{
		SerialNumber: new(big.Int).SetInt64(0),
		Subject: pkix.Name{
			CommonName:   cfg.CommonName,
			Organization: cfg.Organization,
		},
		NotBefore:             now.Add(time.Minute * -5),
		NotAfter:              now.Add(time.Hour * 24 * 365 * 10), // 10 years
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		MaxPathLenZero:        true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		IsCA:                  true,
	}

	b, err := x509.CreateCertificate(cryptorand.Reader, &tmpl, &tmpl, key.Public(), key)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create self signed CA certificate: %+v", tmpl)
	}

	c, err := x509.ParseCertificate(b)
	return c, errors.WithStack(err)
}

func apiServerEtcdClientCertificateConfig() *certs.Config {
	return &certs.Config{
		CommonName:   "apiserver-etcd-client",
		Organization: []string{"system:masters"}, // TODO: check if we can drop
		Usages:       []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
}
