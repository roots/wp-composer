package composer

import (
	_ "embed"
	"encoding/json"
)

//go:embed packages.json
var packagesJSONRaw []byte

// PackagesJSON returns the root Composer repository descriptor (packages.json).
// appURL is prepended to notify-batch and metadata-changes-url when non-empty.
func PackagesJSON(appURL string) []byte {
	if appURL == "" {
		return packagesJSONRaw
	}

	var payload map[string]json.RawMessage
	_ = json.Unmarshal(packagesJSONRaw, &payload)

	payload["notify-batch"], _ = json.Marshal(appURL + "/downloads")
	payload["metadata-changes-url"], _ = json.Marshal(appURL + "/metadata/changes.json")

	data, _ := json.Marshal(payload)
	return data
}
