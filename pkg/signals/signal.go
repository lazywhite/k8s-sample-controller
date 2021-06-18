package signals

import (
	"os"
	"os/signal"
)

var (
	//shutdownSignals = []os.Signal{os.Interrupt, syscall.SigTerm}
	shutdownSignals = []os.Signal{os.Interrupt}

	onlyoneSignalHandler = make(chan struct{})
)

func SetupSignalHandler() (stopCh <-chan struct{}) {
	close(onlyoneSignalHandler) // panic if call twice

	stop := make(chan struct{})
	c := make(chan os.Signal, 2)
	signal.Notify(c, shutdownSignals...)
	go func() {
		<-c
		close(stop)
		<-c
		os.Exit(1)
	}()
	return stop
}
