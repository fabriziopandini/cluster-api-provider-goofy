package api

import (
	"context"
	"fmt"
	"github.com/emicklei/go-restful/v3"
	cmanager "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/manager"
	gportforward "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server/api/portforward"
	"github.com/go-logr/logr"
	"io"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/client-go/tools/portforward"
	"net"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
	"time"
)

// ResourceGroupResolver defines a func that can identify which workloadCluster/resourceGroup a
// request targets to.
type ResourceGroupResolver func(host string) (string, error)

// NewAPIServerHandler returns an http.Handler for fake API server.
func NewAPIServerHandler(manager cmanager.Manager, log logr.Logger, resolver ResourceGroupResolver) http.Handler {
	apiServer := &apiServerHandler{
		container:             restful.NewContainer(),
		manager:               manager,
		log:                   log,
		resourceGroupResolver: resolver,
	}

	apiServer.container.Filter(apiServer.globalLogging)

	ws := new(restful.WebService)
	ws.Consumes(runtime.ContentTypeJSON)
	ws.Produces(runtime.ContentTypeJSON)

	// Discovery endpoints
	ws.Route(ws.GET("/api").To(apiServer.apiDiscovery))
	ws.Route(ws.GET("/api/v1").To(apiServer.apiV1Discovery))
	ws.Route(ws.GET("/apis").To(apiServer.apisDiscovery))

	// CRUD endpoints (global objects)
	// ws.Route(ws.POST("/api/v1/{resource}").Reads().Consumes(runtime.ContentTypeProtobuf).Filter(apiServer.routeLogging).To(apiServer.apiV1Create))
	ws.Route(ws.GET("/api/v1/{resource}").To(apiServer.apiV1List))
	ws.Route(ws.GET("/api/v1/{resource}/{name}").To(apiServer.apiV1Get))
	ws.Route(ws.PATCH("/api/v1/{resource}/{name}").Consumes(string(types.MergePatchType), string(types.StrategicMergePatchType)).To(apiServer.apiV1Patch))
	ws.Route(ws.DELETE("/api/v1/{resource}/{name}").Consumes(runtime.ContentTypeProtobuf).To(apiServer.apiV1Delete))

	// TODO: CRUD endpoints (namespaced objects)
	// TODO: create
	// ws.Route(ws.GET("/api/v1/namespaces/{namespace}/{resource}").To(apiServer.apiV1List))
	// ws.Route(ws.GET("/api/v1/namespaces/{namespace}/{resource}/{name}").To(apiServer.apiV1Get))
	// ws.Route(ws.PATCH("/api/v1/namespaces/{namespace}/{resource}/{name}").Consumes(string(types.MergePatchType), string(types.StrategicMergePatchType)).To(apiServer.apiV1Patch))
	// ws.Route(ws.DELETE("/api/v1/namespaces/{namespace}/{resource}/{name}").Consumes(runtime.ContentTypeProtobuf).To(apiServer.apiV1Delete))

	// Port forward endpoints
	ws.Route(ws.GET("/api/v1/namespaces/{namespace}/pods/{name}/portforward").To(apiServer.apiV1PortForward))
	ws.Route(ws.POST("/api/v1/namespaces/{namespace}/pods/{name}/portforward").Consumes("*/*").To(apiServer.apiV1PortForward))

	apiServer.container.Add(ws)

	return apiServer
}

type apiServerHandler struct {
	container             *restful.Container
	manager               cmanager.Manager
	log                   logr.Logger
	resourceGroupResolver ResourceGroupResolver
}

func (h *apiServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.container.ServeHTTP(w, r)
}

func (h *apiServerHandler) globalLogging(req *restful.Request, resp *restful.Response, chain *restful.FilterChain) {
	fmt.Printf("Serving %s %s %s\n", req.Request.Method, req.Request.URL, req.HeaderParameter("Content-Type"))
	h.log.Info("Serving", "method", req.Request.Method, "url", req.Request.URL, "contentType", req.HeaderParameter("Content-Type"))
	chain.ProcessFilter(req, resp)
}

func (h *apiServerHandler) routeLogging(req *restful.Request, resp *restful.Response, chain *restful.FilterChain) {
	fmt.Printf("Route %s %s %s\n", req.Request.Method, req.Request.URL, req.HeaderParameter("Content-Type"))

	h.log.Info("Route selected", "method", req.Request.Method, "url", req.Request.URL, "contentType", req.HeaderParameter("Content-Type"), "selectedRoutePath", req.SelectedRoutePath())
	chain.ProcessFilter(req, resp)
}

func (h *apiServerHandler) apiDiscovery(req *restful.Request, resp *restful.Response) {
	if err := resp.WriteEntity(apiVersions); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}

func (h *apiServerHandler) apiV1Discovery(req *restful.Request, resp *restful.Response) {
	if err := resp.WriteEntity(apiResourceList); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}

func (h *apiServerHandler) apisDiscovery(req *restful.Request, resp *restful.Response) {
	if err := resp.WriteEntity(apiGroupList); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}

func (h *apiServerHandler) apiV1Create(req *restful.Request, resp *restful.Response) {
	// TODO: protobuf is used for core types:
	//  - figure out how to make restful to work with protibuf (it accepts it, but it tries to convert the object from yaml/json)
	//  - find out where protobuf definition for core types are defined, and how to unmarshal
	ctx := req.Request.Context()

	// Gets the resource group the request targets to (the resolver is aware of the mapping host<->resourceGroup)
	resourceGroup, err := h.resourceGroupResolver(req.Request.Host)
	if err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	// Gets at client to the resource group.
	cloudClient := h.manager.GetResourceGroup(resourceGroup).GetClient()

	// Gets the object from the request
	defer req.Request.Body.Close()
	objData, _ := io.ReadAll(req.Request.Body)

	// Maps the requested resource to a Kind
	var m map[string]interface{}
	if err := yaml.Unmarshal(objData, &m); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	obj := &unstructured.Unstructured{}
	obj.SetUnstructuredContent(m)
	obj.SetAPIVersion(corev1.SchemeGroupVersion.String())
	obj.SetKind(resourceToKind(req.PathParameter("resource")))
	if obj.GetKind() == "" {
		_ = resp.WriteErrorString(http.StatusInternalServerError, fmt.Sprintf("failed to get kind for resource %s", req.PathParameter("resource")))
		return
	}

	// Create the object
	if err := cloudClient.Create(ctx, obj); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	// TODO: check if patch have to return something in body
	// TODO: think about making create, update, patch and delete to change the object in input
}

func (h *apiServerHandler) apiV1List(req *restful.Request, resp *restful.Response) {
	ctx := req.Request.Context()

	// Gets the resource group the request targets to (the resolver is aware of the mapping host<->resourceGroup)
	resourceGroup, err := h.resourceGroupResolver(req.Request.Host)
	if err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	// Gets at client to the resource group.
	cloudClient := h.manager.GetResourceGroup(resourceGroup).GetClient()

	// Maps the requested resource to a Kind
	list := &unstructured.UnstructuredList{}
	list.SetAPIVersion(corev1.SchemeGroupVersion.String())
	list.SetKind(fmt.Sprintf("%sList", resourceToKind(req.PathParameter("resource"))))
	if list.GetKind() == "" {
		_ = resp.WriteErrorString(http.StatusInternalServerError, fmt.Sprintf("failed to get kind for resource %s", req.PathParameter("resource")))
		return
	}

	// Reads and returns the requested data.
	if err := cloudClient.List(ctx, list); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	if err := resp.WriteEntity(list); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}

func (h *apiServerHandler) apiV1Get(req *restful.Request, resp *restful.Response) {
	ctx := req.Request.Context()

	// Gets the resource group the request targets to (the resolver is aware of the mapping host<->resourceGroup)
	resourceGroup, err := h.resourceGroupResolver(req.Request.Host)
	if err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	// Gets at client to the resource group.
	cloudClient := h.manager.GetResourceGroup(resourceGroup).GetClient()

	// Maps the requested resource to a Kind
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(corev1.SchemeGroupVersion.String())
	obj.SetKind(resourceToKind(req.PathParameter("resource")))
	if obj.GetKind() == "" {
		_ = resp.WriteErrorString(http.StatusInternalServerError, fmt.Sprintf("failed to get kind for resource %s", req.PathParameter("resource")))
		return
	}

	// Reads and returns the requested data.
	key := client.ObjectKey{
		Name: req.PathParameter("name"),
	}
	if err := cloudClient.Get(ctx, key, obj); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	if err := resp.WriteEntity(obj); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}

func (h *apiServerHandler) apiV1Patch(req *restful.Request, resp *restful.Response) {
	ctx := req.Request.Context()

	// Gets the resource group the request targets to (the resolver is aware of the mapping host<->resourceGroup)
	resourceGroup, err := h.resourceGroupResolver(req.Request.Host)
	if err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	// Gets at client to the resource group.
	cloudClient := h.manager.GetResourceGroup(resourceGroup).GetClient()

	// Maps the requested resource to a Kind
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(corev1.SchemeGroupVersion.String())
	obj.SetKind(resourceToKind(req.PathParameter("resource")))
	if obj.GetKind() == "" {
		_ = resp.WriteErrorString(http.StatusInternalServerError, fmt.Sprintf("failed to get kind for resource %s", req.PathParameter("resource")))
		return
	}

	// Gets the patch from the request
	defer req.Request.Body.Close()
	patchData, _ := io.ReadAll(req.Request.Body)
	patchType := types.PatchType(req.HeaderParameter("Content-Type"))
	patch := client.RawPatch(patchType, patchData)

	// Applies the Patch.
	obj.SetName(req.PathParameter("name"))
	if err := cloudClient.Patch(ctx, obj, patch); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	if err := resp.WriteEntity(obj); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}

func (h *apiServerHandler) apiV1Delete(req *restful.Request, resp *restful.Response) {
	ctx := req.Request.Context()

	// Gets the resource group the request targets to (the resolver is aware of the mapping host<->resourceGroup)
	resourceGroup, err := h.resourceGroupResolver(req.Request.Host)
	if err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	// Gets at client to the resource group.
	cloudClient := h.manager.GetResourceGroup(resourceGroup).GetClient()

	// Maps the requested resource to a Kind
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(corev1.SchemeGroupVersion.String())
	obj.SetKind(resourceToKind(req.PathParameter("resource")))
	if obj.GetKind() == "" {
		_ = resp.WriteErrorString(http.StatusInternalServerError, fmt.Sprintf("failed to get kind for resource %s", req.PathParameter("resource")))
		return
	}
	obj.SetName(req.PathParameter("name"))

	// Reads and returns the requested data.
	if err := cloudClient.Delete(ctx, obj); err != nil {
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
