package testframework

import (
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func ConfigFromFile(filename string) (*rest.Config, error) {
	config, err := clientcmd.LoadFromFile(filename)
	if err != nil {
		return nil, err
	}

	return clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{}).ClientConfig()
}

func UserClientConfig(clientConfig *rest.Config, token string) *rest.Config {
	userClientConfig := rest.AnonymousClientConfig(clientConfig)
	userClientConfig.BearerToken = token
	return userClientConfig
}
