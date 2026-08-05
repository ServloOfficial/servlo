package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const cloudflareAPI = "https://api.cloudflare.com/client/v4"

type cloudflareProvider struct {
	token   string
	baseURL string
	client  *http.Client
}

func (c *cloudflareProvider) Name() Name { return Cloudflare }

// cloudflareEnvelope is the shape every Cloudflare response shares. The API
// reports failure in the body with a 200 status, so the status code alone is
// not a verdict: treating one as success would leave the challenge unpublished
// while servlo waits for a validation that can never happen.
type cloudflareEnvelope struct {
	Success bool              `json:"success"`
	Errors  []cloudflareError `json:"errors"`
	Result  json.RawMessage   `json:"result"`
}

type cloudflareError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *cloudflareProvider) do(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	var env cloudflareEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("cloudflare %s %s: unreadable response (status %d): %w", method, path, resp.StatusCode, err)
	}
	if !env.Success {
		return nil, fmt.Errorf("cloudflare %s %s: %s", method, path, cloudflareErrText(env.Errors, resp.StatusCode))
	}
	return env.Result, nil
}

func cloudflareErrText(errs []cloudflareError, status int) string {
	if len(errs) == 0 {
		return fmt.Sprintf("request refused (status %d)", status)
	}
	var parts []string
	for _, e := range errs {
		parts = append(parts, fmt.Sprintf("%s (code %d)", e.Message, e.Code))
	}
	return strings.Join(parts, "; ")
}

func (c *cloudflareProvider) zoneID(ctx context.Context, record string) (string, error) {
	raw, err := c.do(ctx, http.MethodGet, "/zones?per_page=200", nil)
	if err != nil {
		return "", err
	}
	var zones []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &zones); err != nil {
		return "", err
	}
	names := make([]string, 0, len(zones))
	for _, z := range zones {
		names = append(names, z.Name)
	}
	zone, err := longestZoneSuffix(record, names)
	if err != nil {
		return "", err
	}
	for _, z := range zones {
		if z.Name == zone {
			return z.ID, nil
		}
	}
	return "", fmt.Errorf("cloudflare returned zone %s without an id", zone)
}

func (c *cloudflareProvider) SetTXT(ctx context.Context, record, value string) error {
	zoneID, err := c.zoneID(ctx, record)
	if err != nil {
		return err
	}
	// A new record rather than an update: a wildcard and its base share this
	// name and both values have to be present at once.
	_, err = c.do(ctx, http.MethodPost, "/zones/"+zoneID+"/dns_records", map[string]any{
		"type": "TXT", "name": record, "content": value, "ttl": 60,
	})
	return err
}

func (c *cloudflareProvider) RemoveTXT(ctx context.Context, record, value string) error {
	zoneID, err := c.zoneID(ctx, record)
	if err != nil {
		return err
	}
	query := url.Values{}
	query.Set("type", "TXT")
	query.Set("name", record)
	raw, err := c.do(ctx, http.MethodGet, "/zones/"+zoneID+"/dns_records?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	var records []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &records); err != nil {
		return err
	}
	for _, r := range records {
		// Only the value this issuance published. Another order in flight for
		// the same name has its own value there and still needs it.
		if r.Name == record && r.Content == value {
			if _, err := c.do(ctx, http.MethodDelete, "/zones/"+zoneID+"/dns_records/"+r.ID, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
