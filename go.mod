module github.com/FoundationDB/fdb-kubernetes-operator

go 1.12

require (
	github.com/apple/foundationdb/bindings/go v0.0.0-20190724023245-90ba203c166c
	github.com/go-logr/logr v0.3.0
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/prometheus/client_golang v1.7.1
	github.com/prometheus/common v0.10.0
	github.com/spf13/cobra v1.1.1
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.7.0
	go.uber.org/zap v1.15.0
	golang.org/x/net v0.0.0-20201110031124-69a78807bb2b
	k8s.io/api v0.20.4
	k8s.io/apimachinery v0.20.4
	k8s.io/cli-runtime v0.20.4
	k8s.io/client-go v0.20.4
	sigs.k8s.io/controller-runtime v0.8.2
	sigs.k8s.io/yaml v1.2.0
)
