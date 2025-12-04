package http

import (
	_ "embed"
	"encoding/json"
	"net/http"

	"github.com/SeventeenthEarth/kkachi/internal/interface/http/handler"
)

//go:embed openapi.yaml
var openapiSpec []byte

func NewHTTPServer(addr string, projectHandler *handler.ProjectHandler, workspaceHandler *handler.WorkspaceHandler, docsHeadHandler *handler.DocsHeadHandler) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

	if projectHandler != nil {
		mux.HandleFunc("DELETE /projects/{project}", projectHandler.Delete)
		mux.HandleFunc("POST /projects", projectHandler.Add)
	}

	if workspaceHandler != nil {
		mux.HandleFunc("DELETE /workspaces/{workspace_id}", workspaceHandler.Delete)
		mux.HandleFunc("POST /workspaces/register", workspaceHandler.Register)
	}

	if docsHeadHandler != nil {
		mux.Handle("GET /docs/head", docsHeadHandler)
	}

	// Serve OpenAPI spec
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		w.Write(openapiSpec)
	})

	// Serve Swagger UI
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <meta name="description" content="SwaggerUI" />
  <title>SwaggerUI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css" />
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js" crossorigin></script>
<script>
  window.onload = () => {
    window.ui = SwaggerUIBundle({
      url: '/openapi.yaml',
      dom_id: '#swagger-ui',
    });
  };
</script>
</body>
</html>
`))
	})

	return &http.Server{
		Addr:    addr,
		Handler: loggingMiddleware(mux),
	}
}
