// Package api embeds the OpenAPI specification and serves it, plus a Swagger
// UI docs page.
package api

import (
	_ "embed"
	"net/http"
)

// OpenAPISpec is the embedded OpenAPI 3.1 document served at /openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPISpec []byte

const docsPage = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>BNPL API docs</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin></script>
    <script>
      window.ui = SwaggerUIBundle({ url: "/openapi.yaml", dom_id: "#swagger-ui" });
    </script>
  </body>
</html>
`

// SpecHandler serves the embedded OpenAPI document.
func SpecHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(OpenAPISpec)
}

// DocsHandler serves a Swagger UI page pointing at /openapi.yaml.
func DocsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(docsPage))
}
