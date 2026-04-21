package cli

import (
	"encoding/json"
	"io"
)

// newIndentEncoder returns a *json.Encoder configured to mirror Python's
// `json.dumps(..., indent=2, sort_keys=True)`. Go's encoder already sorts
// map keys alphabetically, so only indent setup is needed here.
func newIndentEncoder(w io.Writer) *json.Encoder {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc
}
