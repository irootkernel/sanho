package httputil

import (
	"encoding/json"
	"log"
	"net/http"
)

func WriteJSONError(w http.ResponseWriter, code int, message string) {
	WriteJSON(w, code, map[string]string{"error": message})
}

func WriteJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("failed to write JSON response: %v", err)
	}
}
