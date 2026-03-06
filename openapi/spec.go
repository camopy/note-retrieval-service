package openapi

import _ "embed"

//go:embed openapi.yaml
var spec []byte

func Bytes() []byte {
	copied := make([]byte, len(spec))
	copy(copied, spec)
	return copied
}

func String() string {
	return string(spec)
}
