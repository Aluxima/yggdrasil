package k8s

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

type SecretsLister interface {
	List() ([]v1.Secret, error)
}

//SecretsAggregator used for running Secrets infomers
type SecretsAggregator struct {
	stores      []cache.Store
	controllers []cache.Controller
	events      chan interface{}
}

func (i *SecretsAggregator) Events() chan interface{} {
	return i.events
}

//Run starts all the Secrets informers. Will block until all controllers
//have synced. Shouldn't be called in go routine
func (i *SecretsAggregator) Run(ctx context.Context) error {
	for _, c := range i.controllers {
		logrus.Debugf("starting cache controller: %+v", c)
		go c.Run(ctx.Done())
		cache.WaitForCacheSync(ctx.Done(), c.HasSynced)
		logrus.Debugf("cache controller synced")
	}
	return nil
}

// func dumpSecret(action string, obj interface{}) {
// 	secret, ok := obj.(*v1.Secret)
// 	if !ok {
// 		logrus.Errorf("secret error %+v", obj)
// 		return
// 	}
// 	logrus.Infof("%s secret %s/%s", action, secret.Namespace, secret.Name)
// }

func (i *SecretsAggregator) OnAdd(obj interface{}) {
	i.events <- obj
	// dumpSecret("added", obj)
	logrus.Debugf("adding %+v", obj)
}

func (i *SecretsAggregator) OnDelete(obj interface{}) {
	i.events <- obj
	// dumpSecret("deleted", obj)
	logrus.Debugf("deleting %+v", obj)
}

func (i *SecretsAggregator) OnUpdate(old, new interface{}) {
	i.events <- new
	// dumpSecret("updated", new)
	logrus.Debugf("updating %+v", new)
}

//AddSource adds a new source for watching secrets, must be called before running
func (i *SecretsAggregator) AddSource(source cache.ListerWatcher) {
	//Todo implement handler for events
	//Todo + SharedIndexerInformerFactory WithTweakListOptions ?
	store, controller := cache.NewIndexerInformer(source, &v1.Secret{}, time.Minute, i, cache.Indexers{})
	i.stores = append(i.stores, store)
	i.controllers = append(i.controllers, controller)
}

//NewSecretsAggregator returns a new SecretsAggregator
func NewSecretsAggregator(sources []cache.ListerWatcher) *SecretsAggregator {
	a := &SecretsAggregator{
		events: make(chan interface{}),
	}
	for _, s := range sources {
		a.AddSource(s)
	}
	return a
}

//List returns all secrets
func (i *SecretsAggregator) List() ([]*v1.Secret, error) {
	is := make([]*v1.Secret, 0)
	for _, store := range i.stores {
		secrets := store.List()
		for _, obj := range secrets {
			secret, ok := obj.(*v1.Secret)
			if !ok {
				return nil, fmt.Errorf("unexpected object in store: %+v", obj)
			}
			is = append(is, secret)
		}
	}
	return is, nil
}
