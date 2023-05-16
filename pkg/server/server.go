package server

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// TODO: TLS https://medium.com/@satrobit/how-to-build-https-servers-with-certificate-lazy-loading-in-go-bff5e9ef2f1f

type Server struct {
	scheme *runtime.Scheme

	started bool
}

func New(scheme *runtime.Scheme) (*Server, error) {
	return &Server{
		scheme: scheme,
	}, nil
}
