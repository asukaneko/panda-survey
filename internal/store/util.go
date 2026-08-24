package store

import "encoding/json"

func jsonMarshalConfig(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func jsonUnmarshalConfig(s string, v any) error {
	if s == "" {
		s = "{}"
	}
	return json.Unmarshal([]byte(s), v)
}
