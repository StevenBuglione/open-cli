package server

// Client represents a registered OAuth client.
type Client struct {
	ID     string
	Secret string
	Scopes []string
}

// Store holds registered OAuth clients.
type Store struct {
	clients map[string]*Client
}

// NewStore returns a Store pre-seeded with a known test client.
func NewStore() *Store {
	return &Store{
		clients: map[string]*Client{
			"test-client": {
				ID:     "test-client",
				Secret: "test-secret",
				Scopes: []string{"api.read", "api.write"},
			},
			"short-ttl-client": {
				ID:     "short-ttl-client",
				Secret: "short-ttl-secret",
				Scopes: []string{"api.read"},
			},
		},
	}
}

// Lookup returns the client with the given ID, or nil if not found.
func (s *Store) Lookup(id string) *Client { return s.clients[id] }

// ValidateScopes reports whether all requested scopes are allowed for the client.
func (s *Store) ValidateScopes(c *Client, requested []string) bool {
	allowed := make(map[string]bool, len(c.Scopes))
	for _, sc := range c.Scopes {
		allowed[sc] = true
	}
	for _, sc := range requested {
		if !allowed[sc] {
			return false
		}
	}
	return true
}
