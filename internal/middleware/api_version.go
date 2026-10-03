package middleware

import "net/http"

const apiVersionHeader = "X-API-Version"
const apiSupportedVersionsHeader = "API-Supported-Versions"

// APIVersion advertises the public compatibility line served by the API.
func APIVersion(version string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(apiVersionHeader, version)
		w.Header().Set(apiSupportedVersionsHeader, version)
		next.ServeHTTP(w, r)
	})
}
