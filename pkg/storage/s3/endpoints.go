package s3

import (
	"github.com/aws/aws-sdk-go/aws/endpoints"
	configv1 "github.com/openshift/api/config/v1"
)

type endpointsResolver struct {
	region           string
	serviceEndpoints map[string]string
}

func newEndpointsResolver(region, s3Endpoint string, endpoints []configv1.AWSServiceEndpoint) *endpointsResolver {
	serviceEndpoints := make(map[string]string)
	for _, ep := range endpoints {
		serviceEndpoints[ep.Name] = ep.URL
	}

	if s3Endpoint != "" {
		serviceEndpoints["s3"] = s3Endpoint
	}

	return &endpointsResolver{
		region:           region,
		serviceEndpoints: serviceEndpoints,
	}
}

func (er *endpointsResolver) EndpointFor(service, region string, opts ...func(*endpoints.Options)) (endpoints.ResolvedEndpoint, error) {
	if ep, ok := er.serviceEndpoints[service]; ok {
		// The signing region and the cluster region may be different, see
		// https://github.com/openshift/installer/commit/41a15b787b5f6d0b0766e1737dcdfeb5b23020d5
		signingRegion := er.region
		def, _ := endpoints.DefaultResolver().EndpointFor(service, region)
		if len(def.SigningRegion) > 0 {
			signingRegion = def.SigningRegion
		}
		return endpoints.ResolvedEndpoint{
			URL:           ep,
			SigningRegion: signingRegion,
		}, nil
	}
	return endpoints.DefaultResolver().EndpointFor(service, region, opts...)
}

func isUnknownEndpointError(err error) bool {
	_, ok := err.(endpoints.UnknownEndpointError)
	return ok
}

func regionHasDualStackS3(region string) (bool, error) {
	_, err := endpoints.DefaultResolver().EndpointFor("s3", region, endpoints.UseDualStackEndpointOption)
	if isUnknownEndpointError(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
