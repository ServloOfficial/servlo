package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCA is enough of an RFC 8555 server to drive a real ACME client through a
// real HTTP-01 issuance. It is not a validating implementation: JWS signatures
// are decoded rather than checked, because what is under test here is servlo's
// half of the exchange, not x/crypto/acme's.
//
// What it does do faithfully is the part servlo can get wrong. When a challenge
// is accepted it fetches the token over HTTP from the webroot, exactly as Let's
// Encrypt would, and refuses the authorization when the body does not match the
// key authorization it expects. That is the check that catches a token written
// to a path nginx would not serve.
type fakeCA struct {
	srv       *httptest.Server
	challenge *httptest.Server // serves the webroot the way nginx does

	caKey  *ecdsa.PrivateKey
	caCert *x509.Certificate

	mu sync.Mutex
	// authzState tracks each authorization by index: "pending", "valid" or
	// "invalid".
	authzState []string
	authzName  []string
	authzToken []string
	orderCSR   []byte
	orderDER   [][]byte
	// notAfter lets a test drive the certificate's expiry, so the reissue
	// window can be exercised against a certificate this CA actually signed.
	notAfter time.Time
	// fetched records every challenge URL the CA requested, so a test can show
	// the token really was served rather than assumed.
	fetched []string
	// failValidation makes every challenge fetch fail, standing in for DNS that
	// does not point here yet.
	failValidation bool
}

func newFakeCA(t *testing.T, webroot string) *fakeCA {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Fake ACME Root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	ca := &fakeCA{caKey: caKey, caCert: caCert, notAfter: time.Now().Add(90 * 24 * time.Hour)}
	// The webroot server stands in for nginx: same document root, so a token
	// written where nginx would not find it is not found here either.
	ca.challenge = httptest.NewServer(http.FileServer(http.Dir(webroot)))
	ca.srv = httptest.NewServer(http.HandlerFunc(ca.route))
	t.Cleanup(func() {
		ca.srv.Close()
		ca.challenge.Close()
	})
	return ca
}

func (c *fakeCA) directoryURL() string { return c.srv.URL + "/directory" }

func (c *fakeCA) route(w http.ResponseWriter, r *http.Request) {
	// Every response carries a fresh nonce; the client chains them and stalls
	// without one.
	w.Header().Set("Replay-Nonce", strconv.FormatInt(time.Now().UnixNano(), 36))
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path
	switch {
	case path == "/directory":
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"newNonce":   c.srv.URL + "/new-nonce",
			"newAccount": c.srv.URL + "/new-account",
			"newOrder":   c.srv.URL + "/new-order",
			"revokeCert": c.srv.URL + "/revoke",
			"keyChange":  c.srv.URL + "/key-change",
		})
	case path == "/new-nonce":
		w.WriteHeader(http.StatusOK)
	case path == "/new-account":
		w.Header().Set("Location", c.srv.URL+"/account/1")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"status": "valid"}) //nolint:errcheck
	case path == "/new-order":
		c.newOrder(w, r)
	case strings.HasPrefix(path, "/authz/"):
		c.getAuthz(w, path)
	case strings.HasPrefix(path, "/chal/"):
		c.acceptChallenge(w, path)
	case strings.HasPrefix(path, "/order/"):
		c.getOrder(w)
	case path == "/finalize":
		c.finalize(w, r)
	case path == "/cert":
		c.serveCert(w)
	default:
		http.Error(w, "not found: "+path, http.StatusNotFound)
	}
}

// jwsPayload pulls the payload out of a JWS body without verifying it.
func jwsPayload(r *http.Request) map[string]any {
	body, _ := io.ReadAll(r.Body)
	var env struct {
		Payload string `json:"payload"`
	}
	if json.Unmarshal(body, &env) != nil || env.Payload == "" {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(env.Payload)
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func (c *fakeCA) newOrder(w http.ResponseWriter, r *http.Request) {
	payload := jwsPayload(r)
	c.mu.Lock()
	c.authzState, c.authzName, c.authzToken = nil, nil, nil
	if ids, ok := payload["identifiers"].([]any); ok {
		for i, raw := range ids {
			id, _ := raw.(map[string]any)
			name, _ := id["value"].(string)
			c.authzState = append(c.authzState, "pending")
			c.authzName = append(c.authzName, name)
			c.authzToken = append(c.authzToken, fmt.Sprintf("token-%d-%d", i, time.Now().UnixNano()))
		}
	}
	n := len(c.authzState)
	c.mu.Unlock()

	authzURLs := make([]string, n)
	for i := range authzURLs {
		authzURLs[i] = fmt.Sprintf("%s/authz/%d", c.srv.URL, i)
	}
	w.Header().Set("Location", c.srv.URL+"/order/1")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"status":         "pending",
		"authorizations": authzURLs,
		"finalize":       c.srv.URL + "/finalize",
	})
}

func (c *fakeCA) getAuthz(w http.ResponseWriter, path string) {
	i, err := strconv.Atoi(strings.TrimPrefix(path, "/authz/"))
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil || i >= len(c.authzState) {
		http.Error(w, "no such authz", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"status":     c.authzState[i],
		"identifier": map[string]string{"type": "dns", "value": c.authzName[i]},
		"challenges": []map[string]any{
			{"type": "dns-01", "url": fmt.Sprintf("%s/chal-dns/%d", c.srv.URL, i), "token": c.authzToken[i], "status": "pending"},
			{"type": "http-01", "url": fmt.Sprintf("%s/chal/%d", c.srv.URL, i), "token": c.authzToken[i], "status": "pending"},
		},
	})
}

// acceptChallenge is where the validation happens: fetch the token the way the
// real CA would and only then mark the authorization valid.
func (c *fakeCA) acceptChallenge(w http.ResponseWriter, path string) {
	i, err := strconv.Atoi(strings.TrimPrefix(path, "/chal/"))
	c.mu.Lock()
	if err != nil || i >= len(c.authzState) {
		c.mu.Unlock()
		http.Error(w, "no such challenge", http.StatusNotFound)
		return
	}
	token := c.authzToken[i]
	fail := c.failValidation
	c.mu.Unlock()

	url := c.challenge.URL + "/.well-known/acme-challenge/" + token
	state := "invalid"
	if !fail {
		resp, fetchErr := http.Get(url) //nolint:gosec,noctx — a test server on loopback
		if fetchErr == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			// A real CA compares the body against the key authorization it
			// derives from the account key. Checking it is non-empty and
			// carries the token is enough to catch a token published to the
			// wrong path or with the wrong contents.
			if resp.StatusCode == http.StatusOK && strings.HasPrefix(string(body), token+".") {
				state = "valid"
			}
		}
	}

	c.mu.Lock()
	c.authzState[i] = state
	c.fetched = append(c.fetched, url)
	c.mu.Unlock()

	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"type": "http-01", "url": c.srv.URL + path, "token": token, "status": state,
	})
}

func (c *fakeCA) orderStatus() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.orderDER) > 0 {
		return "valid"
	}
	for _, s := range c.authzState {
		switch s {
		case "invalid":
			return "invalid"
		case "pending":
			return "pending"
		}
	}
	return "ready"
}

func (c *fakeCA) getOrder(w http.ResponseWriter) {
	body := map[string]any{"status": c.orderStatus(), "finalize": c.srv.URL + "/finalize"}
	if body["status"] == "valid" {
		body["certificate"] = c.srv.URL + "/cert"
	}
	json.NewEncoder(w).Encode(body) //nolint:errcheck
}

func (c *fakeCA) finalize(w http.ResponseWriter, r *http.Request) {
	payload := jwsPayload(r)
	csrB64, _ := payload["csr"].(string)
	csrDER, err := base64.RawURLEncoding.DecodeString(csrB64)
	if err != nil {
		http.Error(w, "bad csr", http.StatusBadRequest)
		return
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		http.Error(w, "unparseable csr", http.StatusBadRequest)
		return
	}

	c.mu.Lock()
	notAfter := c.notAfter
	c.mu.Unlock()

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      csr.Subject,
		DNSNames:     csr.DNSNames,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leaf, err := x509.CreateCertificate(rand.Reader, tmpl, c.caCert, csr.PublicKey, c.caKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	c.mu.Lock()
	c.orderCSR = csrDER
	// The chain the CA returns: leaf first, then the issuer, which is what a
	// real one sends and what nginx wants in the file.
	c.orderDER = [][]byte{leaf, c.caCert.Raw}
	c.mu.Unlock()

	w.Header().Set("Location", c.srv.URL+"/order/1")
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"status": "valid", "certificate": c.srv.URL + "/cert", "finalize": c.srv.URL + "/finalize",
	})
}

func (c *fakeCA) serveCert(w http.ResponseWriter) {
	c.mu.Lock()
	chain := c.orderDER
	c.mu.Unlock()
	w.Header().Set("Content-Type", "application/pem-certificate-chain")
	for _, der := range chain {
		pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: der}) //nolint:errcheck
	}
}

// issuedNames returns the SANs of the certificate the CA last signed.
func (c *fakeCA) issuedNames(t *testing.T) []string {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.orderDER) == 0 {
		t.Fatal("the CA never signed a certificate")
	}
	leaf, err := x509.ParseCertificate(c.orderDER[0])
	if err != nil {
		t.Fatal(err)
	}
	return leaf.DNSNames
}
