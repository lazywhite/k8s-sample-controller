package main

import (
	"flag"
	"github.com/lazywhite/k8s-sample-controller/pkg/controller"
	"github.com/lazywhite/k8s-sample-controller/pkg/signals"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"time"
)

var (
	masterUrl  string
	kubeconfig string
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	stopCh := signals.SetupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(masterUrl, kubeconfig)
	if err != nil {
		klog.Fatalf("error building kubeconfig: %s", err.Error())
	}
	kubeClient := kubernetes.NewForConfigOrDie(cfg)
	if err != nil {
		klog.Fatalf("error building kuberentes clientset: %s", err.Error())
	}
	kubeInformerFactory := informers.NewSharedInformerFactory(kubeClient, time.Second*30)
	//kubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(kubeClient, time.Second*30, informers.WithNamespace("default"))
	ctrl := controller.NewController(kubeClient, kubeInformerFactory.Apps().V1().Deployments())

	/*
		1. SharedIndexerInformer.Start() will trigger eventFunc
		2. eventFunc will put item in workqueue
		3. ctrl.Run() will use worker to process items
	*/

	kubeInformerFactory.Start(stopCh)
	if err = ctrl.Run(10, stopCh); err != nil {
		klog.Fatalf("error running controller: %s", err.Error())
	}
}

func init() {
	flag.StringVar(&masterUrl, "master", "", "kube-api-server address, override value in kubeconfig")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig file")
}
