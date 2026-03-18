package cache

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/StevenBuglione/open-cli/pkg/obs"
)

type Fetcher struct {
	store    *FileStore
	client   *http.Client
	now      func() time.Time
	observer obs.Observer
}

func NewFetcher(options FetcherOptions) *Fetcher {
	if options.Client == nil {
		options.Client = http.DefaultClient
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Observer == nil {
		options.Observer = obs.NewNop()
	}
	return &Fetcher{
		store:    options.Store,
		client:   options.Client,
		now:      options.Now,
		observer: options.Observer,
	}
}

func (fetcher *Fetcher) Fetch(request *http.Request, options FetchOptions) (*Result, error) {
	if request == nil {
		return nil, fmt.Errorf("request is required")
	}

	start := time.Now()
	ctx, finish := fetcher.observer.StartSpan(request.Context(), "cache.fetch", map[string]string{
		"method": request.Method,
		"url":    request.URL.String(),
	})
	var finishErr error
	defer func() { finish(finishErr) }()

	key := fetcher.cacheKey(request, options)
	var (
		cachedMetadata Metadata
		cachedBody     []byte
		hasCached      bool
		hadCorrupt     bool
	)
	if fetcher.store != nil {
		metadata, body, err := fetcher.store.Load(key)
		switch err {
		case nil:
			cachedMetadata = metadata
			cachedBody = body
			hasCached = true
		case ErrNotFound:
		case ErrCorrupt:
			hadCorrupt = true
		default:
			return nil, err
		}
	}

	now := fetcher.now().UTC()
	if hasCached && options.Policy.ManualOnly && !options.Policy.ForceRefresh {
		result := cachedResult(cachedMetadata, cachedBody, now)
		fetcher.emit(ctx, request, result, time.Since(start), "")
		return result, nil
	}
	if hasCached && !options.Policy.ForceRefresh && isFresh(now, cachedMetadata) {
		result := &Result{
			Body:     cloneBytes(cachedBody),
			Metadata: freshMetadata(cachedMetadata),
			Outcome:  OutcomeFreshHit,
		}
		fetcher.emit(ctx, request, result, time.Since(start), "")
		return result, nil
	}

	networkRequest := request.Clone(request.Context())
	if hasCached {
		if cachedMetadata.ETag != "" {
			networkRequest.Header.Set("If-None-Match", cachedMetadata.ETag)
		}
		if cachedMetadata.LastModified != "" {
			networkRequest.Header.Set("If-Modified-Since", cachedMetadata.LastModified)
		}
	}

	response, err := fetcher.client.Do(networkRequest)
	if err != nil {
		if hasCached && options.Policy.AllowStaleOnError {
			result := staleResult(cachedMetadata, cachedBody)
			fetcher.emit(ctx, request, result, time.Since(start), "")
			return result, nil
		}
		finishErr = err
		fetcher.observer.Emit(ctx, obs.Event{
			Name:          "cache.fetch",
			URL:           request.URL.String(),
			Operation:     request.Method,
			Duration:      time.Since(start),
			ErrorCategory: "transport_error",
		})
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotModified && hasCached {
		updated := cachedMetadata
		updated.Stale = false
		updated.LastValidatedAt = now
		updated.CacheControl = coalesce(response.Header.Get("Cache-Control"), updated.CacheControl)
		updated.ETag = coalesce(response.Header.Get("ETag"), updated.ETag)
		updated.LastModified = coalesce(response.Header.Get("Last-Modified"), updated.LastModified)
		updated.Headers = mergeHeaders(updated.Headers, response.Header)
		updated.ExpiresAt = expiryFrom(now, updated.CacheControl, options.Policy.MaxAge)
		if fetcher.store != nil {
			if err := fetcher.store.Save(key, updated, cachedBody); err != nil {
				finishErr = err
				return nil, err
			}
		}
		result := &Result{
			Body:     cloneBytes(cachedBody),
			Metadata: updated,
			Outcome:  OutcomeRevalidatedHit,
		}
		fetcher.emit(ctx, request, result, time.Since(start), "")
		return result, nil
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		finishErr = err
		return nil, err
	}

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		metadata := Metadata{
			Key:             key,
			URL:             request.URL.String(),
			Method:          request.Method,
			Headers:         cloneHeaders(response.Header),
			ETag:            response.Header.Get("ETag"),
			LastModified:    response.Header.Get("Last-Modified"),
			CacheControl:    response.Header.Get("Cache-Control"),
			CachedAt:        now,
			LastValidatedAt: now,
			ExpiresAt:       expiryFrom(now, response.Header.Get("Cache-Control"), options.Policy.MaxAge),
			StatusCode:      response.StatusCode,
		}
		if fetcher.store != nil {
			if err := fetcher.store.Save(key, metadata, body); err != nil {
				finishErr = err
				return nil, err
			}
		}
		outcome := OutcomeMiss
		if hasCached || hadCorrupt {
			outcome = OutcomeRefreshed
		}
		result := &Result{
			Body:     cloneBytes(body),
			Metadata: metadata,
			Outcome:  outcome,
		}
		fetcher.emit(ctx, request, result, time.Since(start), "")
		return result, nil
	}

	if hasCached && options.Policy.AllowStaleOnError {
		result := staleResult(cachedMetadata, cachedBody)
		fetcher.emit(ctx, request, result, time.Since(start), "")
		return result, nil
	}
	finishErr = fmt.Errorf("unexpected status %d from %s", response.StatusCode, request.URL.String())
	fetcher.observer.Emit(ctx, obs.Event{
		Name:          "cache.fetch",
		URL:           request.URL.String(),
		Operation:     request.Method,
		StatusCode:    response.StatusCode,
		Duration:      time.Since(start),
		ErrorCategory: "http_error",
	})
	return nil, finishErr
}

func (fetcher *Fetcher) cacheKey(request *http.Request, options FetchOptions) string {
	if options.Key != "" {
		return options.Key
	}
	if accept := request.Header.Get("Accept"); accept != "" {
		return request.Method + ":" + request.URL.String() + ":" + accept
	}
	return request.Method + ":" + request.URL.String()
}

func (fetcher *Fetcher) Clear() error {
	if fetcher.store == nil {
		return nil
	}
	return fetcher.store.Clear()
}

func (fetcher *Fetcher) emit(ctx context.Context, request *http.Request, result *Result, duration time.Duration, requestID string) {
	if result == nil {
		return
	}
	if requestID == "" {
		requestID = obs.RequestIDFromContext(ctx)
	}
	fetcher.observer.Emit(ctx, obs.Event{
		Name:         "cache.fetch",
		URL:          request.URL.String(),
		Operation:    request.Method,
		CacheOutcome: string(result.Outcome),
		StatusCode:   result.Metadata.StatusCode,
		Duration:     duration,
		RequestID:    requestID,
	})
}

func isFresh(now time.Time, metadata Metadata) bool {
	return !metadata.ExpiresAt.IsZero() && now.Before(metadata.ExpiresAt)
}

func cachedResult(metadata Metadata, body []byte, now time.Time) *Result {
	if isFresh(now, metadata) {
		return &Result{
			Body:     cloneBytes(body),
			Metadata: freshMetadata(metadata),
			Outcome:  OutcomeFreshHit,
		}
	}
	return staleResult(metadata, body)
}

func staleResult(metadata Metadata, body []byte) *Result {
	metadata.Stale = true
	return &Result{
		Body:     cloneBytes(body),
		Metadata: metadata,
		Outcome:  OutcomeStaleHit,
	}
}

func freshMetadata(metadata Metadata) Metadata {
	metadata.Stale = false
	return metadata
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}

func cloneHeaders(header http.Header) http.Header {
	cloned := make(http.Header, len(header))
	for key, values := range header {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func mergeHeaders(existing, updated http.Header) http.Header {
	merged := cloneHeaders(existing)
	for key, values := range updated {
		if len(values) == 0 {
			continue
		}
		merged[key] = append([]string(nil), values...)
	}
	return merged
}

func coalesce(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func expiryFrom(now time.Time, cacheControl string, fallback time.Duration) time.Time {
	if cacheControl != "" {
		if maxAge, ok := parseMaxAge(cacheControl); ok {
			return now.Add(maxAge)
		}
	}
	if fallback > 0 {
		return now.Add(fallback)
	}
	return time.Time{}
}

func parseMaxAge(cacheControl string) (time.Duration, bool) {
	for _, directive := range strings.Split(cacheControl, ",") {
		directive = strings.TrimSpace(directive)
		if !strings.HasPrefix(strings.ToLower(directive), "max-age=") {
			continue
		}
		seconds, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(directive), "max-age=")))
		if err != nil {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	return 0, false
}
