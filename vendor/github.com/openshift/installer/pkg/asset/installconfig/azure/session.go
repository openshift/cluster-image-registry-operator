package azure

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/Azure/go-autorest/autorest"
	azureenv "github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/AlecAivazis/survey.v1"
)

const azureAuthEnv = "AZURE_AUTH_LOCATION"

var authFilePath = os.Getenv("HOME") + "/.azure/osServicePrincipal.json"

//Session is an object representing session for subscription
type Session struct {
	Authorizer  autorest.Authorizer
	Credentials Credentials
}

//Credentials is the data type for credentials as undestood by the azure sdk
type Credentials struct {
	SubscriptionID string `json:"subscriptionId,omitempty"`
	ClientID       string `json:"clientId,omitempty"`
	ClientSecret   string `json:"clientSecret,omitempty"`
	TenantID       string `json:"tenantId,omitempty"`
}

// GetSession returns an azure session by using credentials found in ~/.azure/osServicePrincipal.json
// and, if no creds are found, asks for them and stores them on disk in a config file
func GetSession() (*Session, error) {
	os.Setenv(azureAuthEnv, authFilePath)
	return newSessionFromFile()
}

func newSessionFromFile() (*Session, error) {
	authorizer, err := auth.NewAuthorizerFromFileWithResource(azureenv.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
		logrus.Debug("could not get an azure authorizer from file. Asking user to provide authentication info")
		credentials, err := askForCredentials()
		if err != nil {
			return nil, errors.Wrap(err, "failed to retrieve credentials from user")
		}
		if err = saveCredentials(*credentials, authFilePath); err != nil {
			return nil, errors.Wrap(err, "failed to save credentials")
		}
	}

	//If the authorizer worked right away, we need to read credentials details
	authSettings, err := auth.GetSettingsFromFile()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get settings from file")
	}

	credentials, err := getCredentials(authSettings)
	if err != nil {
		return nil, errors.Wrap(err, "failed to map authsettings to credentials")
	}

	authorizer, err = authSettings.ClientCredentialsAuthorizerWithResource(azureenv.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get client credentials authorizer from saved azure auth settings")
	}

	return &Session{
		Authorizer:  authorizer,
		Credentials: *credentials,
	}, nil
}

func getCredentials(fs auth.FileSettings) (*Credentials, error) {
	subscriptionID := fs.GetSubscriptionID()
	if subscriptionID == "" {
		return nil, errors.New("could not retrieve subscriptionId from auth file")
	}

	clientID := fs.Values[auth.ClientID]
	if clientID == "" {
		return nil, errors.New("could not retrieve clientId from auth file")
	}
	clientSecret := fs.Values[auth.ClientSecret]
	if clientSecret == "" {
		return nil, errors.New("could not retrieve clientSecret from auth file")
	}
	tenantID := fs.Values[auth.TenantID]
	if tenantID == "" {
		return nil, errors.New("could not retrieve tenantId from auth file")
	}
	return &Credentials{
		SubscriptionID: subscriptionID,
		ClientID:       clientID,
		ClientSecret:   clientSecret,
		TenantID:       tenantID,
	}, nil
}

func askForCredentials() (*Credentials, error) {
	var subscriptionID, tenantID, clientID, clientSecret string

	err := survey.Ask([]*survey.Question{
		{
			Prompt: &survey.Input{
				Message: "azure subscription id",
				Help:    "The azure subscription id to use for installation",
			},
		},
	}, &subscriptionID)
	if err != nil {
		return nil, err
	}

	err = survey.Ask([]*survey.Question{
		{
			Prompt: &survey.Input{
				Message: "azure tenant id",
				Help:    "The azure tenant id to use for installation",
			},
		},
	}, &tenantID)
	if err != nil {
		return nil, err
	}

	err = survey.Ask([]*survey.Question{
		{
			Prompt: &survey.Input{
				Message: "azure service principal client id",
				Help:    "The azure client id to use for installation (this is not your username)",
			},
		},
	}, &clientID)
	if err != nil {
		return nil, err
	}

	err = survey.Ask([]*survey.Question{
		{
			Prompt: &survey.Password{
				Message: "azure service principal client secret",
				Help:    "The azure secret access key corresponding to your client secret (this is not your password).",
			},
		},
	}, &clientSecret)
	if err != nil {
		return nil, err
	}

	return &Credentials{
		SubscriptionID: subscriptionID,
		ClientID:       clientID,
		ClientSecret:   clientSecret,
		TenantID:       tenantID,
	}, nil
}

func saveCredentials(credentials Credentials, filePath string) error {
	jsonCreds, err := json.Marshal(credentials)
	err = os.MkdirAll(filepath.Dir(filePath), 0700)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(filePath, jsonCreds, 0600)
}
