package azure

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/storage/mgmt/2021-04-01/storage"
	"github.com/Azure/go-autorest/autorest/mocks"
)

func Test_cachedKey_get(t *testing.T) {
	for _, tt := range []struct {
		name          string
		key           *cachedKey
		resourceGroup string
		account       string
		err           string
		responses     []string
		expectedKey   string
	}{
		{
			name: "empty resource group name",
			key:  &cachedKey{},
			err:  "parameter=resourceGroupName constraint=MinLength",
		},
		{
			name:          "empty account name",
			key:           &cachedKey{},
			resourceGroup: "resource_group",
			err:           "parameter=accountName constraint=MinLength",
		},
		{
			name:          "cache miss",
			key:           &cachedKey{},
			resourceGroup: "resource_group",
			account:       "account",
			responses:     []string{`{"keys":[{"value":"firstKey"}]}`},
			expectedKey:   "firstKey",
		},
		{
			name: "cache hit",
			key: &cachedKey{
				resourceGroup: "resource_group",
				account:       "account",
				value:         "cachedkey",
				expire:        time.Now().Add(time.Minute),
			},
			resourceGroup: "resource_group",
			account:       "account",
			responses:     []string{`{"keys":[{"value":"firstKey"}]}`},
			expectedKey:   "cachedkey",
		},
		{
			name: "cache expired",
			key: &cachedKey{
				resourceGroup: "resource_group",
				account:       "account",
				value:         "cachedkey",
				expire:        time.Now().Add(-time.Minute),
			},
			resourceGroup: "resource_group",
			account:       "account",
			responses:     []string{`{"keys":[{"value":"apikey"}]}`},
			expectedKey:   "apikey",
		},
		{
			name: "different account",
			key: &cachedKey{
				resourceGroup: "resource_group",
				account:       "account",
				value:         "cachedkey",
				expire:        time.Now().Add(time.Minute),
			},
			resourceGroup: "resource_group",
			account:       "another-account",
			responses:     []string{`{"keys":[{"value":"another-api-key"}]}`},
			expectedKey:   "another-api-key",
		},
		{
			name: "different resource group",
			key: &cachedKey{
				resourceGroup: "resource_group",
				account:       "account",
				value:         "cachedkey",
				expire:        time.Now().Add(time.Minute),
			},
			resourceGroup: "another-resource_group",
			account:       "account",
			responses:     []string{`{"keys":[{"value":"another-api-key"}]}`},
			expectedKey:   "another-api-key",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cli := storage.NewAccountsClient("subscription_id")
			sender := mocks.NewSender()
			for _, resp := range tt.responses {
				sender.AppendResponse(mocks.NewResponseWithContent(resp))
			}
			cli.Sender = sender

			key, err := tt.key.get(
				context.Background(),
				cli,
				tt.resourceGroup,
				tt.account,
			)
			if err != nil {
				if len(tt.err) == 0 {
					t.Errorf("unexpected error: %v", err)
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Errorf(
						"expected error to be %q, %v received instead",
						tt.err,
						err,
					)
				}
			} else if len(tt.err) > 0 {
				t.Errorf("expected error %q, nil received instead", tt.err)
			}

			if key != tt.expectedKey {
				t.Errorf("expected key %q, %q received", tt.expectedKey, key)
			}
		})
	}
}
