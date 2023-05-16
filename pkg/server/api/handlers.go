package api

import (
	"context"
	"fmt"
	"github.com/emicklei/go-restful/v3"
	jsonpatch "github.com/evanphx/json-patch/v5"
	gportforward "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server/portforward"
	"io"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/client-go/tools/portforward"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

func NewAPIServerHandler(scheme *runtime.Scheme) http.Handler {
	apiServer := &apiServerHandler{
		container: restful.NewContainer(),
		scheme:    scheme,
	}

	apiServer.container.Filter(apiServer.globalLogging)

	ws := new(restful.WebService)
	ws.Path("/apiserver")
	ws.Consumes(runtime.ContentTypeJSON)
	ws.Produces(runtime.ContentTypeJSON)

	ws.Route(ws.GET("/{cluster}/api").To(apiServer.apiDiscovery))
	ws.Route(ws.GET("/{cluster}/apis").To(apiServer.discoveryApis))
	ws.Route(ws.GET("/{cluster}/api/v1").To(apiServer.apiV1Discovery))

	ws.Route(ws.GET("/{cluster}/api/v1/{resource}").To(apiServer.apiV1List))
	// TODO: create
	ws.Route(ws.GET("/{cluster}/api/v1/{resource}/{name}").To(apiServer.apiV1Get))
	ws.Route(ws.PATCH("/{cluster}/api/v1/{resource}/{name}").Consumes(string(types.MergePatchType), string(types.StrategicMergePatchType)).To(apiServer.apiV1Patch))
	ws.Route(ws.DELETE("/{cluster}/api/v1/{resource}/{name}").Consumes(runtime.ContentTypeProtobuf).To(apiServer.apiV1Delete))

	ws.Route(ws.GET("/{cluster}/api/v1/namespaces/{namespace}/pods/{name}/portforward").To(apiServer.apiV1PortForward))
	ws.Route(ws.POST("/{cluster}/api/v1/namespaces/{namespace}/pods/{name}/portforward").Consumes("*/*").To(apiServer.apiV1PortForward))

	apiServer.container.Add(ws)

	return apiServer
}

type apiServerHandler struct {
	scheme    *runtime.Scheme
	container *restful.Container
}

func (h *apiServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.container.ServeHTTP(w, r)
}

func (h *apiServerHandler) globalLogging(req *restful.Request, resp *restful.Response, chain *restful.FilterChain) {
	log.Printf("[global-filter (logger)] %s,%s %s\n", req.Request.Method, req.Request.URL, req.HeaderParameter("Content-Type"))
	chain.ProcessFilter(req, resp)
}

func (h *apiServerHandler) routeLogging(req *restful.Request, resp *restful.Response, chain *restful.FilterChain) {
	log.Printf("[route-filter (logger)] %s,%s\n", req.Request.Method, req.Request.URL)
	chain.ProcessFilter(req, resp)
}

func (h *apiServerHandler) apiDiscovery(req *restful.Request, resp *restful.Response) {
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

func (h *apiServerHandler) apiV1Discovery(req *restful.Request, resp *restful.Response) {
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

func (h *apiServerHandler) apiV1List(req *restful.Request, resp *restful.Response) {
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

func (h *apiServerHandler) apiV1Get(req *restful.Request, resp *restful.Response) {
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

func (h *apiServerHandler) apiV1Patch(req *restful.Request, resp *restful.Response) {
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

	encoder, err := h.getEncoder(obj, corev1.SchemeGroupVersion)
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

	codecFactory := serializer.NewCodecFactory(h.scheme)
	err = runtime.DecodeInto(codecFactory.UniversalDecoder(), changedJS, obj)
	if err != nil {

	}

	if err := resp.WriteEntity(obj); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}

func (h *apiServerHandler) getEncoder(obj runtime.Object, gv runtime.GroupVersioner) (runtime.Encoder, error) {
	codecs := serializer.NewCodecFactory(h.scheme)

	info, ok := runtime.SerializerInfoForMediaType(codecs.SupportedMediaTypes(), runtime.ContentTypeJSON)
	if !ok {
		return nil, fmt.Errorf("failed to create serializer for %T", obj)
	}

	encoder := codecs.EncoderForVersion(info.Serializer, gv)
	return encoder, nil
}

func (h *apiServerHandler) apiV1Delete(req *restful.Request, resp *restful.Response) {
	// TODO: make this dynamic deleting objects from the cache.
}

func (h *apiServerHandler) discoveryApis(req *restful.Request, resp *restful.Response) {
	// Note: The return value must contain all the API are required by CAPI.
	// TODO: consider if to make this dynamic, reading APIs from the cache; TBD how to resolve verbs, singular name, categories etc.

	apiGroupList := &metav1.APIGroupList{}

	if err := resp.WriteEntity(apiGroupList); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}

func (h *apiServerHandler) apiV1PortForward(req *restful.Request, resp *restful.Response) {
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

	// Given that in the goofy provider there is no real infrastructure, we are forwarding the connection back to the
	// same server that is implementing the fake API server for all the clusters (the goofy controller pod).
	_, serverPort, err := net.SplitHostPort(req.Request.Host)
	if err != nil {
		panic("error handling not implemented")
	}

	// Start the process handling streams that are published in the stream channel, please note that:
	// - TODO: describe when port forwarder is called
	streamHandler := gportforward.NewHttpStreamHandler(
		conn,
		streamChan,
		req.PathParameter("name"), // name of the Pod to forward to
		req.PathParameter("namespace"),
		portForwarderWithPortOverride(serverPort),
	)
	streamHandler.Run(context.Background())
}

func portForwarderWithPortOverride(serverPort string) func(ctx context.Context, podName, podNamespace, port string, stream io.ReadWriteCloser) error {
	return func(ctx context.Context, podName, podNamespace, port string, stream io.ReadWriteCloser) error {
		if strings.HasPrefix(podName, "kube-apiserver-") {
			return portForwarder(ctx, podName, podNamespace, serverPort, stream)
		}
		if strings.HasPrefix(podName, "kubernetes") {
			return portForwarder(ctx, podName, podNamespace, port, stream)
		}
		panic("not a supported podName")
	}
}

// portForwarder handle a single port
func portForwarder(ctx context.Context, podName, podNamespace, port string, stream io.ReadWriteCloser) error {
	// Given that in the goofy provider there is no real infrastructure, there isn't also
	// a real workload cluster, nor real pods to forward to.
	// Se, for faking a real workload cluster behaviour we are forwarding the connection back to the
	// same server that is implementing the fake API server for all the clusters (the goofy controller pod).
	//
	// This works without additional efforts for the port forward that kcp is doing th check
	// certificate expiration for the apiServer; TBD for etcd.

	// Get a connection to the target of the port forward operation.
	dial, err := net.Dial("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		return fmt.Errorf("failed to dial \":%s\": %w", port, err)
	}
	defer func() {
		_ = dial.Close()
	}()

	// Create a tunnel for bi-directional copy of data between the stream
	// originated from the initiator of the port forward operation and the target.
	return gportforward.HttpStreamTunnel(ctx, stream, dial)
}
