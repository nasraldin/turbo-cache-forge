package openapi

import (
	_ "embed"
	"net/http"

	"github.com/flowchartsman/swaggerui"
)

//go:embed openapi.yaml
var Spec []byte

// Handler serves the embedded Swagger UI rendering Spec.
func Handler() http.Handler { return swaggerui.Handler(Spec) }

// ponytail: hand-written spec, no swag codegen; swaggerui embeds the UI dist so no CDN and no build step.
