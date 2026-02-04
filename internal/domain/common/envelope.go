package common

// Envelope is a domain-level representation of incoming messages
type Envelope struct {
	Type string                 `json:"type"`
	Body map[string]interface{} `json:"body"`
}
