package controller

import (
	"context"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"time"
)

const (
	controllerName = "k8s-sample-controller"
)

type Controller struct {
	kubeclientset    kubernetes.Interface
	deploymentLister appslisters.DeploymentLister //list or get, can not update
	deploymentSynced cache.InformerSynced
	workqueue        workqueue.RateLimitingInterface
	recorder         record.EventRecorder
}

func (c *Controller) handleObj(obj interface{}) {
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); ok {
		name := object.GetName()
		namespace := object.GetNamespace()
		fmt.Printf("deployment: %s/%s", namespace, name)
		deployment, err := c.deploymentLister.Deployments(namespace).Get(name)
		//object deleted
		if err != nil {
			klog.Errorf("ignore orphaned deployment %s/%s", namespace, name)
			return
		}
		c.enqueueDeployment(deployment)
	} else {
		klog.Error("error decoding object, invalid type")
	}
}

func (c *Controller) handleDeploymentUpdate(old, new interface{}) {
	newDep := new.(*appsv1.Deployment)
	oldDep := old.(*appsv1.Deployment)
	if newDep.ResourceVersion == oldDep.ResourceVersion {
		klog.Info("deployment have same resource version, skip update")
		return
	}
	c.handleObj(new)
}

func (c *Controller) Run(concurrent int, stopCh <-chan struct{}) error {
	/*
	1. sync cache
	2. use gorouting to process workqueue items
	 */
	defer c.workqueue.ShutDown()
	klog.Info("starting deployment controller")
	klog.Info("waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.deploymentSynced); !ok {
		klog.Error("failed to wait for cache to sync")
	}
	klog.Info("starting worker")
	for i := 0; i < concurrent; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.Info("started worker")
	<-stopCh
	klog.Info("shutting down worker")
	return nil
}

func (c *Controller) processNextItem() bool {
	//if no item in queue, get will block, if shutdown=True, caller should end their gorouting
	obj, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}
	//wrap this block, so we can defer workqueue.Done()
	err := func(obj interface{}) error {
		//let workqueue know we have finished to process this item
		//get executed after this wrapper function return
		defer c.workqueue.Done(obj)
		defer func() { fmt.Println(".................") }()
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.workqueue.Forget(obj)
			klog.Errorf("expected string in workqueue but got %#v", obj)
			return nil
		}
		if err := c.syncHandler(key); err != nil {
			//put item back to workqueue
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.workqueue.Forget(obj)
		klog.Infof("successfully synced '%s'", key)
		return nil
	}(obj)
	if err != nil {
		klog.Errorf("failed to process item: %s", err.Error())
	}
	fmt.Println("inside processNextItem")
	return true
}

func (c *Controller) syncHandler(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.Errorf("failed to get namespace and name of '%s': %s", key, err.Error())
	}
	deploy, err := c.deploymentLister.Deployments(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("%s not found, maybe deleted", key)
			return nil
		} else {
			return err
		}
	}
	//do reconcile
	//if replicas != 2, change it to 2 and update status
	replicas := *deploy.Spec.Replicas
	fmt.Printf("%s/%s have replicas: '%d'\n", namespace, name, replicas)

	var fixNum int32 = 2
	if replicas != fixNum {
		rep := deploy.DeepCopy()
		rep.Spec.Replicas = &fixNum
		_, _ = c.kubeclientset.AppsV1().Deployments(namespace).Update(context.TODO(), rep, metav1.UpdateOptions{})
	}
	c.recorder.Event(deploy, corev1.EventTypeNormal, "success reason", "success message")
	return nil
}
func (c *Controller) runWorker() {
	for c.processNextItem() {
	}
}

func (c *Controller) enqueueDeployment(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		klog.Errorf("failed to get object namespace/name: %s", err.Error())
		return
	}
	c.workqueue.Add(key)
}

func NewController(kubeclientset kubernetes.Interface, deploymentInformer appsinformers.DeploymentInformer) *Controller {
	//crd addToScheme()
	klog.Info("creating event broadcaster")

	eventBroadCaster := record.NewBroadcaster()
	eventBroadCaster.StartStructuredLogging(0)
	eventBroadCaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadCaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerName})

	controller := &Controller{
		kubeclientset:    kubeclientset,
		deploymentLister: deploymentInformer.Lister(),
		deploymentSynced: deploymentInformer.Informer().HasSynced,
		workqueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "deployments"),
		recorder:         recorder,
	}

	klog.Info("setting up event handlers")
	deploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.handleObj,
		UpdateFunc: controller.handleDeploymentUpdate,
		DeleteFunc: controller.handleObj,
	})
	return controller
}
