// Package accessauth gates HTTP handlers on a valid Cloudflare Access JWT.
//
// Cloudflare Access authenticates users at the edge (here via Managed OAuth)
// and forwards each allowed request with a signed JWT in the
// Cf-Access-Jwt-Assertion header. Verifying that JWT at the origin means a
// request that skipped Access — a misrouted tunnel hostname, or a process on
// the host calling the loopback port — is rejected instead of trusted.
package accessauth

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

// HeaderName is the header Cloudflare Access sets on requests it forwards.
const HeaderName = "Cf-Access-Jwt-Assertion"

// Config identifies the Access application whose tokens are accepted.
type Config struct {
	// TeamDomain is the Zero Trust team domain as a bare host,
	// e.g. "bergin-homelab.cloudflareaccess.com".
	TeamDomain string
	// AUD is the Access application's Audience (AUD) tag.
	AUD string
}

// Validate reports whether the config is complete and well formed.
func (c Config) Validate() error {
	if c.TeamDomain == "" || c.AUD == "" {
		return errors.New("access auth: team domain and AUD tag are both required")
	}
	if strings.Contains(c.TeamDomain, "/") {
		return errors.New("access auth: team domain must be a bare host, not a URL")
	}
	return nil
}

func (c Config) issuer() string { return "https://" + c.TeamDomain }

func (c Config) certsURL() string { return c.issuer() + "/cdn-cgi/access/certs" }

// Middleware returns a wrapper that passes only requests carrying a valid
// Access JWT for cfg: signature checked against the team's published keys,
// issuer equal to the team domain, AUD tag present in aud, and not expired.
// Anything else gets a 401 with an empty body.
//
// Keys are fetched lazily and cached; a token whose kid is not in the cache
// triggers a re-fetch, which handles Access key rotation. ctx scopes those
// fetches and may carry an HTTP client via oidc.ClientContext.
func Middleware(ctx context.Context, cfg Config) func(http.Handler) http.Handler {
	keys := oidc.NewRemoteKeySet(ctx, cfg.certsURL())
	verifier := oidc.NewVerifier(cfg.issuer(), keys, &oidc.Config{ClientID: cfg.AUD})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get(HeaderName)
			if token == "" {
				reject(w, r, "missing "+HeaderName)
				return
			}
			if _, err := verifier.Verify(r.Context(), token); err != nil {
				reject(w, r, err.Error())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// reject logs why a request was refused — never the token itself — and
// answers 401 without a body.
func reject(w http.ResponseWriter, r *http.Request, reason string) {
	log.Printf("access auth: rejected %s %s from %s: %s", r.Method, r.URL.Path, r.RemoteAddr, reason)
	w.WriteHeader(http.StatusUnauthorized)
}
