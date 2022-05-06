package envoy

import (
	"log"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
)

func sortCluster(clusters []*cluster) {
	sort.Slice(clusters, func(i int, j int) bool {
		return clusters[i].identity() < clusters[j].identity()
	})
}

func ClustersEquals(a, b []*cluster) bool {
	if len(a) != len(b) {
		return false
	}

	sortCluster(a)
	sortCluster(b)

	for idx, cluster := range a {
		if !cluster.Equals(b[idx]) {
			return false
		}
	}

	return true
}

func sortVirtualHosts(hosts []*virtualHost) {
	sort.Slice(hosts, func(i int, j int) bool {
		return hosts[i].Host < hosts[j].Host
	})
}

func VirtualHostsEquals(a, b []*virtualHost) bool {
	if len(a) != len(b) {
		return false
	}

	sortVirtualHosts(a)
	sortVirtualHosts(b)

	for idx, hosts := range a {
		if !hosts.Equals(b[idx]) {
			return false
		}
	}

	return true
}

type envoyConfiguration struct {
	VirtualHosts []*virtualHost
	Clusters     []*cluster
}

type virtualHost struct {
	Host            string
	UpstreamCluster string
	Timeout         time.Duration
	PerTryTimeout   time.Duration
	TlsKey          string
	TlsCert         string
}

type vhostSecret struct {
	SecretName      string
	SecretNamespace string
}

func (v *virtualHost) Equals(other *virtualHost) bool {
	if other == nil {
		return false
	}

	return v.Host == other.Host &&
		v.Timeout == other.Timeout &&
		v.UpstreamCluster == other.UpstreamCluster &&
		v.PerTryTimeout == other.PerTryTimeout &&
		v.TlsCert == other.TlsCert &&
		v.TlsKey == other.TlsKey
}

type cluster struct {
	Name            string
	VirtualHost     string
	HealthCheckPath string
	Timeout         time.Duration
	Hosts           []string
}

func (c *cluster) identity() string {
	return c.Name
}

func (c *cluster) Equals(other *cluster) bool {
	if other == nil {
		return false
	}

	if c.Name != other.Name {
		return false
	}

	if c.Timeout != other.Timeout {
		return false
	}

	if c.VirtualHost != other.VirtualHost {
		return false
	}

	if c.HealthCheckPath != other.HealthCheckPath {
		return false
	}

	if len(c.Hosts) != len(other.Hosts) {
		return false
	}

	sort.Strings(c.Hosts)
	sort.Strings(other.Hosts)

	for i, host := range c.Hosts {
		if host != other.Hosts[i] {
			return false
		}
	}

	return true
}

func (cfg *envoyConfiguration) equals(oldCfg *envoyConfiguration) (vmatch bool, cmatch bool) {
	if oldCfg == nil {
		return false, false
	}
	return VirtualHostsEquals(cfg.VirtualHosts, oldCfg.VirtualHosts), ClustersEquals(cfg.Clusters, oldCfg.Clusters)
}

func classFilter(ingresses []v1beta1.Ingress, ingressClass []string) []v1beta1.Ingress {
	is := make([]v1beta1.Ingress, 0)

	for _, i := range ingresses {
		for _, class := range ingressClass {
			if i.GetAnnotations()["kubernetes.io/ingress.class"] == class {
				is = append(is, i)
			}
		}
	}
	matchingIngresses.Set(float64(len(is)))
	return is
}

func validIngressFilter(ingresses []v1beta1.Ingress) []v1beta1.Ingress {
	vi := make([]v1beta1.Ingress, 0)

Ingress:
	for _, i := range ingresses {
		for _, j := range i.Status.LoadBalancer.Ingress {
			if j.Hostname != "" || j.IP != "" {
				for _, k := range i.Spec.Rules {
					if k.Host != "" {
						vi = append(vi, i)
						continue Ingress
					}
				}
				logrus.Debugf("no host found in ingress config for: %+v in namespace: %+v", i.Name, i.Namespace)
				continue Ingress
			}
		}
		logrus.Debugf("no hostname or ip for loadbalancer found in ingress config for: %+v in namespace: %+v", i.Name, i.Namespace)
	}

	return vi
}

type envoyIngress struct {
	vhost   *virtualHost
	cluster *cluster
}

func newEnvoyIngress(host string) *envoyIngress {
	clusterName := strings.Replace(host, ".", "_", -1)
	return &envoyIngress{
		vhost: &virtualHost{
			Host:            host,
			UpstreamCluster: clusterName,
			Timeout:         (15 * time.Second),
			PerTryTimeout:   (5 * time.Second),
		},
		cluster: &cluster{
			Name:            clusterName,
			VirtualHost:     host,
			Hosts:           []string{},
			Timeout:         (30 * time.Second),
			HealthCheckPath: "",
		},
	}
}

func (ing *envoyIngress) addUpstream(host string) {
	ing.cluster.Hosts = append(ing.cluster.Hosts, host)
}

func (ing *envoyIngress) addHealthCheckPath(path string) {
	ing.cluster.HealthCheckPath = path
}

func (ing *envoyIngress) addTimeout(timeout time.Duration) {
	ing.cluster.Timeout = timeout
	ing.vhost.Timeout = timeout
	ing.vhost.PerTryTimeout = timeout
}

func getHostTlsSecret(ingress *v1beta1.Ingress, host string) *vhostSecret {
	for _, tls := range ingress.Spec.TLS {
		for _, h := range tls.Hosts {
			if h == host {
				return &vhostSecret{tls.SecretName, ingress.Namespace}
			}
		}
	}
	return nil
}

func (ing *envoyIngress) injectDynamicTls(vhostSecrets map[string]*vhostSecret, secrets []*v1.Secret) {
	vhostSecret, ok := vhostSecrets[ing.vhost.Host]
	if !ok {
		log.Printf("Error matching dynamic certificate for host '%s'", ing.vhost.Host)
		return
	}
	for _, secret := range secrets {
		if secret.Namespace == vhostSecret.SecretNamespace &&
			secret.Name == vhostSecret.SecretName {

			tlsCert, certOk := secret.Data["tls.crt"]
			tlsKey, keyOk := secret.Data["tls.key"]
			// TODO check that crt and key are valid
			if certOk && keyOk {
				ing.vhost.TlsCert = string(tlsCert)
				ing.vhost.TlsKey = string(tlsKey)
				return
			} else {
				logrus.Debugf("Incorrect secret %s/%s", secret.Namespace, secret.Name)
			}
		}
	}
	log.Printf("Error matching certificate for host '%s': no valid TLS secret '%s/%s' found", ing.vhost.Host, vhostSecret.SecretNamespace, vhostSecret.SecretName)
}

func translateIngresses(ingresses []v1beta1.Ingress, secrets []*v1.Secret) *envoyConfiguration {
	cfg := &envoyConfiguration{}
	envoyIngresses := map[string]*envoyIngress{}
	vhostSecrets := map[string]*vhostSecret{}

	for _, i := range ingresses {
		for _, j := range i.Status.LoadBalancer.Ingress {
			for _, rule := range i.Spec.Rules {
				_, ok := envoyIngresses[rule.Host]
				if !ok {
					envoyIngresses[rule.Host] = newEnvoyIngress(rule.Host)
				}

				envoyIngress := envoyIngresses[rule.Host]

				if j.Hostname != "" {
					envoyIngress.addUpstream(j.Hostname)
				} else {
					envoyIngress.addUpstream(j.IP)
				}

				if hostTlsSecret := getHostTlsSecret(&i, rule.Host); hostTlsSecret != nil {
					vhostSecrets[rule.Host] = hostTlsSecret
				}

				if i.GetAnnotations()["yggdrasil.uswitch.com/healthcheck-path"] != "" {
					envoyIngress.addHealthCheckPath(i.GetAnnotations()["yggdrasil.uswitch.com/healthcheck-path"])
				}

				if i.GetAnnotations()["yggdrasil.uswitch.com/timeout"] != "" {
					timeout, err := time.ParseDuration(i.GetAnnotations()["yggdrasil.uswitch.com/timeout"])
					if err == nil {
						envoyIngress.addTimeout(timeout)
					}
				}
			}
		}
	}

	for _, ingress := range envoyIngresses {
		ingress.injectDynamicTls(vhostSecrets, secrets)
		cfg.Clusters = append(cfg.Clusters, ingress.cluster)
		cfg.VirtualHosts = append(cfg.VirtualHosts, ingress.vhost)
	}

	numVhosts.Set(float64(len(cfg.VirtualHosts)))
	numClusters.Set(float64(len(cfg.Clusters)))

	return cfg
}
