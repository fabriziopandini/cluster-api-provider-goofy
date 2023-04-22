package server

import (
	"context"
	"fmt"
	"github.com/amahi/spdy"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/gorilla/mux"
	"io"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"log"
	"net/http"
	"time"
)

// TODO: TLS https://medium.com/@satrobit/how-to-build-https-servers-with-certificate-lazy-loading-in-go-bff5e9ef2f1f

type Server struct {
	scheme *runtime.Scheme

	clusters []string

	started bool
}

func New(scheme *runtime.Scheme) (*Server, error) {
	return &Server{
		scheme: scheme,
	}, nil
}

func (s *Server) Start(ctx context.Context) error {
	router := mux.NewRouter()
	apiServer := router.PathPrefix("/clusters").Subrouter()
	apiServer.Path("/{cluster}/api").Methods("GET").HandlerFunc(s.apiDiscovery)
	apiServer.Path("/{cluster}/api/v1").Methods("GET").HandlerFunc(s.apiV1Discovery)
	apiServer.Path("/{cluster}/api/v1/{resource}").Methods("GET").HandlerFunc(s.apiV1List)
	apiServer.Path("/{cluster}/api/v1/{resource}/{name}").Methods("GET").HandlerFunc(s.apiV1Get)
	apiServer.Path("/{cluster}/api/v1/{resource}/{name}").Methods("DELETE").HandlerFunc(s.apiV1Delete)
	apiServer.Path("/{cluster}/api/v1/{resource}/{name}").Methods("PATCH").HandlerFunc(s.apiV1Patch)
	apiServer.Path("/{cluster}/api/v1/namespaces/{namespace}/{resource}/{name}/portforward").Methods("POST").HandlerFunc(s.apiV1PortForward)
	apiServer.Path("/{cluster}/apis").Methods("GET").HandlerFunc(s.discoveryApis)

	router.PathPrefix("/").HandlerFunc(s.catchAllHandler)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	// Run our server in a goroutine so that it doesn't block.
	listenAndServe := false
	go func() {
		listenAndServe = true
		if err := srv.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	router2 := mux.NewRouter()
	router2.PathPrefix("/").HandlerFunc(s.catchAllHandler)

	go func() {
		if err := spdy.ListenAndServe(":8081", router2); err != nil {
			log.Println(err)
		}

	}()

	go func() {
		<-ctx.Done()

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	if err := wait.PollImmediate(50*time.Millisecond, 5*time.Second, func() (done bool, err error) {
		if !listenAndServe {
			return false, nil
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to start server: %v", err)
	}
	return nil
}

func (s *Server) RegisterCluster(name string) {
	s.clusters = append(s.clusters, name)
}

func (s *Server) apiDiscovery(w http.ResponseWriter, r *http.Request) {
	// vars := mux.Vars(r)
	// log.Println(vars)
	apiVersions := &metav1.APIVersions{
		Versions: []string{"v1"},
	}

	s.fprintObj(w, apiVersions, metav1.SchemeGroupVersion)
	log.Printf("%d %s %s\n", http.StatusOK, r.Method, r.URL.String())
}

func (s *Server) apiV1Discovery(w http.ResponseWriter, r *http.Request) {
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

	s.fprintObj(w, apiResourceList, metav1.SchemeGroupVersion)
	log.Printf("%d %s %s\n", http.StatusOK, r.Method, r.URL.String())
}

func (s *Server) apiV1List(w http.ResponseWriter, r *http.Request) {
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

	s.fprintObj(w, nodeList, corev1.SchemeGroupVersion)
	log.Printf("%d %s %s\n", http.StatusOK, r.Method, r.URL.String())
}

func (s *Server) apiV1Get(w http.ResponseWriter, r *http.Request) {
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

	s.fprintObj(w, node, corev1.SchemeGroupVersion)
	log.Printf("%d %s %s\n", http.StatusOK, r.Method, r.URL.String())
}

func (s *Server) apiV1Patch(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	patchData, _ := io.ReadAll(r.Body)

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
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, err.Error())
		return
	}

	originalObjJS, err := runtime.Encode(encoder, obj)
	if err != nil {

	}

	var changedJS []byte
	patchType := r.Header.Get("Content-Type")
	switch types.PatchType(patchType) {
	case types.MergePatchType:
	case types.StrategicMergePatchType:

		changedJS, err = jsonpatch.MergePatch(originalObjJS, patchData)
		if err != nil {

		}

	case types.JSONPatchType:
	case types.ApplyPatchType:
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "unsupported patch type %s\n", patchType)
		return
	}

	codecFactory := serializer.NewCodecFactory(s.scheme)
	err = runtime.DecodeInto(codecFactory.UniversalDecoder(), changedJS, obj)
	if err != nil {

	}

	s.fprintObj(w, obj, corev1.SchemeGroupVersion)
	log.Printf("%d %s %s\n", http.StatusOK, r.Method, r.URL.String())
}

func (s *Server) apiV1PortForward(w http.ResponseWriter, r *http.Request) {
	log.Printf("%d %s %s\n", http.StatusMovedPermanently, r.Method, r.URL.String())
	http.Redirect(w, r, "http://localhost:8081", http.StatusMovedPermanently)
}

func (s *Server) apiV1Delete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", runtime.ContentTypeJSON)
	w.WriteHeader(http.StatusOK)
	log.Printf("%d %s %s\n", http.StatusOK, r.Method, r.URL.String())
}

func (s *Server) discoveryApis(w http.ResponseWriter, r *http.Request) {
	apiGroupList := &metav1.APIGroupList{}

	s.fprintObj(w, apiGroupList, metav1.SchemeGroupVersion)
	log.Printf("%d %s %s\n", http.StatusOK, r.Method, r.URL.String())
}

func (s *Server) fprintObj(w http.ResponseWriter, obj runtime.Object, gv runtime.GroupVersioner) {
	encoder, err := s.getEncoder(obj, gv)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, err.Error())
		return
	}

	data, err := runtime.Encode(encoder, obj)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "failed to encode %T: %v\n", obj, err)
		return
	}

	w.Header().Set("Content-Type", runtime.ContentTypeJSON)
	w.WriteHeader(http.StatusOK)
	w.Write(data)
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

func (s *Server) catchAllHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	log.Printf("%d %s %s\n", http.StatusNotFound, r.Method, r.URL.String())
}
