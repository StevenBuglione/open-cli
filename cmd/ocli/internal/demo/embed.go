package demo

import _ "embed"

//go:embed testapi.openapi.yaml
var Spec []byte

// SpecPath is the embedded resource identifier used when the spec is served from memory.
const SpecPath = "embedded://demo/testapi.openapi.yaml"
