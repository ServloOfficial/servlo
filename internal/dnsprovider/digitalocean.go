package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

const digitalOceanAPI = "https://api.digitalocean.com"

type digitalOceanProvider struct {
	token   string
	baseURL string
	client  *http.Client
}

func (d *digitalOceanProvider) Name() Name { return DigitalOcean }

func (d *digitalOceanProvider) do(ctx context.Context, method, path string, body any, out any) error {
	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, d.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode >= 300 {
		// The body carries the reason; a bare status leaves an operator
		// guessing between a wrong token and a domain on another account.
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("digitalocean %s %s: status %d: %s", method, path, resp.StatusCode, bytes.TrimSpace(detail))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (d *digitalOceanProvider) zone(ctx context.Context, record string) (string, error) {
	var page struct {
		Domains []struct {
			Name string `json:"name"`
		} `json:"domains"`
	}
	if err := d.do(ctx, http.MethodGet, "/v2/domains?per_page=200", nil, &page); err != nil {
		return "", err
	}
	names := make([]string, 0, len(page.Domains))
	for _, dom := range page.Domains {
		names = append(names, dom.Name)
	}
	return longestZoneSuffix(record, names)
}

func (d *digitalOceanProvider) SetTXT(ctx context.Context, record, value string) error {
	zone, err := d.zone(ctx, record)
	if err != nil {
		return err
	}
	// DigitalOcean names records relative to the zone. Sending the fully
	// qualified name creates _acme-challenge.example.com.example.com, which
	// validates against nothing and is invisible until the order fails.
	return d.do(ctx, http.MethodPost, "/v2/domains/"+zone+"/records", map[string]any{
		"type": "TXT", "name": relativeName(record, zone), "data": value, "ttl": 60,
	}, nil)
}

func (d *digitalOceanProvider) RemoveTXT(ctx context.Context, record, value string) error {
	zone, err := d.zone(ctx, record)
	if err != nil {
		return err
	}
	var page struct {
		Records []struct {
			ID   int    `json:"id"`
			Type string `json:"type"`
			Name string `json:"name"`
			Data string `json:"data"`
		} `json:"domain_records"`
	}
	if err := d.do(ctx, http.MethodGet, "/v2/domains/"+zone+"/records?per_page=200", nil, &page); err != nil {
		return err
	}
	want := relativeName(record, zone)
	for _, r := range page.Records {
		if r.Type == "TXT" && r.Name == want && r.Data == value {
			if err := d.do(ctx, http.MethodDelete, "/v2/domains/"+zone+"/records/"+strconv.Itoa(r.ID), nil, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
