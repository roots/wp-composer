package composer

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed packages.json
var packagesJSONRaw []byte

// PackagesJSON returns the root Composer repository descriptor (packages.json).
// appURL is prepended to notify-batch and metadata-changes-url when non-empty.
func PackagesJSON(appURL string) ([]byte, error) {
	if appURL == "" {
		return packagesJSONRaw, nil
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(packagesJSONRaw, &payload); err != nil {
		return nil, fmt.Errorf("decoding embedded packages.json: %w", err)
	}

	payload["notify-batch"], _ = json.Marshal(appURL + "/downloads")
	payload["metadata-changes-url"], _ = json.Marshal(appURL + "/metadata/changes.json")

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encoding packages.json: %w", err)
	}
	return data, nil
}
