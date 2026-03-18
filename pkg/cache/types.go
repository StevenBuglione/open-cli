package cache

import (
	"net/http"
	"time"

	"github.com/StevenBuglione/open-cli/pkg/obs"
)

type Outcome string

const (
	OutcomeMiss           Outcome = "miss"
	OutcomeFreshHit       Outcome = "fresh_hit"
	OutcomeRevalidatedHit Outcome = "revalidated_hit"
	OutcomeRefreshed      Outcome = "refreshed"
	OutcomeStaleHit       Outcome = "stale_hit"
)

type Policy struct {
	MaxAge            time.Duration
	ManualOnly        bool
	AllowStaleOnError bool
	ForceRefresh      bool
}

type Metadata struct {
	Key             string      `json:"key"`
	URL             string      `json:"url"`
	Method          string      `json:"method"`
	Headers         http.Header `json:"headers"`
	ETag            string      `json:"etag,omitempty"`
	LastModified    string      `json:"lastModified,omitempty"`
	CacheControl    string      `json:"cacheControl,omitempty"`
	CachedAt        time.Time   `json:"cachedAt"`
	ExpiresAt       time.Time   `json:"expiresAt,omitempty"`
	LastValidatedAt time.Time   `json:"lastValidatedAt,omitempty"`
	StatusCode      int         `json:"statusCode"`
	Stale           bool        `json:"stale,omitempty"`
}

type Result struct {
	Body     []byte   `json:"-"`
	Metadata Metadata `json:"metadata"`
	Outcome  Outcome  `json:"outcome"`
}

type FetchOptions struct {
	Key    string
	Policy Policy
}

type FetcherOptions struct {
	Store    *FileStore
	Client   *http.Client
	Now      func() time.Time
	Observer obs.Observer
}
