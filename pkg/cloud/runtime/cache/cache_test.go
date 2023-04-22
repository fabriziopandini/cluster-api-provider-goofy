package cache

import (
	"context"
	"encoding/json"
	"fmt"
	cloudv1 "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/api/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"math/rand"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var scheme = runtime.NewScheme()

func init() {
	_ = cloudv1.AddToScheme(scheme)
}

func Test_cache_scale(t *testing.T) {
	t.Skip()

	// ctrl.SetLogger(NewLoggerWithT(t))

	resourceGroups := 1000
	objectsForResourceGroups := 500
	operationFrequencyForResourceGroup := 10 * time.Millisecond
	testDuration := 2 * time.Minute

	var createCount uint64
	var getCount uint64
	var listCount uint64
	var deleteCount uint64

	rand.Seed(time.Now().UnixNano())
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	c := NewCache(scheme).(*cache)
	c.syncPeriod = testDuration / 10                        // force a shorter sync period
	c.garbageCollectorRequeueAfter = 500 * time.Millisecond // force a shorter gc requeueAfter
	err := c.Start(ctx)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return c.started
	}, 5*time.Second, 200*time.Millisecond, "manager should start")

	machineName := func(j int) string {
		return fmt.Sprintf("machine-%d", j)
	}

	for i := 0; i < resourceGroups; i++ {
		resourceGroup := fmt.Sprintf("resourceGroup-%d", i)
		c.AddResourceGroup(resourceGroup)

		go func() {
			// log := ctrl.LoggerFrom(ctx)
			for {
				select {
				case <-time.After(wait.Jitter(operationFrequencyForResourceGroup, 1)):
					operation := rand.Intn(3)
					item := rand.Intn(objectsForResourceGroups)
					switch operation {
					case 0: // create or get
						machine := &cloudv1.CloudMachine{
							ObjectMeta: metav1.ObjectMeta{
								Name: machineName(item),
							},
						}
						err := c.Create(resourceGroup, machine)
						if errors.IsAlreadyExists(err) {
							if err = c.Get(resourceGroup, types.NamespacedName{Name: machineName(item)}, machine); err == nil {
								// log.Info("get", "resourceGroup", resourceGroup.GetName(), "Etcd", etcd.Name)
								atomic.AddUint64(&getCount, 1)
								continue
							}
						}
						require.NoError(t, err)
						// log.Info("create", "resourceGroup", resourceGroup.GetName(), "Etcd", etcd.Name)
						atomic.AddUint64(&createCount, 1)
					case 1: // list
						obj := &cloudv1.CloudMachineList{}
						err := c.List(resourceGroup, obj)
						require.NoError(t, err)
						// log.Info("list", "resourceGroup", resourceGroup.GetName(), "Kind", "Etcd")
						atomic.AddUint64(&listCount, 1)
					case 2: // delete
						require.NoError(t, err)
						machine := &cloudv1.CloudMachine{
							ObjectMeta: metav1.ObjectMeta{
								Name: machineName(item),
							},
						}
						err := c.Delete(resourceGroup, machine)
						if errors.IsNotFound(err) {
							continue
						}
						require.NoError(t, err)
						// log.Info("delete", "resourceGroup", resourceGroup.GetName(), "Etcd", etcd.Name)
						atomic.AddUint64(&deleteCount, 1)
					}

				case <-ctx.Done():
					return
				}
			}
		}()
	}

	time.Sleep(1 * time.Minute)

	t.Log("createCount", createCount, "getCount", getCount, "listCount", listCount, "deleteCount", deleteCount)

	cancel()
}

// TODO: move somewhere else

func NewLoggerWithT(t *testing.T) logr.Logger {
	return logr.New(&tLogger{
		T: t,
	})
}

var _ logr.LogSink = &tLogger{}

type tLogger struct {
	*testing.T
	names  []string
	values []interface{}
}

func (f *tLogger) Init(_ logr.RuntimeInfo) {
}

func (f *tLogger) Enabled(_ int) bool { return true }

func (f *tLogger) WithName(name string) logr.LogSink {
	names := append([]string(nil), f.names...)
	names = append(names, name)
	return &tLogger{
		names:  names,
		values: f.values,
		T:      f.T,
	}
}

func (f *tLogger) WithValues(vals ...interface{}) logr.LogSink {
	newValues := append([]interface{}(nil), f.values...)
	newValues = append(newValues, vals...)
	return &tLogger{
		names:  f.names,
		values: newValues,
		T:      f.T,
	}
}

func (f *tLogger) Error(err error, msg string, values ...interface{}) {
	errorValues := append([]interface{}(nil), f.values...)
	errorValues = append(errorValues, "error", err)
	errorValues = append(errorValues, values...)
	f.log(true, msg, errorValues)
}

func (f *tLogger) Info(_ int, msg string, values ...interface{}) {
	infoValues := append([]interface{}(nil), f.values...)
	infoValues = append(infoValues, values...)
	f.log(false, msg, infoValues)
}

func (f *tLogger) log(isError bool, msg string, values []interface{}) {
	out := ""
	if len(f.names) > 0 {
		out += fmt.Sprintf("[%s] ", strings.Join(f.names, ":"))
	}
	if isError {
		out += "ERROR "
	}
	out += msg
	for i := 0; i < len(values)-1; i = i + 2 {
		k := values[i]
		v := values[i+1]
		switch v.(type) {
		case int:
			out += fmt.Sprintf(" %q=\"%d\"", k, v)
		case string:
			out += fmt.Sprintf(" %q=%q", k, v)
		default:
			b, err := json.Marshal(v)
			if err != nil {
				continue
			}
			out += fmt.Sprintf(" %q=%q", k, string(b))
		}
	}
	f.Log(out)
}
