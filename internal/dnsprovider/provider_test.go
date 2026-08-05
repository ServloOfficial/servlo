package dnsprovider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The record name is fixed by RFC 8555 and is the single easiest thing to get
// wrong: publishing at the domain instead of _acme-challenge under it fails
// validation with no useful message from the authority.
func TestChallengeRecordName(t *testing.T) {
	cases := map[string]string{
		"example.com":       "_acme-challenge.example.com",
		"www.example.com":   "_acme-challenge.www.example.com",
		"*.example.com":     "_acme-challenge.example.com",
		"*.dev.example.com": "_acme-challenge.dev.example.com",
	}
	for in, want := range cases {
		if got := ChallengeRecordName(in); got != want {
			t.Errorf("ChallengeRecordName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A wildcard and its base share one challenge record name, so an order covering
// both must publish one record carrying both values rather than one replacing
// the other. Getting this wrong is the classic DNS-01 wildcard failure.
func TestChallengeRecordName_WildcardSharesTheBaseRecord(t *testing.T) {
	if ChallengeRecordName("*.example.com") != ChallengeRecordName("example.com") {
		t.Error("a wildcard and its base must resolve to the same challenge record")
	}
}

// Zone discovery has to pick the registrable zone, not the longest label run:
// a record under foo.bar.example.com belongs to the example.com zone unless the
// provider actually hosts bar.example.com separately.
func TestLongestZoneSuffix(t *testing.T) {
	zones := []string{"example.com", "bar.example.com", "other.net"}
	cases := map[string]string{
		"_acme-challenge.example.com":         "example.com",
		"_acme-challenge.www.example.com":     "example.com",
		"_acme-challenge.foo.bar.example.com": "bar.example.com",
		"_acme-challenge.other.net":           "other.net",
	}
	for record, want := range cases {
		got, err := longestZoneSuffix(record, zones)
		if err != nil {
			t.Errorf("longestZoneSuffix(%q): %v", record, err)
			continue
		}
		if got != want {
			t.Errorf("longestZoneSuffix(%q) = %q, want %q", record, got, want)
		}
	}
	if _, err := longestZoneSuffix("_acme-challenge.nowhere.test", zones); err == nil {
		t.Error("a record with no matching zone was accepted")
	}
}

// ── Cloudflare ────────────────────────────────────────────────────────────────

type cfAPI struct {
	srv     *httptest.Server
	created map[string]string // record name -> content
	deleted []string
	auth    string
}

func newCloudflareAPI(t *testing.T) *cfAPI {
	t.Helper()
	api := &cfAPI{created: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/zones", func(w http.ResponseWriter, r *http.Request) {
		api.auth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"success": true,
			"result":  []map[string]any{{"id": "zone1", "name": "example.com"}},
		})
	})
	mux.HandleFunc("/zones/zone1/dns_records", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			name, _ := body["name"].(string)
			content, _ := body["content"].(string)
			api.created[name] = content
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"success": true, "result": map[string]any{"id": "rec1"},
			})
		case http.MethodGet:
			var results []map[string]any
			for name, content := range api.created {
				results = append(results, map[string]any{"id": "rec1", "name": name, "content": content})
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "result": results}) //nolint:errcheck
		}
	})
	mux.HandleFunc("/zones/zone1/dns_records/rec1", func(w http.ResponseWriter, r *http.Request) {
		api.deleted = append(api.deleted, r.URL.Path)
		json.NewEncoder(w).Encode(map[string]any{"success": true}) //nolint:errcheck
	})
	api.srv = httptest.NewServer(mux)
	t.Cleanup(api.srv.Close)
	return api
}

func TestCloudflare_PublishesAndRemovesTheChallengeRecord(t *testing.T) {
	api := newCloudflareAPI(t)
	p := &cloudflareProvider{token: "cf-token", baseURL: api.srv.URL, client: api.srv.Client()}

	if err := p.SetTXT(context.Background(), "_acme-challenge.example.com", "value-1"); err != nil {
		t.Fatalf("SetTXT: %v", err)
	}
	if got := api.created["_acme-challenge.example.com"]; got != "value-1" {
		t.Errorf("published %q, want the key authorization digest", got)
	}
	if api.auth != "Bearer cf-token" {
		t.Errorf("authorization header = %q, want a bearer token", api.auth)
	}

	if err := p.RemoveTXT(context.Background(), "_acme-challenge.example.com", "value-1"); err != nil {
		t.Fatalf("RemoveTXT: %v", err)
	}
	if len(api.deleted) != 1 {
		t.Errorf("deleted %v, want the record removed", api.deleted)
	}
}

// A provider that reports failure in the body with a 200 status is a real
// pattern, and treating it as success would leave the challenge unpublished
// while servlo waits for a validation that cannot happen.
func TestCloudflare_TreatsABodyLevelFailureAsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"success": false,
			"errors":  []map[string]any{{"code": 9109, "message": "Invalid access token"}},
		})
	}))
	defer srv.Close()
	p := &cloudflareProvider{token: "bad", baseURL: srv.URL, client: srv.Client()}

	err := p.SetTXT(context.Background(), "_acme-challenge.example.com", "v")
	if err == nil {
		t.Fatal("a body-level failure was treated as success")
	}
	if !strings.Contains(err.Error(), "Invalid access token") {
		t.Errorf("error %q does not carry what the provider said", err)
	}
}

// ── DigitalOcean ──────────────────────────────────────────────────────────────

func newDigitalOceanAPI(t *testing.T, created map[string]string, deleted *[]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/domains", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"domains": []map[string]any{{"name": "example.com"}},
		})
	})
	mux.HandleFunc("/v2/domains/example.com/records", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			name, _ := body["name"].(string)
			data, _ := body["data"].(string)
			created[name] = data
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"domain_record": map[string]any{"id": 42},
			})
		case http.MethodGet:
			var recs []map[string]any
			for name, data := range created {
				recs = append(recs, map[string]any{"id": 42, "type": "TXT", "name": name, "data": data})
			}
			json.NewEncoder(w).Encode(map[string]any{"domain_records": recs}) //nolint:errcheck
		}
	})
	mux.HandleFunc("/v2/domains/example.com/records/42", func(w http.ResponseWriter, r *http.Request) {
		*deleted = append(*deleted, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// DigitalOcean names records relative to the zone, so the full record name has
// to be trimmed to its subdomain part. Sending the fully qualified name creates
// _acme-challenge.example.com.example.com, which validates against nothing.
func TestDigitalOcean_PublishesRelativeToTheZone(t *testing.T) {
	created := map[string]string{}
	var deleted []string
	srv := newDigitalOceanAPI(t, created, &deleted)
	p := &digitalOceanProvider{token: "do-token", baseURL: srv.URL, client: srv.Client()}

	if err := p.SetTXT(context.Background(), "_acme-challenge.www.example.com", "value-1"); err != nil {
		t.Fatalf("SetTXT: %v", err)
	}
	if _, ok := created["_acme-challenge.www"]; !ok {
		t.Errorf("published %v, want a name relative to the zone", created)
	}

	if err := p.RemoveTXT(context.Background(), "_acme-challenge.www.example.com", "value-1"); err != nil {
		t.Fatalf("RemoveTXT: %v", err)
	}
	if len(deleted) != 1 {
		t.Errorf("deleted %v, want the record removed", deleted)
	}
}

// ── Route53 ───────────────────────────────────────────────────────────────────

func TestRoute53_SignsItsRequests(t *testing.T) {
	var gotAuth, gotBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/2013-04-01/hostedzone", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		io.WriteString(w, `<ListHostedZonesResponse><HostedZones>`+ //nolint:errcheck
			`<HostedZone><Id>/hostedzone/Z1</Id><Name>example.com.</Name></HostedZone>`+
			`</HostedZones></ListHostedZonesResponse>`)
	})
	mux.HandleFunc("/2013-04-01/hostedzone/Z1/rrset/", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "text/xml")
		io.WriteString(w, `<ChangeResourceRecordSetsResponse/>`) //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := &route53Provider{
		creds:   Credentials{Provider: Route53, AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret", Region: "us-east-1"},
		baseURL: srv.URL,
		client:  srv.Client(),
	}
	if err := p.SetTXT(context.Background(), "_acme-challenge.example.com", "value-1"); err != nil {
		t.Fatalf("SetTXT: %v", err)
	}

	// Route53 is signed rather than bearer-authenticated, which is the whole
	// reason it needs its own code path.
	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 ") {
		t.Errorf("authorization = %q, want a SigV4 signature", gotAuth)
	}
	if !strings.Contains(gotAuth, "AKIAEXAMPLE") {
		t.Errorf("authorization %q does not carry the access key id", gotAuth)
	}
	if strings.Contains(gotAuth, "secret") {
		t.Errorf("authorization %q leaks the secret access key", gotAuth)
	}
	// The value has to reach Route53 quoted; an unquoted TXT value is rejected.
	if !strings.Contains(gotBody, `&#34;value-1&#34;`) && !strings.Contains(gotBody, `"value-1"`) {
		t.Errorf("change body does not carry a quoted TXT value: %s", gotBody)
	}
}

// ── Selection ─────────────────────────────────────────────────────────────────

func TestFor_RefusesAnUnconfiguredProvider(t *testing.T) {
	credsEnv(t)
	if _, err := For(Cloudflare); err == nil {
		t.Error("an unconfigured provider produced a working client")
	}
}

func TestFor_BuildsEachSupportedProvider(t *testing.T) {
	credsEnv(t)
	for _, c := range []Credentials{
		{Provider: Cloudflare, APIToken: "t"},
		{Provider: DigitalOcean, APIToken: "t"},
		{Provider: Route53, AccessKeyID: "AKIA", SecretAccessKey: "s"},
	} {
		if err := SaveCredentials(c); err != nil {
			t.Fatal(err)
		}
		p, err := For(c.Provider)
		if err != nil {
			t.Fatalf("For(%s): %v", c.Provider, err)
		}
		if p.Name() != c.Provider {
			t.Errorf("provider reports %q, want %q", p.Name(), c.Provider)
		}
	}
}

// Route53 has no add-one-value operation: a change replaces the whole record
// set, so SetTXT reads what is there and writes it back with its own value
// added. That makes the read the dangerous step. If it fails and the failure is
// swallowed, the write goes out carrying only the new value and silently
// destroys a proof another order is still relying on, which surfaces later as
// an authorization failure with no cause anywhere near it.
func TestRoute53_RefusesToOverwriteWhenItCannotReadTheRecord(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/2013-04-01/hostedzone", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		io.WriteString(w, `<ListHostedZonesResponse><HostedZones>`+ //nolint:errcheck
			`<HostedZone><Id>/hostedzone/Z1</Id><Name>example.com.</Name></HostedZone>`+
			`</HostedZones></ListHostedZonesResponse>`)
	})
	changed := false
	mux.HandleFunc("/2013-04-01/hostedzone/Z1/rrset/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// A response servlo cannot parse: not an error status, but not
			// something it can conclude "the record is empty" from either.
			w.Header().Set("Content-Type", "text/xml")
			io.WriteString(w, `<<< not xml at all`) //nolint:errcheck
			return
		}
		changed = true
		io.WriteString(w, `<ChangeResourceRecordSetsResponse/>`) //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := &route53Provider{
		creds:   Credentials{Provider: Route53, AccessKeyID: "AKIA", SecretAccessKey: "s"},
		baseURL: srv.URL,
		client:  srv.Client(),
	}
	err := p.SetTXT(context.Background(), "_acme-challenge.example.com", "value-1")
	if err == nil {
		t.Fatal("an unreadable record set was treated as an empty one")
	}
	if changed {
		t.Error("the record was overwritten despite servlo not knowing what was in it")
	}
}

// An absent record is not a failure: it is the ordinary state before the first
// issuance, and reading it has to come back empty rather than error.
func TestRoute53_MissingRecordReadsAsEmpty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/2013-04-01/hostedzone", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		io.WriteString(w, `<ListHostedZonesResponse><HostedZones>`+ //nolint:errcheck
			`<HostedZone><Id>/hostedzone/Z1</Id><Name>example.com.</Name></HostedZone>`+
			`</HostedZones></ListHostedZonesResponse>`)
	})
	var body string
	mux.HandleFunc("/2013-04-01/hostedzone/Z1/rrset/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		if r.Method == http.MethodGet {
			io.WriteString(w, `<ListResourceRecordSetsResponse><ResourceRecordSets></ResourceRecordSets></ListResourceRecordSetsResponse>`) //nolint:errcheck
			return
		}
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		io.WriteString(w, `<ChangeResourceRecordSetsResponse/>`) //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := &route53Provider{
		creds:   Credentials{Provider: Route53, AccessKeyID: "AKIA", SecretAccessKey: "s"},
		baseURL: srv.URL,
		client:  srv.Client(),
	}
	if err := p.SetTXT(context.Background(), "_acme-challenge.example.com", "value-1"); err != nil {
		t.Fatalf("SetTXT on a fresh record: %v", err)
	}
	if !strings.Contains(body, "value-1") {
		t.Errorf("the change did not carry the new value: %s", body)
	}
}
