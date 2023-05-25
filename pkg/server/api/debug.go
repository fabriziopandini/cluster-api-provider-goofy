package api

import (
	"net/http"

	"github.com/emicklei/go-restful/v3"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"

	cmanager "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/manager"
)

// DebugInfoProvider defines the list of method the server must implement
// to provide debug info.
type DebugInfoProvider interface {
	ListProviders() map[string]string
}

// NewDebugHandler returns an http.Handler for debugging the server.
func NewDebugHandler(manager cmanager.Manager, log logr.Logger, infoProvider DebugInfoProvider) http.Handler {
	debugServer := &debugHandler{
		container:    restful.NewContainer(),
		manager:      manager,
		log:          log,
		infoProvider: infoProvider,
	}

	ws := new(restful.WebService)
	ws.Produces(runtime.ContentTypeJSON)

	// Discovery endpoints
	ws.Route(ws.GET("/listeners").To(debugServer.listenersList))

	debugServer.container.Add(ws)

	return debugServer
}

type debugHandler struct {
	container    *restful.Container
	manager      cmanager.Manager
	log          logr.Logger
	infoProvider DebugInfoProvider
}

func (h *debugHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.container.ServeHTTP(w, r)
}

func (h *debugHandler) listenersList(_ *restful.Request, resp *restful.Response) {
	providers := h.infoProvider.ListProviders()

	if err := resp.WriteEntity(providers); err != nil {
		_ = resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
}
