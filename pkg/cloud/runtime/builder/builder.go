package builder

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	ccontroller "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/controller"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/handler"
	cmanager "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/manager"
	cpredicate "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/predicate"
	creconciler "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/reconcile"
	csource "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/source"
)

type Builder struct {
	forInput         ForInput
	watchesInput     []WatchesInput
	mgr              cmanager.Manager
	globalPredicates []cpredicate.Predicate
	ctrl             ccontroller.Controller
	ctrlOptions      ccontroller.Options
	name             string
}

func ControllerManagedBy(m cmanager.Manager) *Builder {
	return &Builder{mgr: m}
}

type ForInput struct {
	object     client.Object
	predicates []cpredicate.Predicate
	err        error
}

func (blder *Builder) For(obj client.Object, opts ...ForOption) *Builder {
	if blder.forInput.object != nil {
		blder.forInput.err = fmt.Errorf("method For(...) should only be called once, could not assign multiple object for reconciliation")
		return blder
	}
	input := ForInput{object: obj}
	for _, opt := range opts {
		opt.ApplyToFor(&input)
	}

	blder.forInput = input
	return blder
}

type WatchesInput struct {
	src          csource.Source
	eventhandler handler.EventHandler
	predicates   []cpredicate.Predicate
}

func (blder *Builder) Watches(src csource.Source, eventhandler handler.EventHandler, opts ...WatchesOption) *Builder {
	input := WatchesInput{src: src, eventhandler: eventhandler}
	for _, opt := range opts {
		opt.ApplyToWatches(&input)
	}

	blder.watchesInput = append(blder.watchesInput, input)
	return blder
}

func (blder *Builder) WithEventFilter(p cpredicate.Predicate) *Builder {
	blder.globalPredicates = append(blder.globalPredicates, p)
	return blder
}

func (blder *Builder) WithOptions(options ccontroller.Options) *Builder {
	blder.ctrlOptions = options
	return blder
}

func (blder *Builder) Named(name string) *Builder {
	blder.name = name
	return blder
}

func (blder *Builder) Complete(r creconciler.Reconciler) error {
	_, err := blder.Build(r)
	return err
}

func (blder *Builder) Build(r creconciler.Reconciler) (ccontroller.Controller, error) {
	if r == nil {
		return nil, fmt.Errorf("must provide a non-nil Reconciler")
	}
	if blder.mgr == nil {
		return nil, fmt.Errorf("must provide a non-nil Manager")
	}
	if blder.forInput.err != nil {
		return nil, blder.forInput.err
	}

	// Set the ControllerManagedBy
	if err := blder.doController(r); err != nil {
		return nil, err
	}

	// Set the Watch
	if err := blder.doWatch(); err != nil {
		return nil, err
	}

	return blder.ctrl, nil
}

func (blder *Builder) doWatch() error {
	// Reconcile type
	if blder.forInput.object != nil {
		i, err := blder.mgr.GetCache().GetInformer(context.TODO(), blder.forInput.object)
		if err != nil {
			return err
		}

		src := &csource.Kind{Type: blder.forInput.object, Informer: i}
		hdler := &handler.EnqueueRequestForObject{}
		allPredicates := append(blder.globalPredicates, blder.forInput.predicates...)
		if err := blder.ctrl.Watch(src, hdler, allPredicates...); err != nil {
			return err
		}
	}

	// Do the watch requests
	if len(blder.watchesInput) == 0 && blder.forInput.object == nil {
		return fmt.Errorf("there are no watches configured, controller will never get triggered. Use For(), Owns() or Watches() to set them up")
	}
	for _, w := range blder.watchesInput {
		allPredicates := append([]cpredicate.Predicate(nil), blder.globalPredicates...)
		allPredicates = append(allPredicates, w.predicates...)

		if err := blder.ctrl.Watch(w.src, w.eventhandler, allPredicates...); err != nil {
			return err
		}
	}
	return nil
}

func (blder *Builder) getControllerName(gvk schema.GroupVersionKind, hasGVK bool) (string, error) {
	if blder.name != "" {
		return blder.name, nil
	}
	if !hasGVK {
		return "", fmt.Errorf("one of For() or Named() must be called")
	}
	return strings.ToLower(gvk.Kind), nil
}

func (blder *Builder) doController(r creconciler.Reconciler) error {
	ctrlOptions := blder.ctrlOptions
	if ctrlOptions.Reconciler == nil {
		ctrlOptions.Reconciler = r
	}

	if ctrlOptions.Concurrency <= 0 {
		ctrlOptions.Concurrency = 1
	}

	var gvk schema.GroupVersionKind
	hasGVK := blder.forInput.object != nil
	if hasGVK {
		var err error
		gvk, err = apiutil.GVKForObject(blder.forInput.object, blder.mgr.GetScheme())
		if err != nil {
			return err
		}
	}

	controllerName, err := blder.getControllerName(gvk, hasGVK)
	if err != nil {
		return err
	}

	// Build the controller and return.
	blder.ctrl, err = ccontroller.New(controllerName, ctrlOptions)
	if err != nil {
		return err
	}

	if err := blder.mgr.AddController(blder.ctrl); err != nil {
		return err
	}
	return err
}
