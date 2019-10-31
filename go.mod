module github.com/openshift/cluster-image-registry-operator

go 1.13

require (
	cloud.google.com/go v0.40.0
	github.com/Azure/azure-pipeline-go v0.2.2 // indirect
	github.com/Azure/azure-sdk-for-go v30.1.0+incompatible
	github.com/Azure/azure-storage-blob-go v0.7.0
	github.com/Azure/go-autorest/autorest v0.9.2
	github.com/Azure/go-autorest/autorest/adal v0.8.0 // indirect
	github.com/Azure/go-autorest/autorest/azure/auth v0.4.0
	github.com/Azure/go-autorest/autorest/to v0.3.0
	github.com/Azure/go-autorest/autorest/validation v0.2.0 // indirect
	github.com/NYTimes/gziphandler v1.1.1 // indirect
	github.com/aws/aws-sdk-go v1.21.10
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver v3.5.1+incompatible // indirect
	github.com/coreos/bbolt v1.3.3 // indirect
	github.com/coreos/etcd v3.3.17+incompatible // indirect
	github.com/coreos/go-systemd v0.0.0-20190719114852-fd7a80b32e1f // indirect
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f // indirect
	github.com/davecgh/go-spew v1.1.1
	github.com/emicklei/go-restful v2.11.1+incompatible // indirect
	github.com/evanphx/json-patch v4.5.0+incompatible // indirect
	github.com/ghodss/yaml v1.0.0
	github.com/go-openapi/jsonreference v0.19.3 // indirect
	github.com/go-openapi/spec v0.19.4 // indirect
	github.com/gogo/protobuf v1.3.1 // indirect
	github.com/golang/groupcache v0.0.0-20191027212112-611e8accdfc9 // indirect
	github.com/google/btree v1.0.0 // indirect
	github.com/google/go-cmp v0.3.1 // indirect
	github.com/googleapis/gax-go/v2 v2.0.5 // indirect
	github.com/googleapis/gnostic v0.3.1 // indirect
	github.com/gophercloud/gophercloud v0.2.1-0.20190725225357-73bf16e49026
	github.com/gophercloud/utils v0.0.0-20190527093828-25f1b77b8c03
	github.com/gorilla/websocket v1.4.1 // indirect
	github.com/goware/urlx v0.3.1
	github.com/grpc-ecosystem/go-grpc-middleware v1.1.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.9.5 // indirect
	github.com/hashicorp/golang-lru v0.5.3 // indirect
	github.com/imdario/mergo v0.3.8 // indirect
	github.com/json-iterator/go v1.1.8 // indirect
	github.com/mailru/easyjson v0.7.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/onsi/ginkgo v1.10.1 // indirect
	github.com/onsi/gomega v1.7.0 // indirect
	github.com/openshift/api v3.9.1-0.20191031084152-11eee842dafd+incompatible
	github.com/openshift/client-go v0.0.0-20191022152013-2823239d2298
	github.com/openshift/crd-schema-gen v1.0.0
	github.com/openshift/installer v0.9.0-master.0.20190726121806-6e8f9c335410
	github.com/openshift/library-go v0.0.0-20191030100914-d6f3b526f033
	github.com/prometheus/client_golang v0.9.4
	github.com/prometheus/client_model v0.0.0-20190812154241-14fe0d1b01d4
	github.com/prometheus/common v0.4.1
	github.com/prometheus/procfs v0.0.5 // indirect
	github.com/soheilhy/cmux v0.1.4 // indirect
	github.com/spf13/cobra v0.0.5
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20190109142713-0ad062ec5ee5 // indirect
	github.com/xiang90/probing v0.0.0-20190116061207-43a291ad63a2 // indirect
	go.etcd.io/bbolt v1.3.3 // indirect
	go.uber.org/zap v1.12.0 // indirect
	golang.org/x/crypto v0.0.0-20191029031824-8986dd9e96cf // indirect
	golang.org/x/net v0.0.0-20191028085509-fe3aa8a45271
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45
	golang.org/x/sys v0.0.0-20191029155521-f43be2a4598c // indirect
	golang.org/x/time v0.0.0-20191024005414-555d28b269f0 // indirect
	golang.org/x/tools v0.0.0-20191031144223-d9fd88a569ec // indirect
	gonum.org/v1/gonum v0.6.0 // indirect
	google.golang.org/api v0.7.0
	google.golang.org/appengine v1.6.5 // indirect
	google.golang.org/genproto v0.0.0-20191028173616-919d9bdd9fe6 // indirect
	google.golang.org/grpc v1.24.0 // indirect
	gopkg.in/yaml.v2 v2.2.4
	k8s.io/api v0.0.0-20191016110408-35e52d86657a
	k8s.io/apiextensions-apiserver v0.0.0-20191016113550-5357c4baaf65 // indirect
	k8s.io/apimachinery v0.0.0-20191004115801-a2eda9f80ab8
	k8s.io/client-go v0.0.0-20191016111102-bec269661e48
	k8s.io/code-generator v0.0.0-20191004115455-8e001e5d1894
	k8s.io/gengo v0.0.0-20191010091904-7fa3014cb28f // indirect
	k8s.io/klog v1.0.0
	k8s.io/kube-openapi v0.0.0-20190918143330-0270cf2f1c1d // indirect
	k8s.io/utils v0.0.0-20191030222137-2b95a09bc58d // indirect
	sigs.k8s.io/controller-tools v0.2.1 // indirect
)
