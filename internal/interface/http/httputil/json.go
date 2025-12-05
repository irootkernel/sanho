package httputil

import (
	"encoding/json"
	"log"
	"net/http"
)

func WriteJSONError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		log.Printf("failed to write JSON error response: %v", err)
	}
}
