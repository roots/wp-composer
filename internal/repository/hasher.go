package repository

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// DeterministicJSON produces JSON with recursively sorted keys for reproducible hashes.
func DeterministicJSON(v any) ([]byte, error) {
	sorted := sortKeys(v)
	return json.Marshal(sorted)
}

// HashJSON returns the SHA-256 hex digest of deterministic JSON.
func HashJSON(v any) (string, []byte, error) {
	data, err := DeterministicJSON(v)
	if err != nil {
		return "", nil, fmt.Errorf("encoding JSON: %w", err)
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h), data, nil
}

func sortKeys(v any) any {
	switch val := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sorted := make(map[string]any, len(val))
		for _, k := range keys {
			sorted[k] = sortKeys(val[k])
		}
		return sorted
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = sortKeys(item)
		}
		return result
	default:
		return v
	}
}
