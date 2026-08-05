package dnsprovider

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Route53, and why it is signed by hand.
//
// The AWS SDK would bring dozens of modules and a large dependency surface into
// a binary that needs exactly two API calls. SigV4 is a documented algorithm in
// about a hundred lines, and writing it here keeps the whole provider auditable
// in one file. Route53 is also the reason the Provider interface exists at all:
// it is signed rather than bearer-authenticated, so it could never share a code
// path with the other two.

const route53API = "https://route53.amazonaws.com"

// route53Region is fixed. Route53 is a global service whose endpoint always
// signs against us-east-1 regardless of where the operator's other resources
// live, and signing against anything else is rejected.
const route53Region = "us-east-1"

const route53Service = "route53"

type route53Provider struct {
	creds   Credentials
	baseURL string
	client  *http.Client
	// now is a seam so the signature can be checked against a fixed timestamp.
	now func() time.Time
}

func (r *route53Provider) Name() Name { return Route53 }

func (r *route53Provider) timestamp() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now().UTC()
}

func (r *route53Provider) do(ctx context.Context, method, path, body string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, r.baseURL+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/xml")
	if err := r.sign(req, body); err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("route53 %s %s: status %d: %s", method, path, resp.StatusCode, route53ErrText(data))
	}
	return data, nil
}

// route53ErrText pulls the message out of an AWS error document, falling back
// to the raw body when it is not the shape we expect.
func route53ErrText(body []byte) string {
	var doc struct {
		Error struct {
			Code    string `xml:"Code"`
			Message string `xml:"Message"`
		} `xml:"Error"`
	}
	if err := xml.Unmarshal(body, &doc); err == nil && doc.Error.Message != "" {
		return doc.Error.Code + ": " + doc.Error.Message
	}
	return strings.TrimSpace(string(body))
}

// sign applies AWS Signature Version 4 to req.
func (r *route53Provider) sign(req *http.Request, body string) error {
	now := r.timestamp().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("Host", req.URL.Host)

	// Canonical request. The header set that is signed has to be sorted and
	// lowercased, and listed in the same order in the signed-headers string, or
	// AWS computes a different hash and rejects the request.
	signedHeaders, canonicalHeaders := canonicalHeaders(req)
	payloadHash := sha256Hex(body)
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURIPath(req.URL),
		canonicalQuery(req.URL),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := strings.Join([]string{dateStamp, route53Region, route53Service, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex(canonicalRequest),
	}, "\n")

	key := hmacSHA256([]byte("AWS4"+r.creds.SecretAccessKey), dateStamp)
	key = hmacSHA256(key, route53Region)
	key = hmacSHA256(key, route53Service)
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		r.creds.AccessKeyID, scope, signedHeaders, signature))
	return nil
}

func canonicalHeaders(req *http.Request) (signedHeaders, canonical string) {
	names := []string{"host", "x-amz-date"}
	if req.Header.Get("Content-Type") != "" {
		names = append(names, "content-type")
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		value := req.Header.Get(name)
		if name == "host" {
			value = req.URL.Host
		}
		b.WriteString(name + ":" + strings.TrimSpace(value) + "\n")
	}
	return strings.Join(names, ";"), b.String()
}

func canonicalURIPath(u *url.URL) string {
	if u.Path == "" {
		return "/"
	}
	return u.EscapedPath()
}

func canonicalQuery(u *url.URL) string {
	values := u.Query()
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := values[k]
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data)) //nolint:errcheck — hash writes never fail
	return h.Sum(nil)
}

func (r *route53Provider) zoneID(ctx context.Context, record string) (id, zone string, err error) {
	body, err := r.do(ctx, http.MethodGet, "/2013-04-01/hostedzone", "")
	if err != nil {
		return "", "", err
	}
	var doc struct {
		Zones []struct {
			ID   string `xml:"Id"`
			Name string `xml:"Name"`
		} `xml:"HostedZones>HostedZone"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		return "", "", fmt.Errorf("route53: unreadable hosted zone list: %w", err)
	}
	names := make([]string, 0, len(doc.Zones))
	for _, z := range doc.Zones {
		names = append(names, z.Name)
	}
	zone, err = longestZoneSuffix(record, names)
	if err != nil {
		return "", "", err
	}
	for _, z := range doc.Zones {
		if strings.TrimSuffix(z.Name, ".") == zone {
			return strings.TrimPrefix(z.ID, "/hostedzone/"), zone, nil
		}
	}
	return "", "", fmt.Errorf("route53 returned zone %s without an id", zone)
}

// changeTXT issues one UPSERT or DELETE. Route53 has no add-one-value
// operation: a change replaces the whole record set, so the caller passes every
// value that should be there afterwards.
func (r *route53Provider) changeTXT(ctx context.Context, action, record string, values []string) error {
	zoneID, _, err := r.zoneID(ctx, record)
	if err != nil {
		return err
	}
	var records strings.Builder
	for _, v := range values {
		// TXT values reach Route53 quoted; an unquoted one is rejected outright.
		records.WriteString("<ResourceRecord><Value>" + xmlEscape(`"`+v+`"`) + "</Value></ResourceRecord>")
	}
	body := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<ChangeResourceRecordSetsRequest xmlns="https://route53.amazonaws.com/doc/2013-04-01/">` +
		`<ChangeBatch><Changes><Change>` +
		`<Action>` + action + `</Action>` +
		`<ResourceRecordSet>` +
		`<Name>` + xmlEscape(record) + `</Name>` +
		`<Type>TXT</Type><TTL>60</TTL>` +
		`<ResourceRecords>` + records.String() + `</ResourceRecords>` +
		`</ResourceRecordSet></Change></Changes></ChangeBatch>` +
		`</ChangeResourceRecordSetsRequest>`

	_, err = r.do(ctx, http.MethodPost, "/2013-04-01/hostedzone/"+zoneID+"/rrset/", body)
	return err
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(s)) //nolint:errcheck
	return b.String()
}

// existingTXT reads the values already at a record name, so a change can keep
// the ones another in-flight order still needs.
func (r *route53Provider) existingTXT(ctx context.Context, record string) ([]string, error) {
	zoneID, _, err := r.zoneID(ctx, record)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("name", record)
	query.Set("type", "TXT")
	query.Set("maxitems", "1")
	body, err := r.do(ctx, http.MethodGet, "/2013-04-01/hostedzone/"+zoneID+"/rrset/?"+query.Encode(), "")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Sets []struct {
			Name   string   `xml:"Name"`
			Type   string   `xml:"Type"`
			Values []string `xml:"ResourceRecords>ResourceRecord>Value"`
		} `xml:"ResourceRecordSets>ResourceRecordSet"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		// Not treated as "the record is empty". A change replaces the whole
		// record set, so proceeding on a guess here would write out only the
		// new value and silently destroy a proof another order still needs.
		return nil, fmt.Errorf("route53: unreadable record set for %s: %w", record, err)
	}
	for _, set := range doc.Sets {
		if strings.TrimSuffix(set.Name, ".") != record || set.Type != "TXT" {
			continue
		}
		var out []string
		for _, v := range set.Values {
			out = append(out, strings.Trim(v, `"`))
		}
		return out, nil
	}
	return nil, nil
}

// SetTXT reads the record, adds this value and writes the set back, because
// Route53 has no add-one-value operation. The read is the step that matters: an
// error there means servlo does not know what is at the record, and writing
// anyway would replace another order's proof with this one's.
//
// The read and the write are not atomic, so two issuances racing on the same
// record name could still lose one value. In practice they cannot: a wildcard
// and its base are the only pair sharing a record name, and they are published
// sequentially within one order that a per-domain lock already serialises.
func (r *route53Provider) SetTXT(ctx context.Context, record, value string) error {
	existing, err := r.existingTXT(ctx, record)
	if err != nil {
		return err
	}
	values := append(dropValue(existing, value), value)
	return r.changeTXT(ctx, "UPSERT", record, values)
}

func (r *route53Provider) RemoveTXT(ctx context.Context, record, value string) error {
	existing, err := r.existingTXT(ctx, record)
	if err != nil {
		return err
	}
	remaining := dropValue(existing, value)
	if len(remaining) == 0 {
		// Route53 refuses a DELETE that does not match the record set exactly,
		// so the delete carries the value being removed.
		if len(existing) == 0 {
			return nil
		}
		return r.changeTXT(ctx, "DELETE", record, []string{value})
	}
	return r.changeTXT(ctx, "UPSERT", record, remaining)
}

func dropValue(values []string, drop string) []string {
	var out []string
	for _, v := range values {
		if v != drop {
			out = append(out, v)
		}
	}
	return out
}
