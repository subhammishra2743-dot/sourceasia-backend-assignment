<<<<<<< HEAD
# sourceasia-backend-assignment
Backend assignment in Golang including rate limiting and product catalog APIs.
=======
# Source Asia Backend Assignment

This repository contains one runnable Go HTTP service for both required parts:

1. Rate-limited API
2. Product catalog with media URLs

The service uses in-memory storage only. No database, CDN, file upload, binary body, or base64 media handling is used.

## Tech stack

- Go 1.22
- Standard library `net/http`
- In-memory maps protected by `sync.RWMutex`

## Run

```bash
go mod tidy
go run main.go
```

Server starts on:

```text
http://localhost:8080
```

## Part 1: Rate-limited API

### Rate limit approach

This implementation uses a **fixed 1-minute window** per `user_id`.

Each user can have maximum **5 accepted requests per minute**. The store is protected by a mutex, so parallel requests for the same user cannot bypass the limit.

### POST /request

Request:

```bash
curl -X POST http://localhost:8080/request \
  -H "Content-Type: application/json" \
  -d '{"user_id":"user-1","payload":{"message":"hello"}}'
```

Success response: `201 Created`

```json
{
  "message": "request accepted",
  "user_id": "user-1",
  "accepted_current_window": 1,
  "window_start": "2026-05-21T10:00:00Z"
}
```

Invalid input returns `400 Bad Request`.

When the rate limit is exceeded, response is `429 Too Many Requests`:

```json
{
  "error": "rate limit exceeded: maximum 5 accepted requests per user per fixed 1-minute window",
  "user_id": "user-1",
  "accepted_current_window": 5,
  "rejected_cumulative": 1,
  "window_start": "2026-05-21T10:00:00Z"
}
```

### GET /stats

```bash
curl http://localhost:8080/stats
```

Response schema:

```json
{
  "users": {
    "user-1": {
      "accepted_current_window": 5,
      "rejected_cumulative": 1,
      "window_start": "2026-05-21T10:00:00Z"
    }
  }
}
```

`rejected_cumulative` is cumulative since the service started. `accepted_current_window` is for the active fixed 1-minute window.

## Part 2: Product catalog with media

### Data model

Products are stored in memory in a map:

```text
products[id]Product
skuIndex[sku]id
```

Each product stores `image_urls` and `video_urls` as string slices.

The list endpoint returns only lightweight fields like counts and thumbnail URL. It does **not** serialize full media URL arrays for every product. The detail endpoint returns the full product with all media URLs.

### Validation rules

- `name` is required and cannot be empty.
- `sku` is required and cannot be empty.
- `sku` must be unique.
- Media URLs must use `http://` or `https://`.
- URL length must be at most 2048 characters.
- Maximum 20 image URLs and 20 video URLs are accepted per request.
- `POST /products/{id}/media` requires at least one URL in `image_urls` or `video_urls`.

### POST /products

```bash
curl -X POST http://localhost:8080/products \
  -H "Content-Type: application/json" \
  -d '{
    "name":"Widget A",
    "sku":"SKU-001",
    "image_urls":[
      "https://cdn.example.com/products/sku-001/img-1.jpg",
      "https://cdn.example.com/products/sku-001/img-2.jpg"
    ],
    "video_urls":[
      "https://cdn.example.com/products/sku-001/demo.mp4"
    ]
  }'
```

Success response: `201 Created` with the full created product.

Duplicate SKU returns `409 Conflict`.

### GET /products

Pagination uses `limit` and `offset`.

Defaults:

- `limit=20`
- `offset=0`
- maximum `limit=100`

```bash
curl "http://localhost:8080/products?limit=20&offset=0"
```

Response:

```json
{
  "limit": 20,
  "offset": 0,
  "total": 1,
  "products": [
    {
      "id": 1,
      "name": "Widget A",
      "sku": "SKU-001",
      "image_count": 2,
      "video_count": 1,
      "thumbnail_url": "https://cdn.example.com/products/sku-001/img-1.jpg",
      "created_at": "2026-05-21T10:00:00Z"
    }
  ]
}
```

Important: this list response does not include `image_urls` or `video_urls` arrays.

### GET /products/{id}

```bash
curl http://localhost:8080/products/1
```

Returns full product including all `image_urls` and `video_urls`.

Unknown ID returns `404 Not Found`.

### POST /products/{id}/media

```bash
curl -X POST http://localhost:8080/products/1/media \
  -H "Content-Type: application/json" \
  -d '{
    "image_urls":["https://cdn.example.com/products/sku-001/img-3.jpg"],
    "video_urls":["https://cdn.example.com/products/sku-001/review.mp4"]
  }'
```

Appends URLs to the existing product and returns the updated full product.

Unknown ID returns `404 Not Found`.

Empty body or no URLs returns `400 Bad Request`.

## Quick rate limit test

Run this command six times quickly:

```bash
curl -X POST http://localhost:8080/request \
  -H "Content-Type: application/json" \
  -d '{"user_id":"user-1","payload":{"n":1}}'
```

The first five requests should return `201`. The sixth should return `429`.

## Production limitations

- State is stored in memory, so restart loses all users, stats, products, and media URLs.
- This works for a single service instance only. Multiple instances would need shared storage such as Redis for rate limiting and PostgreSQL for products.
- Fixed-window limiting can allow short bursts around window boundaries. A rolling-window or token-bucket algorithm would be better for production.
- In production, product core data should be stored in PostgreSQL, media metadata in a separate table, and actual media files should be uploaded to object storage/CDN such as S3 + CloudFront.
- For large catalogs, list endpoints should use indexed database queries and avoid loading full media rows unless the detail endpoint asks for them.

## AI tools note

AI assistance was used to review requirements and improve the code/README.
>>>>>>> 131f001 (Initial commit)
