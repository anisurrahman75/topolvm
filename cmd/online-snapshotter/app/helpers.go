package app

import (
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	restclient "k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

func NewRuntimeClient(config *restclient.Config) (client.Client, error) {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(topolvmv1.AddToScheme(scheme))

	hc, err := restclient.HTTPClientFor(config)
	if err != nil {
		return nil, err
	}
	mapper, err := apiutil.NewDynamicRESTMapper(config, hc)
	if err != nil {
		return nil, err
	}

	return client.New(config, client.Options{
		Scheme: scheme,
		Mapper: mapper,
	})
}
