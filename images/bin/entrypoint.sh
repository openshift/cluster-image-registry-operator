#!/bin/sh

# When upgrading from 4.1 -> 4.2, the trust bundle may not be immediately present.
# If trust bundle is not present, assume the cluster is not at 4.2 yet
trustedCA="/var/run/configmaps/trusted-ca/tls-ca-bundle.pem"

if [ -e "$trustedCA" ]; then
    echo "Overwriting root TLS certificate authority trust store"
    cp -f "$trustedCA" /etc/pki/ca-trust/extracted/pem/
fi

exec /usr/bin/cluster-image-registry-operator
