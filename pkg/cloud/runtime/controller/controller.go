package controller

import (
	"context"
	"fmt"
	chandler "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/handler"
	cpredicate "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/predicate"
	creconciler "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/reconciler"
	csource "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/source"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sync"
	"time"
)

// Options are the arguments for creating a new Controller.
type Options struct {
	Concurrency int
	Reconciler  creconciler.Reconciler
}

type Controller interface {
	Watch(src csource.Source, evthdler chandler.EventHandler, prct ...cpredicate.Predicate) error
	Start(ctx context.Context) error
}

func New(name string, options Options) (Controller, error) {
	if name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}

	if options.Reconciler == nil {
		return nil, fmt.Errorf("reconciler cannot be nil")
	}

	if options.Concurrency < 1 {
		return nil, fmt.Errorf("concurrency cannot be less then 1")
	}

	return &controller{
		name:             name,
		reconciler:       options.Reconciler,
		concurrency:      options.Concurrency,
		cacheSyncTimeout: 2 * time.Minute,
	}, nil
}

type controller struct {
	name             string
	reconciler       creconciler.Reconciler
	concurrency      int
	cacheSyncTimeout time.Duration // TODO: make this an option

	queue workqueue.RateLimitingInterface

	lock         sync.Mutex
	startWatches []watchDescription
	started      bool

	ctx context.Context
}

type watchDescription struct {
	src        csource.Source
	handler    chandler.EventHandler
	predicates []cpredicate.Predicate
}

func (c *controller) Watch(src csource.Source, evthdler chandler.EventHandler, prct ...cpredicate.Predicate) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	if !c.started {
		c.startWatches = append(c.startWatches, watchDescription{src: src, handler: evthdler, predicates: prct})
		return nil
	}

	log := ctrl.LoggerFrom(c.ctx)
	log.Info("Starting EventSource", "source", src)
	return src.Start(c.ctx, evthdler, c.queue, prct...)
}

func (c *controller) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}

	if c.started {
		return fmt.Errorf("controller started more than once")
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	log := ctrl.LoggerFrom(ctx).WithValues("controller", c.name)
	ctx = ctrl.LoggerInto(ctx, log)
	c.ctx = ctx

	log.Info("Starting controller queue")
	c.queue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	go func() {
		<-ctx.Done()
		c.queue.ShutDown()
		// log.Info("Controller queue stopped")
	}()

	for _, watch := range c.startWatches {
		log.Info("Starting EventSource", "source", watch.src)
		if err := watch.src.Start(ctx, watch.handler, c.queue, watch.predicates...); err != nil {
			return err
		}
	}

	c.startWatches = nil

	workers := 0
	go func() {
		log.Info("Starting reconcile workers", "count", c.concurrency)
		wg := &sync.WaitGroup{}
		wg.Add(c.concurrency)
		for i := 0; i < c.concurrency; i++ {
			go func() {
				workers += 1
				defer wg.Done()
				for c.processNextWorkItem(ctx) {
				}
			}()
		}

		<-ctx.Done()
		// log.Info("Shutdown signal received, waiting for all workers to finish")
		wg.Wait()
		// log.Info("All workers finished")
	}()

	if err := wait.PollImmediate(50*time.Millisecond, 5*time.Second, func() (done bool, err error) {
		if workers < c.concurrency {
			return false, nil
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to start reconcile workers: %v", err)
	}
	c.started = true
	return nil
}

func (c *controller) processNextWorkItem(ctx context.Context) bool {
	obj, shutdown := c.queue.Get()
	if shutdown {
		return false
	}

	defer c.queue.Done(obj)

	c.reconcileHandler(ctx, obj)
	return true
}

func (c *controller) reconcileHandler(ctx context.Context, obj interface{}) {
	req, ok := obj.(creconciler.Request)
	if !ok {
		c.queue.Forget(obj)
		return
	}

	// TODO: Inject logger

	result, err := c.reconciler.Reconcile(ctx, req)
	switch {
	case err != nil:
		c.queue.AddRateLimited(req)
	case result.RequeueAfter > 0:
		c.queue.Forget(obj)
		c.queue.AddAfter(req, result.RequeueAfter)
	case result.Requeue:
		c.queue.AddRateLimited(req)
	default:
		c.queue.Forget(obj)
	}
}
