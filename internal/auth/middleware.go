package auth

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

type unauthorizedResponse struct {
	Error string `json:"error"`
}

func Bearer(apiKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := extractBearerToken(r.Header.Get("Authorization"))
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(apiKey)) != 1 {
			respondUnauthorized(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func extractBearerToken(authorization string) (string, bool) {
	parts := strings.SplitN(strings.TrimSpace(authorization), " ", 2)
	if len(parts) != 2 {
		return "", false
	}
	if !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	if strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	return parts[1], true
}

func respondUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(unauthorizedResponse{Error: "unauthorized"})
}
