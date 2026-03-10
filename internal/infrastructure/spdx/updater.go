package spdx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const UpstreamURL = "https://raw.githubusercontent.com/spdx/license-list-data/refs/heads/main/json/licenses.json"

// FetchLatest retrieves the latest SPDX licenses.json from upstream.
func FetchLatest(ctx context.Context, client *http.Client) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, UpstreamURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get spdx: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if len(b) < 2048 { // basic sanity check
		return nil, errors.New("spdx payload too small")
	}
	return b, nil
}

type minimal struct {
	LicenseListVersion string `json:"licenseListVersion"`
	Licenses           []struct {
		LicenseID string `json:"licenseId"`
	} `json:"licenses"`
}

// ValidatePayload does a light structural validation and returns the list version.
func ValidatePayload(data []byte) (string, error) {
	var m minimal
	if err := json.Unmarshal(data, &m); err != nil {
		return "", fmt.Errorf("unmarshal: %w", err)
	}
	if m.LicenseListVersion == "" || len(m.Licenses) == 0 {
		return "", errors.New("missing version or licenses")
	}
	return m.LicenseListVersion, nil
}

// WriteAtomic writes file atomically.
func WriteAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	return os.Rename(tmp, path)
}
