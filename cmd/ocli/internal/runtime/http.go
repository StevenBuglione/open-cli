package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// HTTPError represents an HTTP error response from the runtime.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string { return e.Body }

// PostJSON sends a JSON POST request and decodes the response into T.
func PostJSON[T any](endpoint string, payload any, token string) (T, error) {
	var zero T
	body, err := json.Marshal(payload)
	if err != nil {
		return zero, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return zero, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		return zero, fmt.Errorf("%s", strings.TrimSpace(string(data)))
	}
	var decoded T
	err = json.NewDecoder(resp.Body).Decode(&decoded)
	return decoded, err
}

// GetJSON sends a GET request and decodes the JSON response into T.
func GetJSON[T any](endpoint, token string) (T, error) {
	var zero T
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return zero, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		return zero, fmt.Errorf("%s", strings.TrimSpace(string(data)))
	}
	var decoded T
	err = json.NewDecoder(resp.Body).Decode(&decoded)
	return decoded, err
}
