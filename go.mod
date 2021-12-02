module github.com/FoundationDB/fdb-kubernetes-operator

go 1.16

require (
	github.com/apple/foundationdb/bindings/go v0.0.0-20190724023245-90ba203c166c
	github.com/fatih/color v1.10.0
	github.com/go-logr/logr v0.4.0
	github.com/google/go-cmp v0.5.5
	github.com/hashicorp/go-retryablehttp v0.6.8
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.14.0
	github.com/prometheus/client_golang v1.11.0
	github.com/prometheus/common v0.26.0
	github.com/spf13/cobra v1.1.1
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.7.0
	golang.org/x/net v0.0.0-20210428140749-89ef3d95e781
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
	k8s.io/api v0.21.3
	k8s.io/apimachinery v0.21.3
	k8s.io/cli-runtime v0.21.3
	k8s.io/client-go v0.21.3
	k8s.io/klog/v2 v2.8.0
	k8s.io/utils v0.0.0-20210820185131-d34e5cb4466e
	sigs.k8s.io/controller-runtime v0.9.6
	sigs.k8s.io/yaml v1.2.0
)
