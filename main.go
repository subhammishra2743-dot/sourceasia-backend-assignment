package main

import (
    "encoding/json"
    "errors"
    "net/http"
    "net/url"
    "sort"
    "strconv"
    "strings"
    "sync"
    "time"
)

const (
    rateLimitPerMinute = 5
    maxURLsPerRequest  = 20
    maxURLLength       = 2048
    defaultLimit       = 20
    maxLimit           = 100
)

type appStore struct {
    mu       sync.RWMutex
    users    map[string]*userStats
    products map[int64]*Product
    skuIndex map[string]int64
    nextID   int64
}

type userStats struct {
    windowStart time.Time
    accepted    int
    rejected    int
}

type requestBody struct {
    UserID  string          `json:"user_id"`
    Payload json.RawMessage `json:"payload"`
}

type Product struct {
    ID        int64     `json:"id"`
    Name      string    `json:"name"`
    SKU       string    `json:"sku"`
    ImageURLs []string  `json:"image_urls"`
    VideoURLs []string  `json:"video_urls"`
    CreatedAt time.Time `json:"created_at"`
}

type createProductRequest struct {
    Name      string   `json:"name"`
    SKU       string   `json:"sku"`
    ImageURLs []string `json:"image_urls"`
    VideoURLs []string `json:"video_urls"`
}

type addMediaRequest struct {
    ImageURLs []string `json:"image_urls"`
    VideoURLs []string `json:"video_urls"`
}

type productListItem struct {
    ID           int64     `json:"id"`
    Name         string    `json:"name"`
    SKU          string    `json:"sku"`
    ImageCount   int       `json:"image_count"`
    VideoCount   int       `json:"video_count"`
    ThumbnailURL string    `json:"thumbnail_url,omitempty"`
    CreatedAt    time.Time `json:"created_at"`
}

type listProductsResponse struct {
    Limit    int               `json:"limit"`
    Offset   int               `json:"offset"`
    Total    int               `json:"total"`
    Products []productListItem `json:"products"`
}

func main() {
    store := newStore()
    mux := http.NewServeMux()
    mux.HandleFunc("/request", store.handleRequest)
    mux.HandleFunc("/stats", store.handleStats)
    mux.HandleFunc("/products", store.handleProducts)
    mux.HandleFunc("/products/", store.handleProductByID)
    _ = http.ListenAndServe(":8080", mux)
}

func newStore() *appStore {
    return &appStore{users: map[string]*userStats{}, products: map[int64]*Product{}, skuIndex: map[string]int64{}, nextID: 1}
}

func (s *appStore) handleProducts(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodPost:
        s.handleCreateProduct(w, r)
    case http.MethodGet:
        s.handleListProducts(w, r)
    default:
        writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error":"method not allowed"})
    }
}

func (s *appStore) handleProductByID(w http.ResponseWriter, r *http.Request) {
    path := strings.TrimPrefix(r.URL.Path, "/products/")
    parts := strings.Split(strings.Trim(path, "/"), "/")
    if len(parts) == 0 || parts[0] == "" {
        writeJSON(w, http.StatusNotFound, map[string]string{"error":"not found"})
        return
    }
    id, err := strconv.ParseInt(parts[0], 10, 64)
    if err != nil || id <= 0 {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error":"invalid product id"})
        return
    }
    if len(parts) == 1 && r.Method == http.MethodGet {
        s.handleGetProduct(w, id)
        return
    }
    if len(parts) == 2 && parts[1] == "media" && r.Method == http.MethodPost {
        s.handleAddMedia(w, r, id)
        return
    }
    writeJSON(w, http.StatusNotFound, map[string]string{"error":"not found"})
}

func (s *appStore) handleRequest(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error":"method not allowed"})
        return
    }
    var req requestBody
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error":"invalid JSON body"})
        return
    }
    req.UserID = strings.TrimSpace(req.UserID)
    if req.UserID == "" {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error":"user_id is required and cannot be empty"})
        return
    }
    if len(req.Payload) == 0 || string(req.Payload) == "null" {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error":"payload is required"})
        return
    }

    now := time.Now().UTC()
    s.mu.Lock()
    st, ok := s.users[req.UserID]
    if !ok {
        st = &userStats{windowStart: now}
        s.users[req.UserID] = st
    }
    if now.Sub(st.windowStart) >= time.Minute {
        st.windowStart = now
        st.accepted = 0
    }
    if st.accepted >= rateLimitPerMinute {
        st.rejected++
        resp := map[string]any{"error":"rate limit exceeded: maximum 5 accepted requests per user per fixed 1-minute window", "user_id":req.UserID, "accepted_current_window":st.accepted, "rejected_cumulative":st.rejected, "window_start":st.windowStart}
        s.mu.Unlock()
        writeJSON(w, http.StatusTooManyRequests, resp)
        return
    }
    st.accepted++
    resp := map[string]any{"message":"request accepted", "user_id":req.UserID, "accepted_current_window":st.accepted, "window_start":st.windowStart}
    s.mu.Unlock()
    writeJSON(w, http.StatusCreated, resp)
}

func (s *appStore) handleStats(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error":"method not allowed"})
        return
    }
    now := time.Now().UTC()
    s.mu.RLock()
    users := make(map[string]map[string]any, len(s.users))
    for userID, st := range s.users {
        accepted := st.accepted
        if now.Sub(st.windowStart) >= time.Minute { accepted = 0 }
        users[userID] = map[string]any{"accepted_current_window":accepted, "rejected_cumulative":st.rejected, "window_start":st.windowStart}
    }
    s.mu.RUnlock()
    writeJSON(w, http.StatusOK, map[string]any{"users":users})
}

func (s *appStore) handleCreateProduct(w http.ResponseWriter, r *http.Request) {
    var req createProductRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error":"invalid JSON body"})
        return
    }
    req.Name = strings.TrimSpace(req.Name)
    req.SKU = strings.TrimSpace(req.SKU)
    if req.Name == "" || req.SKU == "" {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error":"name and sku are required and cannot be empty"})
        return
    }
    if err := validateURLs(req.ImageURLs, req.VideoURLs, false); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error":err.Error()})
        return
    }
    s.mu.Lock()
    if _, exists := s.skuIndex[req.SKU]; exists {
        s.mu.Unlock()
        writeJSON(w, http.StatusConflict, map[string]string{"error":"duplicate sku"})
        return
    }
    p := &Product{ID:s.nextID, Name:req.Name, SKU:req.SKU, ImageURLs:copyStrings(req.ImageURLs), VideoURLs:copyStrings(req.VideoURLs), CreatedAt:time.Now().UTC()}
    s.products[p.ID] = p
    s.skuIndex[p.SKU] = p.ID
    s.nextID++
    resp := cloneProduct(p)
    s.mu.Unlock()
    writeJSON(w, http.StatusCreated, resp)
}

func (s *appStore) handleListProducts(w http.ResponseWriter, r *http.Request) {
    limit := parseInt(r.URL.Query().Get("limit"), defaultLimit)
    offset := parseInt(r.URL.Query().Get("offset"), 0)
    if limit < 1 { limit = defaultLimit }
    if limit > maxLimit { limit = maxLimit }
    if offset < 0 { offset = 0 }

    s.mu.RLock()
    ids := make([]int64, 0, len(s.products))
    for id := range s.products { ids = append(ids, id) }
    sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
    total := len(ids)
    if offset > total { offset = total }
    end := offset + limit
    if end > total { end = total }
    items := make([]productListItem, 0, end-offset)
    for _, id := range ids[offset:end] {
        p := s.products[id]
        item := productListItem{ID:p.ID, Name:p.Name, SKU:p.SKU, ImageCount:len(p.ImageURLs), VideoCount:len(p.VideoURLs), CreatedAt:p.CreatedAt}
        if len(p.ImageURLs) > 0 { item.ThumbnailURL = p.ImageURLs[0] }
        items = append(items, item)
    }
    s.mu.RUnlock()
    writeJSON(w, http.StatusOK, listProductsResponse{Limit:limit, Offset:offset, Total:total, Products:items})
}

func (s *appStore) handleGetProduct(w http.ResponseWriter, id int64) {
    s.mu.RLock()
    p, exists := s.products[id]
    if !exists {
        s.mu.RUnlock()
        writeJSON(w, http.StatusNotFound, map[string]string{"error":"product not found"})
        return
    }
    resp := cloneProduct(p)
    s.mu.RUnlock()
    writeJSON(w, http.StatusOK, resp)
}

func (s *appStore) handleAddMedia(w http.ResponseWriter, r *http.Request, id int64) {
    var req addMediaRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error":"invalid JSON body"})
        return
    }
    if err := validateURLs(req.ImageURLs, req.VideoURLs, true); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error":err.Error()})
        return
    }
    s.mu.Lock()
    p, exists := s.products[id]
    if !exists {
        s.mu.Unlock()
        writeJSON(w, http.StatusNotFound, map[string]string{"error":"product not found"})
        return
    }
    p.ImageURLs = append(p.ImageURLs, req.ImageURLs...)
    p.VideoURLs = append(p.VideoURLs, req.VideoURLs...)
    resp := cloneProduct(p)
    s.mu.Unlock()
    writeJSON(w, http.StatusOK, resp)
}

func validateURLs(imageURLs, videoURLs []string, requireAtLeastOne bool) error {
    if requireAtLeastOne && len(imageURLs)+len(videoURLs) == 0 { return errors.New("at least one of image_urls or video_urls is required") }
    if len(imageURLs) > maxURLsPerRequest || len(videoURLs) > maxURLsPerRequest { return errors.New("maximum 20 URLs are allowed per array per request") }
    all := append(copyStrings(imageURLs), videoURLs...)
    for _, raw := range all {
        raw = strings.TrimSpace(raw)
        if raw == "" || len(raw) > maxURLLength { return errors.New("URLs must be non-empty and at most 2048 characters") }
        parsed, err := url.ParseRequestURI(raw)
        if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" { return errors.New("URLs must be valid http:// or https:// URLs") }
    }
    return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(v)
}

func parseInt(value string, fallback int) int {
    if value == "" { return fallback }
    n, err := strconv.Atoi(value)
    if err != nil { return fallback }
    return n
}

func copyStrings(in []string) []string {
    if in == nil { return nil }
    out := make([]string, len(in))
    copy(out, in)
    return out
}

func cloneProduct(p *Product) Product {
    return Product{ID:p.ID, Name:p.Name, SKU:p.SKU, ImageURLs:copyStrings(p.ImageURLs), VideoURLs:copyStrings(p.VideoURLs), CreatedAt:p.CreatedAt}
}
