package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Event struct {
	Timestamp     time.Time          `json:"timestamp"`
	EventType     string             `json:"eventType,omitempty"`
	Principal     string             `json:"principal,omitempty"`
	Lineage       *DelegationLineage `json:"lineage,omitempty"`
	SessionID     string             `json:"sessionId,omitempty"`
	AgentProfile  string             `json:"agentProfile,omitempty"`
	ToolID        string             `json:"toolId"`
	ServiceID     string             `json:"serviceId,omitempty"`
	TargetBaseURL string             `json:"targetBaseUrl,omitempty"`
	Decision      string             `json:"decision"`
	ReasonCode    string             `json:"reasonCode"`
	Method        string             `json:"method,omitempty"`
	Path          string             `json:"path,omitempty"`
	AuthScheme    string             `json:"authScheme,omitempty"`
	RequestSize   int                `json:"requestSize,omitempty"`
	StatusCode    int                `json:"statusCode,omitempty"`
	LatencyMS     int64              `json:"latencyMs,omitempty"`
	RetryCount    int                `json:"retryCount,omitempty"`
}

type DelegationLineage struct {
	DelegatedBy  string            `json:"delegatedBy,omitempty"`
	DelegationID string            `json:"delegationId,omitempty"`
	Actor        map[string]string `json:"actor,omitempty"`
}

type FileStore struct {
	path string
	mu   sync.Mutex
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (store *FileStore) Path() string {
	if store == nil {
		return ""
	}
	return store.path
}

func (store *FileStore) Append(event Event) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(store.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(store.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	return encoder.Encode(event)
}

func (store *FileStore) List() ([]Event, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	file, err := os.Open(store.path)
	if os.IsNotExist(err) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var events []Event
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}
