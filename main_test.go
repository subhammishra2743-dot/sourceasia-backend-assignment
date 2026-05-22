package main

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
)

func testMux() http.Handler {
    s := newStore()
    mux := http.NewServeMux()
    mux.HandleFunc("/request", s.handleRequest)
    mux.HandleFunc("/stats", s.handleStats)
    mux.HandleFunc("/products", s.handleProducts)
    mux.HandleFunc("/products/", s.handleProductByID)
    return mux
}

func perform(r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
    var buf bytes.Buffer
    if body != nil { _ = json.NewEncoder(&buf).Encode(body) }
    req := httptest.NewRequest(method, path, &buf)
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)
    return w
}

func TestRateLimitFivePerMinute(t *testing.T) {
    r := testMux()
    body := map[string]any{"user_id":"u1", "payload":map[string]any{"x":1}}
    for i := 0; i < 5; i++ {
        if w := perform(r, http.MethodPost, "/request", body); w.Code != http.StatusCreated {
            t.Fatalf("request %d got %d body %s", i+1, w.Code, w.Body.String())
        }
    }
    if w := perform(r, http.MethodPost, "/request", body); w.Code != http.StatusTooManyRequests {
        t.Fatalf("expected 429, got %d body %s", w.Code, w.Body.String())
    }
}

func TestCreateListDetailAndAddMedia(t *testing.T) {
    r := testMux()
    create := map[string]any{
        "name":"Widget A",
        "sku":"SKU-001",
        "image_urls":[]string{"https://cdn.example.com/products/sku-001/img-1.jpg"},
        "video_urls":[]string{"https://cdn.example.com/products/sku-001/demo.mp4"},
    }
    w := perform(r, http.MethodPost, "/products", create)
    if w.Code != http.StatusCreated { t.Fatalf("expected 201, got %d %s", w.Code, w.Body.String()) }

    w = perform(r, http.MethodGet, "/products?limit=20&offset=0", nil)
    if w.Code != http.StatusOK { t.Fatalf("expected 200, got %d", w.Code) }
    if bytes.Contains(w.Body.Bytes(), []byte("image_urls")) || bytes.Contains(w.Body.Bytes(), []byte("video_urls")) {
        t.Fatalf("list response must not include full media arrays: %s", w.Body.String())
    }

    w = perform(r, http.MethodGet, "/products/1", nil)
    if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte("image_urls")) {
        t.Fatalf("detail should include media arrays, got %d %s", w.Code, w.Body.String())
    }

    media := map[string]any{"image_urls":[]string{"https://cdn.example.com/products/sku-001/img-2.jpg"}}
    w = perform(r, http.MethodPost, "/products/1/media", media)
    if w.Code != http.StatusOK { t.Fatalf("expected 200, got %d %s", w.Code, w.Body.String()) }
}

func TestDuplicateSKU(t *testing.T) {
    r := testMux()
    body := map[string]any{"name":"Widget", "sku":"DUP-001"}
    if w := perform(r, http.MethodPost, "/products", body); w.Code != http.StatusCreated { t.Fatalf("expected 201") }
    if w := perform(r, http.MethodPost, "/products", body); w.Code != http.StatusConflict { t.Fatalf("expected 409 got %d", w.Code) }
}
