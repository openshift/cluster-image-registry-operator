package framework

import "encoding/json"

type JSONPatch struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func MarshalJSON(patch interface{}) []byte {
	buf, err := json.Marshal(patch)
	if err != nil {
		// This function is expected to be used only in tests with known data
		// that should always be marshable.
		panic(err)
	}
	return buf
}
