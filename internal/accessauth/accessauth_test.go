package accessauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
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
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT"))
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
// context carries the test server's TLS client so the key set can fetch.
func (ti *testIssuer) gated() http.Handler {
	ctx := oidc.ClientContext(context.Background(), ti.server.Client())
	gate := Middleware(ctx, Config{TeamDomain: ti.teamDomain(), AUD: testAUD})
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

	cases := map[string]string{
		"missing header": "",
		"not a JWT":      "garbage",
		"bad signature":  sign(t, forged, valid),
		"expired":        sign(t, published, ti.claims(testAUD, time.Now().Add(-time.Hour))),
		"wrong audience": sign(t, published, ti.claims("some-other-app", time.Now().Add(time.Hour))),
		"wrong issuer":   sign(t, published, wrongIssuer),
	}
	handler := ti.gated()
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			rec := request(handler, token)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("got %d, want 401", rec.Code)
			}
			if rec.Body.Len() != 0 {
				t.Fatalf("rejection body must be empty, got %q", rec.Body.String())
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
