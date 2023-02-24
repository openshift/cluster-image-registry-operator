package ibmcos

import (
	"fmt"
)

// powerVSRegion
// Power VS is a cloud offering of Power Systems in IBM Cloud and COS provides object storage service in IBM Cloud.
// powerVSRegion describes respective IBM Cloud COS region associated with a region in Power VS.
// It helps to decide COS region from Power VS region with the intention to keep COS in same region as Power VS.
// This is required since region notation followed by Power VS is different from COS.
type powerVSRegion struct {
	description string
	cosRegion   string
}

// powerVSRegions provides a mapping between Power VS and COS region.
var powerVSRegions = map[string]powerVSRegion{
	"dal": {
		description: "Dallas, USA",
		cosRegion:   "us-south",
	},
	"eu-de": {
		description: "Frankfurt, Germany",
		cosRegion:   "eu-de",
	},
	"lon": {
		description: "London, UK.",
		cosRegion:   "eu-gb",
	},
	"mon": {
		description: "Montreal, Canada",
		cosRegion:   "ca-tor",
	},
	"osa": {
		description: "Osaka, Japan",
		cosRegion:   "jp-osa",
	},
	"syd": {
		description: "Sydney, Australia",
		cosRegion:   "au-syd",
	},
	"sao": {
		description: "São Paulo, Brazil",
		cosRegion:   "br-sao",
	},
	"tok": {
		description: "Tokyo, Japan",
		cosRegion:   "jp-tok",
	},
	"us-east": {
		description: "Washington DC, USA",
		cosRegion:   "us-east",
	},
}

// cosRegionForPowerVSRegion returns the COS region for the specified Power VS region.
func cosRegionForPowerVSRegion(region string) (string, error) {
	if r, ok := powerVSRegions[region]; ok {
		return r.cosRegion, nil
	}

	return "", fmt.Errorf("cos region corresponding to a powervs region %s not found ", region)
}
