module github.com/openshift/cluster-image-registry-operator

go 1.16

require (
	cloud.google.com/go/storage v1.10.0
	github.com/Azure/azure-pipeline-go v0.2.3
	github.com/Azure/azure-sdk-for-go v55.6.0+incompatible
	github.com/Azure/azure-storage-blob-go v0.7.0
	github.com/Azure/go-autorest/autorest v0.11.18
	github.com/Azure/go-autorest/autorest/azure/auth v0.5.7
	github.com/Azure/go-autorest/autorest/mocks v0.4.1
	github.com/Azure/go-autorest/autorest/to v0.4.0
	github.com/Azure/go-autorest/autorest/validation v0.2.0 // indirect
	github.com/IBM/go-sdk-core/v5 v5.5.0
	github.com/IBM/ibm-cos-sdk-go v1.7.0
	github.com/IBM/platform-services-go-sdk v0.18.15
	github.com/aws/aws-sdk-go v1.38.35
	github.com/davecgh/go-spew v1.1.1
	github.com/emicklei/go-restful v2.11.1+incompatible // indirect
	github.com/ghodss/yaml v1.0.0
	github.com/golang-jwt/jwt v3.2.2+incompatible
	github.com/google/go-cmp v0.5.5
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.2.0
	github.com/gophercloud/gophercloud v0.17.0
	github.com/gophercloud/utils v0.0.0-20210323225332-7b186010c04f
	github.com/goware/urlx v0.3.1
	github.com/imdario/mergo v0.3.8 // indirect
	github.com/openshift/api v0.0.0-20211108165917-be1be0e89115
	github.com/openshift/build-machinery-go v0.0.0-20211213093930-7e33a7eb4ce3
	github.com/openshift/client-go v0.0.0-20210916133943-9acee1a0fb83
	github.com/openshift/installer v0.9.0-master.0.20190726121806-6e8f9c335410
	github.com/openshift/library-go v0.0.0-20211110085240-047b536a17c6
	github.com/prometheus/client_golang v1.11.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.26.0
	github.com/spf13/cobra v1.1.3
	golang.org/x/crypto v0.0.0-20210506145944-38f3c27a63bf // indirect
	golang.org/x/net v0.0.0-20210520170846-37e1c6afe023
	golang.org/x/oauth2 v0.0.0-20210622215436-a8dc77f794b6
	google.golang.org/api v0.30.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.22.1
	k8s.io/apimachinery v0.22.1
	k8s.io/client-go v0.22.1
	k8s.io/klog/v2 v2.9.0
	k8s.io/utils v0.0.0-20210707171843-4b05e18ac7d9
)

replace (
	github.com/openshift/api => github.com/josefkarasek/api-1 v0.0.0-20211122143617-216bd2eae3e2
)
