package tls

import (
	"crypto/x509"
	"crypto/x509/pkix"

	"github.com/openshift/installer/pkg/asset"
)

// EtcdCA is the asset that generates the etcd-ca key/cert pair.
// [DEPRECATED]
type EtcdCA struct {
	SelfSignedCertKey
}

var _ asset.Asset = (*EtcdCA)(nil)

// Dependencies returns the dependency of the the cert/key pair, which includes
// the parent CA, and install config if it depends on the install config for
// DNS names, etc.
func (a *EtcdCA) Dependencies() []asset.Asset {
	return []asset.Asset{}
}

// Generate generates the cert/key pair based on its dependencies.
func (a *EtcdCA) Generate(dependencies asset.Parents) error {
	cfg := &CertCfg{
		Subject:   pkix.Name{CommonName: "etcd", OrganizationalUnit: []string{"etcd"}},
		KeyUsages: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		Validity:  ValidityTenYears,
		IsCA:      true,
	}

	return a.SelfSignedCertKey.Generate(cfg, "etcd-client-ca")
}

// Name returns the human-friendly name of the asset.
func (a *EtcdCA) Name() string {
	return "Certificate (etcd)"
}

// EtcdClientCertKey is the asset that generates the etcd client key/cert pair.
// [DEPRECATED]
type EtcdClientCertKey struct {
	SignedCertKey
}

var _ asset.Asset = (*EtcdClientCertKey)(nil)

// Dependencies returns the dependency of the the cert/key pair, which includes
// the parent CA, and install config if it depends on the install config for
// DNS names, etc.
func (a *EtcdClientCertKey) Dependencies() []asset.Asset {
	return []asset.Asset{
		&EtcdCA{},
	}
}

// Generate generates the cert/key pair based on its dependencies.
func (a *EtcdClientCertKey) Generate(dependencies asset.Parents) error {
	etcdCA := &EtcdCA{}
	dependencies.Get(etcdCA)

	cfg := &CertCfg{
		Subject:      pkix.Name{CommonName: "etcd", OrganizationalUnit: []string{"etcd"}},
		KeyUsages:    x509.KeyUsageKeyEncipherment,
		ExtKeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		Validity:     ValidityTenYears,
	}

	return a.SignedCertKey.Generate(cfg, etcdCA, "etcd-client", DoNotAppendParent)
}

// Name returns the human-friendly name of the asset.
func (a *EtcdClientCertKey) Name() string {
	return "Certificate (etcd)"
}

// EtcdSignerCertKey is a key/cert pair that signs the etcd client and peer certs.
type EtcdSignerCertKey struct {
	SelfSignedCertKey
}

var _ asset.WritableAsset = (*EtcdSignerCertKey)(nil)

// Dependencies returns the dependency of the root-ca, which is empty.
func (c *EtcdSignerCertKey) Dependencies() []asset.Asset {
	return []asset.Asset{}
}

// Generate generates the root-ca key and cert pair.
func (c *EtcdSignerCertKey) Generate(parents asset.Parents) error {
	cfg := &CertCfg{
		Subject:   pkix.Name{CommonName: "etcd-signer", OrganizationalUnit: []string{"openshift"}},
		KeyUsages: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		Validity:  ValidityTenYears,
		IsCA:      true,
	}

	return c.SelfSignedCertKey.Generate(cfg, "etcd-signer")
}

// Name returns the human-friendly name of the asset.
func (c *EtcdSignerCertKey) Name() string {
	return "Certificate (etcd-signer)"
}

// EtcdCABundle is the asset the generates the etcd-ca-bundle,
// which contains all the individual client CAs.
type EtcdCABundle struct {
	CertBundle
}

var _ asset.Asset = (*EtcdCABundle)(nil)

// Dependencies returns the dependency of the cert bundle.
func (a *EtcdCABundle) Dependencies() []asset.Asset {
	return []asset.Asset{
		&EtcdSignerCertKey{},
	}
}

// Generate generates the cert bundle based on its dependencies.
func (a *EtcdCABundle) Generate(deps asset.Parents) error {
	var certs []CertInterface
	for _, asset := range a.Dependencies() {
		deps.Get(asset)
		certs = append(certs, asset.(CertInterface))
	}
	return a.CertBundle.Generate("etcd-ca-bundle", certs...)
}

// Name returns the human-friendly name of the asset.
func (a *EtcdCABundle) Name() string {
	return "Certificate (etcd-ca-bundle)"
}

// EtcdSignerClientCertKey is the asset that generates the etcd client key/cert pair.
type EtcdSignerClientCertKey struct {
	SignedCertKey
}

var _ asset.Asset = (*EtcdSignerClientCertKey)(nil)

// Dependencies returns the dependency of the the cert/key pair, which includes
// the parent CA, and install config if it depends on the install config for
// DNS names, etc.
func (a *EtcdSignerClientCertKey) Dependencies() []asset.Asset {
	return []asset.Asset{
		&EtcdSignerCertKey{},
	}
}

// Generate generates the cert/key pair based on its dependencies.
func (a *EtcdSignerClientCertKey) Generate(dependencies asset.Parents) error {
	ca := &EtcdSignerCertKey{}
	dependencies.Get(ca)

	cfg := &CertCfg{
		Subject:      pkix.Name{CommonName: "etcd", OrganizationalUnit: []string{"etcd"}},
		KeyUsages:    x509.KeyUsageKeyEncipherment,
		ExtKeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		Validity:     ValidityTenYears,
	}

	return a.SignedCertKey.Generate(cfg, ca, "etcd-signer-client", DoNotAppendParent)
}

// Name returns the human-friendly name of the asset.
func (a *EtcdSignerClientCertKey) Name() string {
	return "Certificate (etcd-signer-client)"
}
