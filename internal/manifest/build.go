package manifest

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// DailyBuildsRepo publishes every daily build as a release whose tag looks like
// "daily-2026-10-27+803" and carries the build manifest as a zipped asset.
const DailyBuildsRepo = "GTNewHorizons/GTNH-Daily-Builds"

// dailyBuildsReleaseURL is a var so tests can point it at a local server.
var dailyBuildsReleaseURL = "https://api.github.com/repos/" + DailyBuildsRepo + "/releases/tags/"

const buildManifestSuffix = "-manifest.json.zip"

// BuildNumber returns the build counter from a GTNH-Daily-Builds tag,
// e.g. 803 for "daily-2026-10-27+803".
func BuildNumber(tag string) (int, error) {
	i := strings.LastIndex(tag, "+")
	if i < 0 {
		return 0, fmt.Errorf("build tag %q has no +<number> suffix", tag)
	}
	n, err := strconv.Atoi(tag[i+1:])
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("build tag %q has no valid build number", tag)
	}
	return n, nil
}

// FetchBuild downloads the manifest of a specific daily build from GTNH-Daily-Builds.
func FetchBuild(ctx context.Context, tag, token string) (*DailyManifest, error) {
	tag = strings.TrimSpace(tag)
	if _, err := BuildNumber(tag); err != nil {
		return nil, err
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	// "+" is legal in a path but some proxies read it as a space, so encode it.
	data, err := get(ctx, dailyBuildsReleaseURL+strings.ReplaceAll(url.PathEscape(tag), "+", "%2B"), token)
	if err != nil {
		return nil, fmt.Errorf("fetching daily build %s: %w", tag, err)
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return nil, fmt.Errorf("parsing daily build %s release: %w", tag, err)
	}

	assetURL := ""
	for _, a := range release.Assets {
		if strings.HasSuffix(a.Name, buildManifestSuffix) {
			assetURL = a.BrowserDownloadURL
			break
		}
	}
	if assetURL == "" {
		return nil, fmt.Errorf("daily build %s has no %s asset", tag, buildManifestSuffix)
	}

	// Asset downloads go through a CDN redirect and need no token.
	archive, err := get(ctx, assetURL, "")
	if err != nil {
		return nil, fmt.Errorf("downloading manifest of daily build %s: %w", tag, err)
	}
	m, err := parseZippedManifest(archive)
	if err != nil {
		return nil, fmt.Errorf("daily build %s: %w", tag, err)
	}
	return m, nil
}

// parseZippedManifest reads the first .json file of a manifest zip.
func parseZippedManifest(archive []byte) (*DailyManifest, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("opening manifest zip: %w", err)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || path.Ext(f.Name) != ".json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("opening %s: %w", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", f.Name, err)
		}
		var m DailyManifest
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", f.Name, err)
		}
		return &m, nil
	}
	return nil, fmt.Errorf("manifest zip has no .json file")
}

func get(ctx context.Context, u, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
