package engine

import "encoding/json"

// Small indirection so the engines do not each import encoding/json twice.
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

func jsonMarshalIndent(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }
