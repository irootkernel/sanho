package http

import (
	"encoding/json"
	"net/http"

	"github.com/SeventeenthEarth/kkachi/internal/interface/http/handler"
)

// ServerConfig holds configuration for the HTTP server.
type ServerConfig struct {
	Addr string
}

func NewHTTPServer(
	cfg ServerConfig,
	projectHandler *handler.ProjectHandler,
	workspaceHandler *handler.WorkspaceHandler,
	docsHeadHandler *handler.DocsHeadHandler,
	docsSnapshotHandler *handler.DocsSnapshotHandler,
	docsPushHandler *handler.DocsPushHandler,
	stateHandler *handler.StateHandler,
) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
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
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "not_found",
			"message": "endpoint not found",
		})
	})

	return &http.Server{
		Addr:    cfg.Addr,
		Handler: loggingMiddleware(mux),
	}
}
