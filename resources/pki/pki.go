package pki

import (
	"embed"
)

var (
	// files for a test Kubernetes pki, this is not used in production and it must never be.
	//go:embed *.crt *.key
	files embed.FS
)

// APIServerCertificateData IMPORTANT! this is not used in production and it must never be.
func APIServerCertificateData() []byte {
	return mustRead("apiserver.crt")
}

// APIServerKeyData IMPORTANT! this is not used in production and it must never be.
func APIServerKeyData() []byte {
	return mustRead("apiserver.key")
}

// CACertificateData IMPORTANT! this is not used in production and it must never be.
func CACertificateData() []byte {
	return mustRead("ca.crt")
}

// AdminCertificateData IMPORTANT! this is not used in production and it must never be.
func AdminCertificateData() []byte {
	return mustRead("admin.crt")
}

// AdminKeyData IMPORTANT! this is not used in production and it must never be.
func AdminKeyData() []byte {
	return mustRead("admin.key")
}

func mustRead(name string) []byte {
	data, err := files.ReadFile(name)
	if err != nil {
		panic("failed to read " + name)
	}
	return data
}
