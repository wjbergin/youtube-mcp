package accessauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

const testAUD = "test-aud-tag"

// testIssuer stands in for a Cloudflare Access team domain: it serves a JWKS
// at /cdn-cgi/access/certs over TLS, so the middleware derives the issuer and
// certs URL from a bare host exactly as it does in production.
type testIssuer struct {
	server  *httptest.Server
	mu      sync.Mutex
	keys    []jose.JSONWebKey
	fetches int
}

func newTestIssuer(t *testing.T) *testIssuer {
	t.Helper()
	ti := &testIssuer{}
	ti.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cdn-cgi/access/certs" {
			http.NotFound(w, r)
			return
		}
		ti.mu.Lock()
		defer ti.mu.Unlock()
		ti.fetches++
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: ti.keys})
	}))
	t.Cleanup(ti.server.Close)
	return ti
}

func (ti *testIssuer) teamDomain() string {
	return strings.TrimPrefix(ti.server.URL, "https://")
}

func (ti *testIssuer) fetchCount() int {
	ti.mu.Lock()
	defer ti.mu.Unlock()
	return ti.fetches
}

// newKey generates an RSA key; publish controls whether the JWKS serves it.
func (ti *testIssuer) newKey(t *testing.T, kid string, publish bool) jose.JSONWebKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if publish {
		ti.mu.Lock()
		ti.keys = append(ti.keys, jose.JSONWebKey{Key: &priv.PublicKey, KeyID: kid, Algorithm: "RS256", Use: "sig"})
		ti.mu.Unlock()
	}
	return jose.JSONWebKey{Key: priv, KeyID: kid, Algorithm: "RS256"}
}

func (ti *testIssuer) claims(aud string, exp time.Time) map[string]any {
	return map[string]any{
		"iss":   ti.server.URL,
		"aud":   []string{aud},
		"sub":   "user-id",
		"email": "bill@example.com",
		"iat":   time.Now().Unix(),
		"exp":   exp.Unix(),
	}
}

func sign(t *testing.T, key jose.JSONWebKey, claims map[string]any) string {
	t.Helper()
	// go-jose only auto-embeds "kid" for asymmetric keys (it derives the
	// header from the key's own public half); an HMAC key has no public
	// half, so set "kid" explicitly to cover both cases uniformly.
	opts := (&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), key.KeyID)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.SignatureAlgorithm(key.Algorithm), Key: key}, opts)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	object, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	token, err := object.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// gated wraps a handler that answers 200 in the middleware under test. The
// config's HTTPClient is the test server's TLS client so the key set can
// fetch from it despite the self-signed cert.
func (ti *testIssuer) gated() http.Handler {
	gate := Middleware(context.Background(), Config{TeamDomain: ti.teamDomain(), AUD: testAUD, HTTPClient: ti.server.Client()})
	return gate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func request(handler http.Handler, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	if token != "" {
		req.Header.Set(HeaderName, token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestValidTokenPassesThrough(t *testing.T) {
	ti := newTestIssuer(t)
	key := ti.newKey(t, "k1", true)
	token := sign(t, key, ti.claims(testAUD, time.Now().Add(time.Hour)))

	if got := request(ti.gated(), token).Code; got != http.StatusOK {
		t.Fatalf("valid token: got %d, want 200", got)
	}
}

func TestRejectsInvalidTokens(t *testing.T) {
	ti := newTestIssuer(t)
	published := ti.newKey(t, "k1", true)
	// Same kid as the published key, but a private key the JWKS never served.
	forged := ti.newKey(t, "k1", false)
	valid := ti.claims(testAUD, time.Now().Add(time.Hour))

	wrongIssuer := ti.claims(testAUD, time.Now().Add(time.Hour))
	wrongIssuer["iss"] = "https://evil.example.com"

	// Algorithm confusion: an HMAC-signed token using the same kid as the
	// published RSA key, with the RSA public key's bytes nowhere involved.
	// The verifier must reject it for using an unsupported algorithm (checked
	// below), not merely because the HMAC key happens to be the wrong type
	// for the RSA key set.
	hmacKey := jose.JSONWebKey{Key: []byte("not-the-real-secret-but-32-bytes"), KeyID: "k1", Algorithm: "HS256"}

	cases := map[string]string{
		"missing header":      "",
		"not a JWT":           "garbage",
		"bad signature":       sign(t, forged, valid),
		"expired":             sign(t, published, ti.claims(testAUD, time.Now().Add(-time.Hour))),
		"wrong audience":      sign(t, published, ti.claims("some-other-app", time.Now().Add(time.Hour))),
		"wrong issuer":        sign(t, published, wrongIssuer),
		"HS256 alg confusion": sign(t, hmacKey, valid),
	}
	handler := ti.gated()

	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })

	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			buf.Reset()
			rec := request(handler, token)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("got %d, want 401", rec.Code)
			}
			if rec.Body.Len() != 0 {
				t.Fatalf("rejection body must be empty, got %q", rec.Body.String())
			}
			if name == "HS256 alg confusion" && !strings.Contains(buf.String(), "unexpected signature algorithm") {
				t.Fatalf("expected rejection reason to cite the unsupported algorithm, got %q", buf.String())
			}
		})
	}
}

// Access rotates its signing keys; a token signed with a key the cached set
// has never seen must trigger a re-fetch rather than a permanent failure.
func TestUnknownKeyIDTriggersRefetch(t *testing.T) {
	ti := newTestIssuer(t)
	first := ti.newKey(t, "k1", true)
	handler := ti.gated()

	if got := request(handler, sign(t, first, ti.claims(testAUD, time.Now().Add(time.Hour)))).Code; got != http.StatusOK {
		t.Fatalf("first key: got %d, want 200", got)
	}
	fetchesBefore := ti.fetchCount()

	rotated := ti.newKey(t, "k2", true)
	if got := request(handler, sign(t, rotated, ti.claims(testAUD, time.Now().Add(time.Hour)))).Code; got != http.StatusOK {
		t.Fatalf("rotated key: got %d, want 200", got)
	}
	if ti.fetchCount() <= fetchesBefore {
		t.Fatalf("expected a JWKS re-fetch for the unknown kid; fetches stayed at %d", fetchesBefore)
	}
}

// If Access's certs endpoint is simply down, the fetch must fail fast with a
// plain connection error rather than hang.
func TestUnreachableCertsEndpointRejects(t *testing.T) {
	ti := newTestIssuer(t)
	key := ti.newKey(t, "k1", true)
	token := sign(t, key, ti.claims(testAUD, time.Now().Add(time.Hour)))
	handler := ti.gated()

	ti.server.Close() // simulate the certs endpoint being unreachable

	rec := request(handler, token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("rejection body must be empty, got %q", rec.Body.String())
	}
}

// If Access's certs endpoint accepts the connection but never responds, the
// bounded HTTPClient must still cut the fetch off instead of hanging the
// request (and the inflight JWKS fetch) forever.
func TestStalledCertsEndpointRejectsPromptly(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	// t.Cleanup runs LIFO: register Close first (runs last) and the release
	// second (runs first), so the stalled handler unblocks and returns
	// before Close waits for outstanding requests to finish.
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	client := server.Client()
	client.Timeout = 200 * time.Millisecond

	teamDomain := strings.TrimPrefix(server.URL, "https://")
	gate := Middleware(context.Background(), Config{TeamDomain: teamDomain, AUD: testAUD, HTTPClient: client})
	handler := gate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// The token's shape only needs to be valid enough to reach the key
	// fetch; the stalled server never serves a JWKS to verify against.
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	key := jose.JSONWebKey{Key: priv, KeyID: "k1", Algorithm: "RS256"}
	token := sign(t, key, map[string]any{
		"iss": server.URL,
		"aud": []string{testAUD},
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	start := time.Now()
	rec := request(handler, token)
	elapsed := time.Since(start)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("rejection body must be empty, got %q", rec.Body.String())
	}
	if elapsed > 2*time.Second {
		t.Fatalf("request took %v, want it to fail within ~2s of the 200ms client timeout", elapsed)
	}
}

// go-oidc's fetch errors can embed the full HTTP response body (e.g. a large
// Cloudflare HTML error page), so the logged reason must be capped.
func TestRejectReasonTruncated(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })

	longReason := strings.Repeat("x", 500)
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rec := httptest.NewRecorder()

	reject(rec, req, longReason)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
	logged := buf.String()
	if strings.Contains(logged, longReason) {
		t.Fatalf("expected the long reason to be truncated, but it was logged in full: %q", logged)
	}
	if !strings.Contains(logged, "…") {
		t.Fatalf("expected a truncation marker in the logged reason, got %q", logged)
	}
}

func TestConfigValidate(t *testing.T) {
	cases := map[string]struct {
		cfg     Config
		wantErr bool
	}{
		"complete":            {Config{TeamDomain: "team.cloudflareaccess.com", AUD: "abc"}, false},
		"missing team domain": {Config{AUD: "abc"}, true},
		"missing aud":         {Config{TeamDomain: "team.cloudflareaccess.com"}, true},
		"team domain is URL":  {Config{TeamDomain: "https://team.cloudflareaccess.com", AUD: "abc"}, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
