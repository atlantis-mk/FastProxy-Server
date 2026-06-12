package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestWebAppHandlerServesAssetsAndSPAFallback(t *testing.T) {
	handler := newWebAppHandler(fstest.MapFS{
		"index.html":    {Data: []byte("<html>FastProxy</html>")},
		"assets/app.js": {Data: []byte("console.log('fastproxy')")},
	})

	assetRequest := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	assetResponse := httptest.NewRecorder()
	handler.ServeHTTP(assetResponse, assetRequest)
	if assetResponse.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want 200", assetResponse.Code)
	}
	if got := assetResponse.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("asset cache-control = %q, want immutable", got)
	}

	spaRequest := httptest.NewRequest(http.MethodGet, "/settings/backend", nil)
	spaResponse := httptest.NewRecorder()
	handler.ServeHTTP(spaResponse, spaRequest)
	if spaResponse.Code != http.StatusOK {
		t.Fatalf("spa status = %d, want 200", spaResponse.Code)
	}
	body, err := io.ReadAll(spaResponse.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !strings.Contains(string(body), "FastProxy") {
		t.Fatalf("spa body = %q, want index.html", body)
	}
}

func TestWebAppHandlerDoesNotFallbackAPIPaths(t *testing.T) {
	handler := newWebAppHandler(fstest.MapFS{
		"index.html": {Data: []byte("<html>FastProxy</html>")},
	})

	request := httptest.NewRequest(http.MethodGet, "/api/missing", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("api status = %d, want 404", response.Code)
	}
	if strings.Contains(response.Body.String(), "FastProxy") {
		t.Fatal("api path should not serve SPA index")
	}
}
