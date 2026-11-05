package middleware

import (
	"net"
	"net/http"
	"time"

	"github.com/cheng-alvin/esplanade/server/respond"
	"github.com/go-chi/httprate"
)

const (
	defaultRateLimitRequests = 100
	defaultRateLimitWindow   = time.Minute
)

// RateLimit returns middleware that caps requests per client IP over a sliding
// window. Non-positive values fall back to the defaults rather than disabling
// the limit, and health probes are exempt so orchestrator checks are never
// throttled.
func RateLimit(requests int, window time.Duration) Middleware {
	if requests <= 0 {
		requests = defaultRateLimitRequests
	}
	if window <= 0 {
		window = defaultRateLimitWindow
	}

	limited := httprate.LimitBy(requests, window, clientIPKey,
		httprate.WithLimitHandler(func(w http.ResponseWriter, _ *http.Request) {
			respond.ErrorCode(w, http.StatusTooManyRequests, "rate_limited", "too many requests; try again later")
		}),
	)

	return func(next http.Handler) http.Handler {
		limitedNext := limited(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/healthz", "/readyz":
				next.ServeHTTP(w, r)
			default:
				limitedNext.ServeHTTP(w, r)
			}
		})
	}
}

// clientIPKey keys requests by the transport-level peer address. Forwarding
// headers (X-Forwarded-For, X-Real-IP) are deliberately ignored: without a
// configured proxy trust boundary they are client-spoofable, letting a caller
// evade the limit or pin it onto another IP. A proxy deployment should resolve
// the client IP against its trusted hops instead.
func clientIPKey(r *http.Request) (string, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return httprate.CanonicalizeIP(host), nil
}
