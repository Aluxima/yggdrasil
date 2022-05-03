package k8s

import (
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

func NewListWatch(client *kubernetes.Clientset) *cache.ListWatch {
	return cache.NewListWatchFromClient(client.ExtensionsV1beta1().RESTClient(), "ingresses", "", fields.Everything())
}
func NewSecretsListWatch(client *kubernetes.Clientset) *cache.ListWatch {
	return cache.NewListWatchFromClient(client.CoreV1().RESTClient(), "secrets", "", fields.OneTermEqualSelector("type", "kubernetes.io/tls"))
}
