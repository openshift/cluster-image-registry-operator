package azure

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// mockKeyFetcher implements KeyFetcher for testing
type mockKeyFetcher struct {
	key string
	err error
}

func (m *mockKeyFetcher) GetPrimaryStorageAccountKey(ctx context.Context, resourceGroup, account string) (string, error) {
	if resourceGroup == "" {
		return "", fmt.Errorf("validation failed: parameter=resourceGroupName constraint=MinLength")
	}
	if account == "" {
		return "", fmt.Errorf("validation failed: parameter=accountName constraint=MinLength")
	}
	if m.err != nil {
		return "", m.err
	}
	return m.key, nil
}

func Test_cachedKey_get(t *testing.T) {
	for _, tt := range []struct {
		name          string
		key           *cachedKey
		resourceGroup string
		account       string
		err           string
		mockKey       string
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
			mockKey:       "firstKey",
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
			mockKey:       "firstKey",
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
			mockKey:       "apikey",
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
			mockKey:       "another-api-key",
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
			mockKey:       "another-api-key",
			expectedKey:   "another-api-key",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fetcher := &mockKeyFetcher{key: tt.mockKey}

			key, err := tt.key.get(
				context.Background(),
				fetcher,
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
