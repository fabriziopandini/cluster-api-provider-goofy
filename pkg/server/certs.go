package server

import (
	"crypto/rsa"
	"crypto/x509"
	"github.com/pkg/errors"
	"net"
	"sigs.k8s.io/cluster-api/util/certs"
)

// newCertAndKey builds a new cert and key signed with the given CA and with the given config.
func newCertAndKey(caCert *x509.Certificate, caKey *rsa.PrivateKey, config *certs.Config) (*x509.Certificate, *rsa.PrivateKey, error) {
	key, err := certs.NewPrivateKey()
	if err != nil {
		return nil, nil, errors.Wrap(err, "unable to create private key")
	}

	cert, err := config.NewSignedCert(key, caCert, caKey)
	if err != nil {
		return nil, nil, errors.Wrap(err, "unable to create certificate")
	}

	return cert, key, nil
}

// apiServerCertificateConfig returns the config for an API server serving certificate.
func apiServerCertificateConfig(controlPlaneIP string) *certs.Config {
	altNames := &certs.AltNames{
		DNSNames: []string{
			// NOTE: DNS names for the kubernetes service are not required (the API
			// server will never be accessed via the service); same for the podName
			"localhost",
		},
		IPs: []net.IP{
			// NOTE: PodIP is not required (it is the same of control plane IP)
			net.IPv4(127, 0, 0, 1),
			net.IPv6loopback,
			net.ParseIP(controlPlaneIP),
		},
	}

	return &certs.Config{
		CommonName: "kube-apiserver",
		AltNames:   *altNames,
		Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
}

// adminClientCertificateConfig returns the config for an admin client certificate
// to be used for access to the API server.
func adminClientCertificateConfig() *certs.Config {
	return &certs.Config{
		CommonName:   "admin",
		Organization: []string{"system:masters"},
		Usages:       []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
}

// etcdServerCertificateConfig returns the config for an etcd member serving certificate.
func etcdServerCertificateConfig(podName, podIP string) *certs.Config {
	altNames := certs.AltNames{
		DNSNames: []string{
			"localhost",
			podName,
		},
		IPs: []net.IP{
			net.IPv4(127, 0, 0, 1),
			net.IPv6loopback,
			net.ParseIP(podIP),
		},
	}

	return &certs.Config{
		CommonName: podName,
		AltNames:   altNames,
		Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
}
