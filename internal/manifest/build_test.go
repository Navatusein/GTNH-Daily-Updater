package manifest

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuildNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tag     string
		want    int
		wantErr bool
	}{
		{tag: "daily-2026-10-27+803", want: 803},
		{tag: "daily-2026-10-28+1", want: 1},
		{tag: "daily-2026-10-27", wantErr: true},
		{tag: "daily-2026-10-27+", wantErr: true},
		{tag: "daily-2026-10-27+abc", wantErr: true},
		{tag: "daily-2026-10-27+0", wantErr: true},
		{tag: "", wantErr: true},
	}

	for _, tt := range tests {
		got, err := BuildNumber(tt.tag)
		if tt.wantErr {
			if err == nil {
				t.Errorf("BuildNumber(%q) expected error, got %d", tt.tag, got)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("BuildNumber(%q) = %d, %v; want %d", tt.tag, got, err, tt.want)
		}
	}
}

func zipWith(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFetchBuild(t *testing.T) {
	const tag = "daily-2026-10-27+803"
	manifestZip := zipWith(t, "daily.json",
		`{"version":"daily","last_updated":"2026-10-27T11:00:00","config":"2.9.0-nightly-2026-10-24-02",`+
			`"github_mods":{"GT5-Unofficial":{"version":"5.09.54.238","side":"BOTH"}},"external_mods":{}}`)

	var gotPath, gotAuth string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/releases/tags/", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[`+
			`{"name":"GTNH-daily-2026-10-27+803-server-java17-26.zip","browser_download_url":"%s/server.zip"},`+
			`{"name":"gtnh-daily-2026-10-27+803-manifest.json.zip","browser_download_url":"%s/manifest.zip"}]}`,
			tag, srv.URL, srv.URL)
	})
	mux.HandleFunc("/manifest.zip", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("asset download must not send the token")
		}
		w.Write(manifestZip)
	})

	old := dailyBuildsReleaseURL
	dailyBuildsReleaseURL = srv.URL + "/releases/tags/"
	defer func() { dailyBuildsReleaseURL = old }()

	m, err := FetchBuild(context.Background(), tag, "secret")
	if err != nil {
		t.Fatalf("FetchBuild: %v", err)
	}
	if gotPath != "/releases/tags/daily-2026-10-27%2B803" {
		t.Errorf("release path = %q, want + encoded", gotPath)
	}
	if gotAuth != "token secret" {
		t.Errorf("Authorization = %q, want token header", gotAuth)
	}
	if m.Config != "2.9.0-nightly-2026-10-24-02" {
		t.Errorf("Config = %q", m.Config)
	}
	if got := m.AllMods()["GT5-Unofficial"].Version; got != "5.09.54.238" {
		t.Errorf("GT5-Unofficial version = %q", got)
	}
}

func TestFetchBuildErrors(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/releases/tags/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() == "/releases/tags/daily-2026-01-01%2B1" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"tag_name":"x","assets":[{"name":"server.zip","browser_download_url":"x"}]}`)
	})

	old := dailyBuildsReleaseURL
	dailyBuildsReleaseURL = srv.URL + "/releases/tags/"
	defer func() { dailyBuildsReleaseURL = old }()

	for _, tag := range []string{"no-number", "daily-2026-01-01+1", "daily-2026-01-02+2"} {
		if _, err := FetchBuild(context.Background(), tag, ""); err == nil {
			t.Errorf("FetchBuild(%q) expected error", tag)
		}
	}
}

func TestParseZippedManifestWithoutJSON(t *testing.T) {
	if _, err := parseZippedManifest(zipWith(t, "readme.txt", "x")); err == nil {
		t.Fatal("expected error for zip without .json")
	}
	if _, err := parseZippedManifest([]byte("not a zip")); err == nil {
		t.Fatal("expected error for non-zip data")
	}
}
