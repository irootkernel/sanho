package http

import (
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/SeventeenthEarth/kkachi/internal/interface/http/handler"
)

//go:embed openapi.yaml
var openapiSpec []byte

// ServerConfig holds configuration for the HTTP server.
type ServerConfig struct {
	Addr       string
	WebDistDir string // Path to web distribution directory (default: "web/dist")
}

func NewHTTPServer(cfg ServerConfig, projectHandler *handler.ProjectHandler, workspaceHandler *handler.WorkspaceHandler, docsHeadHandler *handler.DocsHeadHandler, docsSnapshotHandler *handler.DocsSnapshotHandler, docsPushHandler *handler.DocsPushHandler, stateHandler *handler.StateHandler) *http.Server {
	// Apply defaults
	if cfg.WebDistDir == "" {
		cfg.WebDistDir = "web/dist"
	}

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

	if docsSnapshotHandler != nil {
		mux.Handle("GET /docs/snapshot", docsSnapshotHandler)
	}

	if docsPushHandler != nil {
		mux.Handle("POST /docs/push", docsPushHandler)
	}

	if stateHandler != nil {
		mux.Handle("GET /state", stateHandler)
		// API alias for Web UI (v2)
		mux.Handle("GET /api/state", stateHandler)
	}

	// Fallback for unknown /api/* paths - return 404 JSON instead of SPA
	apiNotFoundHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "not_found",
			"message": "API endpoint not found",
		})
	}
	mux.HandleFunc("GET /api", apiNotFoundHandler)           // exact /api (no trailing slash)
	mux.HandleFunc("GET /api/{path...}", apiNotFoundHandler) // /api/* with any path

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

	// Static file serving for Web UI (v2)
	registerWebUIHandlers(mux, cfg.WebDistDir)

	return &http.Server{
		Addr:    cfg.Addr,
		Handler: loggingMiddleware(mux),
	}
}

// registerWebUIHandlers registers handlers for serving the web UI static files.
// Routing rules (per requirement):
//   - GET /api/* → API endpoints (handled separately)
//   - GET /assets/* → web/dist/assets/* (static files)
//   - GET /* → web/dist/index.html (SPA fallback, excluding existing API routes)
func registerWebUIHandlers(mux *http.ServeMux, webDistDir string) {
	// Check if web dist directory exists
	webDistExists := false
	if info, err := os.Stat(webDistDir); err == nil && info.IsDir() {
		webDistExists = true
	}

	if !webDistExists {
		log.Printf("Warning: Web distribution directory not found: %s. Static file serving is disabled.", webDistDir)
		// Register fallback handler for root that returns appropriate error
		notFoundHandler := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "web_dist_not_found",
				"message": "Web distribution directory not found. Please build the web UI first.",
			})
		}
		mux.HandleFunc("GET /", notFoundHandler)
		return
	}

	indexPath := filepath.Join(webDistDir, "index.html")
	assetsDir := filepath.Join(webDistDir, "assets")

	// Serve /assets/* as static files (JS/CSS/images)
	if info, err := os.Stat(assetsDir); err == nil && info.IsDir() {
		assetsFS := http.FileServer(http.Dir(assetsDir))
		mux.Handle("GET /assets/", http.StripPrefix("/assets/", assetsFS))
	}

	// SPA fallback handler
	spaHandler := func(w http.ResponseWriter, r *http.Request) {
		// Serve index.html for SPA routes
		content, err := os.ReadFile(indexPath)
		if err != nil {
			log.Printf("Error reading index.html: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "internal_server_error"})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(content)
	}

	// Serve root and all unmatched paths as SPA entry
	// NOTE: API routes (/state, /api/*, /docs/*, /healthz, etc.) are registered
	// BEFORE this handler and will be matched first by Go's ServeMux.
	//
	// SECURITY: We use http.Dir to safely serve static files, which prevents
	// path traversal attacks by rejecting paths that escape the root.
	staticFS := http.Dir(webDistDir)

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// For exact root path, serve SPA
		if r.URL.Path == "/" {
			spaHandler(w, r)
			return
		}

		// Try to serve static file from webDistDir (e.g., favicon.ico)
		// Use http.Dir.Open for safe path handling (prevents path traversal)
		cleanPath := strings.TrimPrefix(r.URL.Path, "/")
		if f, err := staticFS.Open(cleanPath); err == nil {
			defer f.Close()
			if stat, err := f.Stat(); err == nil && !stat.IsDir() {
				http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
				return
			}
		}

		// For all other paths (SPA routes like /projects/..., /debug/...), serve index.html
		spaHandler(w, r)
	})
}

// staticFileHandler serves static files with proper MIME types.
func staticFileHandler(root string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(root, filepath.Clean(r.URL.Path))

		// Security: prevent path traversal
		if !strings.HasPrefix(path, root) {
			http.NotFound(w, r)
			return
		}

		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		if info.IsDir() {
			// Try index.html
			indexPath := filepath.Join(path, "index.html")
			if _, err := os.Stat(indexPath); err == nil {
				http.ServeFile(w, r, indexPath)
				return
			}
			http.NotFound(w, r)
			return
		}

		http.ServeFile(w, r, path)
	})
}
