package discovery

import (
	"time"

	"github.com/StevenBuglione/open-cli/pkg/cache"
)

func fetchRecordFromCache(url string, method ProvenanceMethod, result *cache.Result) FetchRecord {
	record := FetchRecord{
		URL:       url,
		FetchedAt: fetchedAt(result.Metadata),
		Method:    method,
	}
	if result == nil {
		return record
	}
	record.RequestMethod = result.Metadata.Method
	record.StatusCode = result.Metadata.StatusCode
	record.CacheOutcome = string(result.Outcome)
	record.ETag = result.Metadata.ETag
	record.LastModified = result.Metadata.LastModified
	record.CacheControl = result.Metadata.CacheControl
	record.Stale = result.Metadata.Stale
	if !result.Metadata.ExpiresAt.IsZero() {
		expiresAt := result.Metadata.ExpiresAt
		record.ExpiresAt = &expiresAt
	}
	return record
}

func fetchedAt(metadata cache.Metadata) time.Time {
	if !metadata.LastValidatedAt.IsZero() {
		return metadata.LastValidatedAt
	}
	return metadata.CachedAt
}
