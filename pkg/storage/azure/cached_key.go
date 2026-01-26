package azure

import (
	"context"
	"sync"
	"time"

	"github.com/openshift/cluster-image-registry-operator/pkg/metrics"
)

// cacheExpiration is the cache expiration duration in minutes.
const cacheExpiration time.Duration = 20 * time.Minute

// primaryKey keeps account primary key in a cache.
var primaryKey cachedKey

// KeyFetcher abstracts storage account key retrieval.
type KeyFetcher interface {
	GetPrimaryStorageAccountKey(ctx context.Context, resourceGroup, account string) (string, error)
}

// cachedKey holds an API access key in memory for five minutes.
type cachedKey struct {
	mtx           sync.Mutex
	resourceGroup string
	account       string
	value         string
	expire        time.Time
}

// get returns the cached key if it is not expired yet, if expired fetches the key
// remotely using provided KeyFetcher.
func (k *cachedKey) get(
	ctx context.Context, fetcher KeyFetcher, resourceGroup, account string,
) (string, error) {
	k.mtx.Lock()
	defer k.mtx.Unlock()

	if k.resourceGroup == resourceGroup && k.account == account && time.Now().Before(k.expire) {
		metrics.AzureKeyCacheHit()
		return k.value, nil
	}
	metrics.AzureKeyCacheMiss()

	key, err := fetcher.GetPrimaryStorageAccountKey(ctx, resourceGroup, account)
	if err != nil {
		return "", err
	}

	k.resourceGroup = resourceGroup
	k.account = account
	k.value = key
	k.expire = time.Now().Add(cacheExpiration)
	return k.value, nil
}
