package utils

import (
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

type informerManager struct {
	mutex     sync.Mutex
	factories map[string]dynamicinformer.DynamicSharedInformerFactory
}

var informerMgr *informerManager

func init() {
	informerMgr = &informerManager{
		factories: make(map[string]dynamicinformer.DynamicSharedInformerFactory),
	}
}

func GetInformer(client dynamic.Interface, namespace string, gvr schema.GroupVersionResource) cache.SharedInformer {
	informerMgr.mutex.Lock()
	defer informerMgr.mutex.Unlock()

	factory, ok := informerMgr.factories[namespace]
	if !ok {
		factory = dynamicinformer.NewFilteredDynamicSharedInformerFactory(client, 0, namespace, nil)
		informerMgr.factories[namespace] = factory
	}

	return factory.ForResource(gvr).Informer()
}
