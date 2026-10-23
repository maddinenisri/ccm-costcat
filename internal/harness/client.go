package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://app.harness.io/gateway"
	basePath       = "/ccm/api/business-mapping"
)

// Client talks to the Harness CCM business-mapping (Cost Categories) API.
type Client struct {
	BaseURL    string
	APIKey     string
	AccountID  string
	HTTP       *http.Client
	MaxRetries int
	UserAgent  string
}

type Option func(*Client)

func WithBaseURL(u string) Option          { return func(c *Client) { c.BaseURL = strings.TrimRight(u, "/") } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.HTTP = h } }
func WithMaxRetries(n int) Option          { return func(c *Client) { c.MaxRetries = n } }
func WithUserAgent(ua string) Option       { return func(c *Client) { c.UserAgent = ua } }

func NewClient(apiKey, accountID string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, errors.New("harness: api key is required")
	}
	if accountID == "" {
		return nil, errors.New("harness: account identifier is required")
	}
	c := &Client{
		BaseURL:    DefaultBaseURL,
		APIKey:     apiKey,
		AccountID:  accountID,
		HTTP:       &http.Client{Timeout: 60 * time.Second},
		MaxRetries: 4,
		UserAgent:  "harness-ccm-costcat/1.0",
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// HTTPError carries the status and decoded Harness error body.
type HTTPError struct {
	StatusCode int
	Method     string
	URL        string
	Body       []byte
	API        *APIError
}

func (e *HTTPError) Error() string {
	msg := strings.TrimSpace(string(e.Body))
	if e.API != nil && e.API.Message != "" {
		msg = e.API.Message
		if e.API.Code != "" {
			msg = e.API.Code + ": " + msg
		}
	}
	if len(msg) > 400 {
		msg = msg[:400] + "..."
	}
	return fmt.Sprintf("harness: %s %s -> %d: %s", e.Method, e.URL, e.StatusCode, msg)
}

// Result bundles the raw body with its decoded form.
type Result[T any] struct {
	Raw   []byte
	Value T
}

// ---- Endpoints -------------------------------------------------------------

// List performs GET /business-mapping with the given options (single page).
func (c *Client) List(ctx context.Context, opts ListOptions) (Result[RestResponse[BusinessMappingList]], error) {
	q := url.Values{}
	if opts.SearchKey != "" {
		q.Set("searchKey", opts.SearchKey)
	}
	if opts.SortType != "" {
		q.Set("sortType", opts.SortType)
	}
	if opts.SortOrder != "" {
		q.Set("sortOrder", opts.SortOrder)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		q.Set("offset", strconv.Itoa(opts.Offset))
	}
	return do[RestResponse[BusinessMappingList]](ctx, c, http.MethodGet, basePath, q, nil)
}

// ListAll pages through GET /business-mapping until totalCount is reached.
func (c *Client) ListAll(ctx context.Context, opts ListOptions) ([]BusinessMapping, [][]byte, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	var (
		all  []BusinessMapping
		raws [][]byte
	)
	for {
		res, err := c.List(ctx, opts)
		if err != nil {
			return all, raws, err
		}
		raws = append(raws, res.Raw)
		page := res.Value.Resource.BusinessMappings
		all = append(all, page...)
		if len(page) == 0 || len(all) >= res.Value.Resource.TotalCount {
			return all, raws, nil
		}
		opts.Offset += len(page)
	}
}

// Get performs GET /business-mapping/{id}. Resource is nil-equivalent (empty) when not found.
func (c *Client) Get(ctx context.Context, id string) (Result[RestResponse[*BusinessMapping]], error) {
	return do[RestResponse[*BusinessMapping]](ctx, c, http.MethodGet, basePath+"/"+url.PathEscape(id), nil, nil)
}

// Create performs POST /business-mapping.
func (c *Client) Create(ctx context.Context, bm BusinessMapping) (Result[json.RawMessage], error) {
	if bm.AccountID == "" {
		bm.AccountID = c.AccountID
	}
	return do[json.RawMessage](ctx, c, http.MethodPost, basePath, nil, bm)
}

// Update performs PUT /business-mapping (bm.UUID must be set).
func (c *Client) Update(ctx context.Context, bm BusinessMapping) (Result[json.RawMessage], error) {
	if bm.UUID == "" {
		return Result[json.RawMessage]{}, errors.New("harness: update requires uuid")
	}
	if bm.AccountID == "" {
		bm.AccountID = c.AccountID
	}
	return do[json.RawMessage](ctx, c, http.MethodPut, basePath, nil, bm)
}

// Delete performs DELETE /business-mapping/{id}.
func (c *Client) Delete(ctx context.Context, id string) (Result[RestResponse[CostCategoryDeleteResult]], error) {
	return do[RestResponse[CostCategoryDeleteResult]](ctx, c, http.MethodDelete, basePath+"/"+url.PathEscape(id), nil, nil)
}

// FilterPanel performs GET /business-mapping/filter-panel.
func (c *Client) FilterPanel(ctx context.Context, costCategory, search string) (Result[ResponseDTO[[]string]], error) {
	q := url.Values{}
	if costCategory != "" {
		q.Set("costCategory", costCategory)
	}
	if search != "" {
		q.Set("search", search)
	}
	return do[ResponseDTO[[]string]](ctx, c, http.MethodGet, basePath+"/filter-panel", q, nil)
}

// LinkedBusinessMapping performs POST /business-mapping/linkedBusinessMapping/{id}.
func (c *Client) LinkedBusinessMapping(ctx context.Context, id string) (Result[RestResponse[LinkedBusinessMapping]], error) {
	return do[RestResponse[LinkedBusinessMapping]](ctx, c, http.MethodPost, basePath+"/linkedBusinessMapping/"+url.PathEscape(id), nil, nil)
}

// LinkedPerspectives performs POST /business-mapping/linkedPerspectives with a list of IDs.
func (c *Client) LinkedPerspectives(ctx context.Context, ids []string) (Result[RestResponse[[]LinkedPerspectives]], error) {
	if len(ids) == 0 {
		return Result[RestResponse[[]LinkedPerspectives]]{}, errors.New("harness: at least one id required")
	}
	return do[RestResponse[[]LinkedPerspectives]](ctx, c, http.MethodPost, basePath+"/linkedPerspectives", nil, ids)
}

// ---- Transport -------------------------------------------------------------

func do[T any](ctx context.Context, c *Client, method, path string, q url.Values, body any) (Result[T], error) {
	var out Result[T]

	if q == nil {
		q = url.Values{}
	}
	q.Set("accountIdentifier", c.AccountID)
	full := c.BaseURL + path + "?" + q.Encode()

	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return out, fmt.Errorf("harness: marshal body: %w", err)
		}
		payload = b
	}

	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, backoff(attempt)); err != nil {
				return out, err
			}
		}

		var rdr io.Reader
		if payload != nil {
			rdr = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, full, rdr)
		if err != nil {
			return out, err
		}
		req.Header.Set("x-api-key", c.APIKey)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.UserAgent)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = newHTTPError(resp.StatusCode, method, full, raw)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return out, newHTTPError(resp.StatusCode, method, full, raw)
		}

		out.Raw = raw
		if len(bytes.TrimSpace(raw)) == 0 {
			return out, nil
		}
		if err := json.Unmarshal(raw, &out.Value); err != nil {
			return out, fmt.Errorf("harness: decode %s %s: %w", method, path, err)
		}
		return out, nil
	}
	return out, fmt.Errorf("harness: giving up after %d attempts: %w", c.MaxRetries+1, lastErr)
}

func newHTTPError(status int, method, u string, body []byte) *HTTPError {
	e := &HTTPError{StatusCode: status, Method: method, URL: redactURL(u), Body: body}
	var api APIError
	if json.Unmarshal(body, &api) == nil && (api.Message != "" || api.Code != "") {
		e.API = &api
	}
	return e
}

func redactURL(u string) string {
	if i := strings.Index(u, "?"); i >= 0 {
		return u[:i]
	}
	return u
}

func backoff(attempt int) time.Duration {
	base := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
	if base > 16*time.Second {
		base = 16 * time.Second
	}
	jitter := time.Duration(rand.Int63n(int64(base) / 2))
	return base + jitter
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
