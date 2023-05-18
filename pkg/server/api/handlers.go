package api

import (
	"context"
	"fmt"
	"github.com/emicklei/go-restful/v3"
	jsonpatch "github.com/evanphx/json-patch/v5"
	cmanager "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/manager"
	gportforward "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server/api/portforward"
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
	"time"
)

// ResourceGroupResolver defines a func that can identify which workloadCluster/resourceGroup a
// request targets to.
type ResourceGroupResolver func(host string) (string, error)

// NewAPIServerHandler returns an http.Handler for fake API server.
func NewAPIServerHandler(manager cmanager.Manager, resolver ResourceGroupResolver) http.Handler {
	apiServer := &apiServerHandler{
		container:             restful.NewContainer(),
		manager:               manager,
		resourceGroupResolver: resolver,
	}

	apiServer.container.Filter(apiServer.globalLogging)

	ws := new(restful.WebService)
	ws.Consumes(runtime.ContentTypeJSON)
	ws.Produces(runtime.ContentTypeJSON)

	// Discovery endpoints
	ws.Route(ws.GET("/api").To(apiServer.apiDiscovery))
	ws.Route(ws.GET("/apis").To(apiServer.discoveryApis))
	ws.Route(ws.GET("/api/v1").To(apiServer.apiV1Discovery))

	// CRUD endpoints
	// TODO: create
	ws.Route(ws.GET("/api/v1/{resource}").To(apiServer.apiV1List))
	ws.Route(ws.GET("/api/v1/{resource}/{name}").To(apiServer.apiV1Get))
	ws.Route(ws.PATCH("/api/v1/{resource}/{name}").Consumes(string(types.MergePatchType), string(types.StrategicMergePatchType)).To(apiServer.apiV1Patch))
	ws.Route(ws.DELETE("/api/v1/{resource}/{name}").Consumes(runtime.ContentTypeProtobuf).To(apiServer.apiV1Delete))

	// Port forward endpoints
	ws.Route(ws.GET("/api/v1/namespaces/{namespace}/pods/{name}/portforward").To(apiServer.apiV1PortForward))
	ws.Route(ws.POST("/api/v1/namespaces/{namespace}/pods/{name}/portforward").Consumes("*/*").To(apiServer.apiV1PortForward))

	apiServer.container.Add(ws)

	return apiServer
}

type apiServerHandler struct {
	manager               cmanager.Manager
	container             *restful.Container
	resourceGroupResolver ResourceGroupResolver
}

func (h *apiServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.container.ServeHTTP(w, r)
}

func (h *apiServerHandler) globalLogging(req *restful.Request, resp *restful.Response, chain *restful.FilterChain) {
	log.Printf("%s,%s %s\n", req.Request.Method, req.Request.URL, req.HeaderParameter("Content-Type"))
	chain.ProcessFilter(req, resp)
}

func (h *apiServerHandler) routeLogging(req *restful.Request, resp *restful.Response, chain *restful.FilterChain) {
	log.Printf("[route-debug] %s,%s\n", req.Request.Method, req.Request.URL)
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
	resourceGroup, err := h.resourceGroupResolver(req.Request.Host)
	if err != nil {

	}
	fmt.Print(resourceGroup)

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

	codecFactory := serializer.NewCodecFactory(h.manager.GetScheme())
	err = runtime.DecodeInto(codecFactory.UniversalDecoder(), changedJS, obj)
	if err != nil {

	}

	if err := resp.WriteEntity(obj); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}

func (h *apiServerHandler) getEncoder(obj runtime.Object, gv runtime.GroupVersioner) (runtime.Encoder, error) {
	codecs := serializer.NewCodecFactory(h.manager.GetScheme())

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

	podName := req.PathParameter("name")
	podNamespace := req.PathParameter("namespace")

	// Perform a sub protocol negotiation, ensuring tha client and the server agree on how
	// to handle communications over the port forwarded connection.
	request := req.Request
	respWriter := resp.ResponseWriter
	_, err := httpstream.Handshake(request, respWriter, []string{portforward.PortForwardProtocolV1Name})
	if err != nil {

	}

	// Create a channel where to handle http streams that will be generated for each subsequent
	// operations over the port forwarded connection.
	streamChan := make(chan httpstream.Stream, 1)

	// Upgrade the connection specifying what to do when a new http stream is received.
	// After being received, the new stream will be published into the stream channel for handling.
	upgrader := spdy.NewResponseUpgrader()
	conn := upgrader.UpgradeResponse(respWriter, request, gportforward.HttpStreamReceived(streamChan))
	if conn == nil {

	}
	defer func() {
		_ = conn.Close()
	}()
	conn.SetIdleTimeout(10 * time.Minute)

	// Given that in the goofy provider there is no real infrastructure, and thus no real workload cluster,
	// we are going to forward all the connection back to the same server (the goofy controller pod).
	_, serverPort, err := net.SplitHostPort(req.Request.Host)
	if err != nil {

	}

	// Start the process handling streams that are published in the stream channel, please note that:
	// - The connection with the target will be established only when the first operation will be executed
	// - Following operations will re-use the same connection.
	streamHandler := gportforward.NewHttpStreamHandler(
		conn,
		streamChan,
		podName,
		podNamespace,
		func(ctx context.Context, podName, podNamespace, _ string, stream io.ReadWriteCloser) error {
			return doPortForward(ctx, podName, podNamespace, serverPort, stream)
		},
	)
	streamHandler.Run(context.Background())
}

// doPortForward establish a connection to the target of the port forward operation,  and sets up
// a bi-directional copy of data.
// In the case of this provider, the target endpoint is always on the same server (the goofy controller pod).
func doPortForward(ctx context.Context, _, _, port string, stream io.ReadWriteCloser) error {
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
