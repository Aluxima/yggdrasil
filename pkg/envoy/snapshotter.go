package envoy

import (
	"context"

	cache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"

	"github.com/uswitch/yggdrasil/pkg/k8s"
)

//Configurator is an interface that implements Generate and NodeID
type Configurator interface {
	Generate([]v1beta1.Ingress, []*v1.Secret) cache.Snapshot
	NodeID() string
}

//Snapshotter watches for Ingress changes and updates the
//config snapshot
type Snapshotter struct {
	snapshotCache cache.SnapshotCache
	configurator  Configurator
	lister        *k8s.IngressAggregator
	secretsLister *k8s.SecretsAggregator
}

//NewSnapshotter returns a new Snapshotter
func NewSnapshotter(snapshotCache cache.SnapshotCache, config Configurator, lister *k8s.IngressAggregator, secretsLister *k8s.SecretsAggregator) *Snapshotter {
	return &Snapshotter{
		snapshotCache: snapshotCache,
		configurator:  config,
		lister:        lister,
		secretsLister: secretsLister}
}

func (s *Snapshotter) snapshot() error {
	ingresses, err := s.lister.List()
	secrets, secErr := s.secretsLister.List()
	if err != nil {
		return err
	}
	if secErr != nil {
		return secErr
	}
	snapshot := s.configurator.Generate(ingresses, secrets)

	log.Debugf("took snapshot: %+v", snapshot)

	s.snapshotCache.SetSnapshot(s.configurator.NodeID(), snapshot)
	return nil
}

//Run will periodically refresh the snapshot
func (s *Snapshotter) Run(ctx context.Context) {
	go func() {
		for {
			select {
			// TODO stop snapshotting at each resource update, do bulk
			case <-s.secretsLister.Events():
			case <-s.lister.Events():
				s.snapshot()
			case <-ctx.Done():
				return
			}
		}
	}()
	log.Infof("started snapshotter")
}
