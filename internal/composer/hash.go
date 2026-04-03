package composer

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// HashJSON returns the SHA-256 hex digest and marshaled bytes of a JSON value.
func HashJSON(v any) (string, []byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", nil, fmt.Errorf("encoding JSON: %w", err)
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h), data, nil
}
