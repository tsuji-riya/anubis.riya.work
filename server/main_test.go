package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "app.wasm"), []byte{0x00, 0x61, 0x73, 0x6d}, 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return &fileServer{root: root}
}

func request(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestServesIndexWithNoStore(t *testing.T) {
	rec := request(t, setup(t), http.MethodGet, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
}

func TestWasmContentType(t *testing.T) {
	rec := request(t, setup(t), http.MethodGet, "/sub/app.wasm")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/wasm" {
		t.Errorf("Content-Type = %q", got)
	}
}

func TestNoDirectoryListing(t *testing.T) {
	rec := request(t, setup(t), http.MethodGet, "/sub/")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (no listing, no index.html)", rec.Code)
	}
}

func TestPathTraversalBlocked(t *testing.T) {
	for _, target := range []string{"/../go.mod", "/%2e%2e/go.mod", "/sub/../../go.mod"} {
		rec := request(t, setup(t), http.MethodGet, target)
		if rec.Code == http.StatusOK {
			t.Errorf("%s: traversal succeeded", target)
		}
	}
}

func TestMethodNotAllowed(t *testing.T) {
	rec := request(t, setup(t), http.MethodPost, "/")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHead(t *testing.T) {
	rec := request(t, setup(t), http.MethodHead, "/")
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("status = %d body = %d bytes", rec.Code, rec.Body.Len())
	}
}
