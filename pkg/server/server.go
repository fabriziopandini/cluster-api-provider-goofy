package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/emicklei/go-restful/v3"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server/etcd"
	gportforward "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server/portforward"
	"github.com/fabriziopandini/cluster-api-provider-goofy/resources/pki"
	"io"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/portforward"
	"log"
	"net"
	"net/http"
	"time"
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

func (s *Server) Start(ctx context.Context) error {
	apiServer := restful.NewContainer()
	apiServer.Filter(globalLogging)

	apiServerWS := new(restful.WebService)
	apiServerWS.Path("/apiserver")
	apiServerWS.Consumes(runtime.ContentTypeJSON)
	apiServerWS.Produces(runtime.ContentTypeJSON)

	apiServerWS.Route(apiServerWS.GET("/{cluster}/api").To(s.apiDiscovery))
	apiServerWS.Route(apiServerWS.GET("/{cluster}/apis").To(s.discoveryApis))
	apiServerWS.Route(apiServerWS.GET("/{cluster}/api/v1").To(s.apiV1Discovery))

	apiServerWS.Route(apiServerWS.GET("/{cluster}/api/v1/{resource}").To(s.apiV1List))
	// TODO: create
	apiServerWS.Route(apiServerWS.GET("/{cluster}/api/v1/{resource}/{name}").To(s.apiV1Get))
	apiServerWS.Route(apiServerWS.PATCH("/{cluster}/api/v1/{resource}/{name}").Consumes(string(types.MergePatchType), string(types.StrategicMergePatchType)).To(s.apiV1Patch))
	apiServerWS.Route(apiServerWS.DELETE("/{cluster}/api/v1/{resource}/{name}").Consumes(runtime.ContentTypeProtobuf).To(s.apiV1Delete))

	apiServerWS.Route(apiServerWS.GET("/{cluster}/api/v1/namespaces/{namespace}/pods/{name}/portforward").To(s.apiV1PortForward))
	apiServerWS.Route(apiServerWS.POST("/{cluster}/api/v1/namespaces/{namespace}/pods/{name}/portforward").Consumes("*/*").To(s.apiV1PortForward))
	apiServerWS.Route(apiServerWS.GET("/{cluster}/etcd").Filter(routeLogging).To(s.etcd))

	apiServer.Add(apiServerWS)

	cert, err := tls.X509KeyPair(pki.APIServerCertificateData(), pki.APIServerKeyData())
	if err != nil {

	}

	srv := &http.Server{
		Addr:    ":8080", // TODO: make this configurable
		Handler: apiServer,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
		},
	}

	// Handle server shut down via context cancellation.
	go func() {
		<-ctx.Done()

		// Use a new context for shutdown, with a timeout for this operation.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	// Run server in a goroutine so that it doesn't block.
	listenAndServe := false
	go func() {
		listenAndServe = true

		if err := srv.ListenAndServeTLS("", ""); err != nil {
			log.Println(err)
		}
	}()

	// Wait for the server to be started.
	if err := wait.PollImmediate(50*time.Millisecond, 5*time.Second, func() (done bool, err error) {
		if !listenAndServe {
			return false, nil
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to start server: %v", err)
	}

	s.started = true

	return nil
}

func globalLogging(req *restful.Request, resp *restful.Response, chain *restful.FilterChain) {
	log.Printf("[global-filter (logger)] %s,%s %s\n", req.Request.Method, req.Request.URL, req.HeaderParameter("Content-Type"))
	chain.ProcessFilter(req, resp)
}

func routeLogging(req *restful.Request, resp *restful.Response, chain *restful.FilterChain) {
	log.Printf("[route-filter (logger)] %s,%s\n", req.Request.Method, req.Request.URL)
	chain.ProcessFilter(req, resp)
}

func (s *Server) apiDiscovery(req *restful.Request, resp *restful.Response) {
	// Note: The return value must contain all the API are required by CAPI.
	// TODO: consider if to make this dynamic, reading APIs from the cache
	apiVersions := &metav1.APIVersions{
		Versions: []string{"v1"},
	}

	if err := resp.WriteEntity(apiVersions); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}

func (s *Server) apiV1Discovery(req *restful.Request, resp *restful.Response) {
	// Note: The return value must contain all the API are required by CAPI.
	// TODO: consider if to make this dynamic, reading APIs from the cache; TBD how to resolve verbs, singular name, categories etc.
	apiResourceList := &metav1.APIResourceList{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{
			{
				Name:         "nodes",
				SingularName: "",
				Namespaced:   false,
				Kind:         "Node",
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
				ShortNames: []string{
					"no",
				},
				Categories:         nil,
				StorageVersionHash: "",
			},
		},
	}

	if err := resp.WriteEntity(apiResourceList); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}

func (s *Server) apiV1List(req *restful.Request, resp *restful.Response) {
	// TODO: make this dynamic reading objects from the cache.
	nodeList := &corev1.NodeList{
		Items: []corev1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Now(),
					Name:              "foo",
				},
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{
							Key:    "foo",
							Value:  "foo",
							Effect: "foo",
						},
						{
							Key:    "bar",
							Value:  "bar",
							Effect: "bar",
						},
					},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
					NodeInfo: corev1.NodeSystemInfo{
						KubeletVersion: "v1.25.2",
					},
				},
			},
		},
	}

	if err := resp.WriteEntity(nodeList); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}

func (s *Server) apiV1Get(req *restful.Request, resp *restful.Response) {
	// TODO: make this dynamic reading objects from the cache.
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.Now(),
			Name:              "foo",
		},
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{
				{
					Key:    "foo",
					Value:  "foo",
					Effect: "foo",
				},
				{
					Key:    "bar",
					Value:  "bar",
					Effect: "bar",
				},
			},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
			NodeInfo: corev1.NodeSystemInfo{
				KubeletVersion: "v1.25.2",
			},
		},
	}

	if err := resp.WriteEntity(node); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}

func (s *Server) apiV1Patch(req *restful.Request, resp *restful.Response) {
	// TODO: make this dynamic patching objects into the cache.
	// TODO: consider if to move patch implementation into the client vs doing patch here and calling upgrade

	defer req.Request.Body.Close()
	patchData, _ := io.ReadAll(req.Request.Body)

	obj := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.Now(),
			Name:              "foo",
		},
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{
				{
					Key:    "foo",
					Value:  "foo",
					Effect: "foo",
				},
				{
					Key:    "bar",
					Value:  "bar",
					Effect: "bar",
				},
			},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
			NodeInfo: corev1.NodeSystemInfo{
				KubeletVersion: "v1.25.2",
			},
		},
	}

	encoder, err := s.getEncoder(obj, corev1.SchemeGroupVersion)
	if err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	originalObjJS, err := runtime.Encode(encoder, obj)
	if err != nil {

	}

	var changedJS []byte
	patchType := req.HeaderParameter("Content-Type")
	switch types.PatchType(patchType) {
	case types.MergePatchType:
	case types.StrategicMergePatchType:

		changedJS, err = jsonpatch.MergePatch(originalObjJS, patchData)
		if err != nil {
			_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
			return
		}

	case types.JSONPatchType:
	case types.ApplyPatchType:
		panic("not supported")
	}

	codecFactory := serializer.NewCodecFactory(s.scheme)
	err = runtime.DecodeInto(codecFactory.UniversalDecoder(), changedJS, obj)
	if err != nil {

	}

	if err := resp.WriteEntity(obj); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}

func (s *Server) getEncoder(obj runtime.Object, gv runtime.GroupVersioner) (runtime.Encoder, error) {
	codecs := serializer.NewCodecFactory(s.scheme)

	info, ok := runtime.SerializerInfoForMediaType(codecs.SupportedMediaTypes(), runtime.ContentTypeJSON)
	if !ok {
		return nil, fmt.Errorf("failed to create serializer for %T", obj)
	}

	encoder := codecs.EncoderForVersion(info.Serializer, gv)
	return encoder, nil
}

func (s *Server) apiV1Delete(req *restful.Request, resp *restful.Response) {
	// TODO: make this dynamic deleting objects from the cache.
}

func (s *Server) discoveryApis(req *restful.Request, resp *restful.Response) {
	// Note: The return value must contain all the API are required by CAPI.
	// TODO: consider if to make this dynamic, reading APIs from the cache; TBD how to resolve verbs, singular name, categories etc.

	apiGroupList := &metav1.APIGroupList{}

	if err := resp.WriteEntity(apiGroupList); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}

func (s *Server) apiV1PortForward(req *restful.Request, resp *restful.Response) {
	// In order to handle a port forward request the current connection has to be upgraded
	// in order to become compliant with the spyd protocol.
	// This implies two steps:
	// - Adding support for handling multiple http streams, used for subsequent operations over
	//   the forwarded connection.
	// - Opening a connection to the target endpoint, the endpoint to port forward to, and setting up
	//   a bi-directional copy of data because the server acts as a man in the middle.

	// Perform a sub protocol negotiation, ensuring tha client and the server agree on how
	// to handle communications over the port forwarded connection.
	request := req.Request
	respWriter := resp.ResponseWriter
	_, err := httpstream.Handshake(request, respWriter, []string{portforward.PortForwardProtocolV1Name})
	if err != nil {
		panic("error handling not implemented")
	}

	// Create a channel where to handle http streams that will be generated for each subsequent
	// operations over the port forwarded connection.
	streamChan := make(chan httpstream.Stream, 1)

	// Upgrade the connection specifying what to do when a new http stream is received.
	// After being received, the new stream will be published into the stream channel for handling.
	upgrader := spdy.NewResponseUpgrader()
	conn := upgrader.UpgradeResponse(respWriter, request, gportforward.HttpStreamReceived(streamChan))
	if conn == nil {
		panic("error handling not implemented")
	}
	defer func() {
		_ = conn.Close()
	}()
	conn.SetIdleTimeout(10 * time.Minute)

	// Start the process handling streams that are published in the stream channel, please note that:
	// - TODO: describe when port forwarder is called
	h := gportforward.NewHttpStreamHandler(
		conn,
		streamChan,
		req.PathParameter("name"), // name of the Pod to forward to
		req.PathParameter("namespace"),
		portForwarder,
	)
	h.Run(context.Background())
}

// portForwarder handle a single port
func portForwarder(ctx context.Context, podName, podNamespace string, port int32, stream io.ReadWriteCloser) error {
	// Given that in the goofy provider there is no real infrastructure, there isn't also
	// a real workload cluster, nor real pods to forward to.
	// Se, for faking a real workload cluster behaviour we are forwarding the connection back to the
	// same server that is implementing the fake API server for all the clusters (the goofy controller pod).
	//
	// This works without additional efforts for the port forward that kcp is doing th check
	// certificate expiration for the apiServer; TBD for etcd.

	// Get a connection to the target of the port forward operation.
	dial, err := net.Dial("tcp", ":8080") // TODO: compose this
	if err != nil {
		return fmt.Errorf("failed to dial %s: %w", ":8080", err)
	}
	defer func() {
		_ = dial.Close()
	}()

	// Create a tunnel for bi-directional copy of data between the stream
	// originated from the initiator of the port forward operation and the target.
	return gportforward.HttpStreamTunnel(ctx, stream, dial)
}

func (s *Server) etcd(req *restful.Request, resp *restful.Response) {

	etcd.NewEtcdServer().ServeHTTP(resp.ResponseWriter, req.Request)

}
