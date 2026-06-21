// Package docs holds the hand-maintained OpenAPI 3 specification served by the
// API. The spec is the single source of truth for the HTTP contract; edit
// openapi.yaml directly (there is no code-generation step).
package docs

import _ "embed"

// OpenAPISpec is the embedded OpenAPI 3.0 document (YAML) served at
// /openapi.yaml and rendered by the Swagger UI at /swagger/.
//
//go:embed openapi.yaml
var OpenAPISpec []byte
