
run:
	go run main.go -kubeconfig=${KUBECONFIG}
build:
	go build -o sample-controller main.go
