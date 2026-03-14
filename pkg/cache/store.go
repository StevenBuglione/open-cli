package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	ErrNotFound = errors.New("cache entry not found")
	ErrCorrupt  = errors.New("cache entry corrupt")
)

type FileStore struct {
	root string
}

func NewFileStore(root string) (*FileStore, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &FileStore{root: root}, nil
}

func (store *FileStore) metadataPath(key string) string {
	return filepath.Join(store.root, digestKey(key)+".json")
}

func (store *FileStore) bodyPath(key string) string {
	return filepath.Join(store.root, digestKey(key)+".body")
}

func (store *FileStore) Load(key string) (Metadata, []byte, error) {
	metadataPath := store.metadataPath(key)
	bodyPath := store.bodyPath(key)

	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Metadata{}, nil, ErrNotFound
		}
		return Metadata{}, nil, err
	}

	var metadata Metadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		_ = store.Delete(key)
		return Metadata{}, nil, ErrCorrupt
	}

	body, err := os.ReadFile(bodyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_ = store.Delete(key)
			return Metadata{}, nil, ErrCorrupt
		}
		return Metadata{}, nil, err
	}

	return metadata, body, nil
}

func (store *FileStore) Save(key string, metadata Metadata, body []byte) error {
	metadata.Key = key
	if err := os.MkdirAll(store.root, 0o755); err != nil {
		return err
	}

	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	metadataPath := store.metadataPath(key)
	bodyPath := store.bodyPath(key)
	if err := writeAtomically(metadataPath, metadataBytes); err != nil {
		return err
	}
	return writeAtomically(bodyPath, body)
}

func (store *FileStore) Delete(key string) error {
	var firstErr error
	for _, path := range []string{store.metadataPath(key), store.bodyPath(key)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (store *FileStore) Clear() error {
	if err := os.RemoveAll(store.root); err != nil {
		return err
	}
	return os.MkdirAll(store.root, 0o755)
}

func digestKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func writeAtomically(path string, data []byte) error {
	tempPath := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
