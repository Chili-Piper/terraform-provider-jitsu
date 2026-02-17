package client

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/lib/pq"
)

// Client provides HTTP and optional DB access to the Jitsu Console API.
type Client struct {
	consoleURL  string
	username    string
	password    string
	databaseURL string
	userAgent   string
	httpClient  *http.Client

	authMu        sync.Mutex
	authenticated bool

	dbOnce sync.Once
	db     *sql.DB
	dbErr  error
}

// New creates a new Jitsu API client. databaseURL is optional — needed only for soft-delete recovery.
func New(consoleURL, username, password, databaseURL, userAgent string) *Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		panic(fmt.Sprintf("cookiejar.New: %v", err))
	}
	return &Client{
		consoleURL:  strings.TrimRight(consoleURL, "/"),
		username:    username,
		password:    password,
		databaseURL: databaseURL,
		userAgent:   userAgent,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
	}
}

// Close releases resources held by the client (e.g., DB connection pool).
func (c *Client) Close() {
	if c.db != nil {
		c.db.Close()
	}
}

// getDB returns a lazily-initialized DB connection pool.
func (c *Client) getDB() (*sql.DB, error) {
	if c.databaseURL == "" {
		return nil, fmt.Errorf("database_url not configured in provider; " +
			"Jitsu uses soft-delete, so re-creating objects with the same ID requires database_url to hard-delete stale rows")
	}
	c.dbOnce.Do(func() {
		c.db, c.dbErr = sql.Open("postgres", c.databaseURL)
		if c.dbErr != nil {
			return
		}
		c.db.SetMaxOpenConns(2)
		c.db.SetMaxIdleConns(1)
	})
	return c.db, c.dbErr
}

// hardDeleteSoftDeleted removes a soft-deleted row from the DB so it can be re-created via POST.
// For ConfigurationObject, it also cascades to soft-deleted links referencing it.
func (c *Client) hardDeleteSoftDeleted(ctx context.Context, id, table string) error {
	db, err := c.getDB()
	if err != nil {
		return fmt.Errorf("cannot purge soft-deleted %q: %w", id, err)
	}

	tflog.Warn(ctx, "hard-deleting soft-deleted row for re-creation", map[string]interface{}{
		"id":    id,
		"table": table,
	})

	// For config objects, first delete any soft-deleted links that reference this object (FK constraint)
	if table == "ConfigurationObject" {
		_, err = db.ExecContext(ctx,
			`DELETE FROM newjitsu."ConfigurationObjectLink" WHERE deleted = true AND ("fromId" = $1 OR "toId" = $1)`,
			id,
		)
		if err != nil {
			return fmt.Errorf("hard-deleting referencing links for %q: %w", id, err)
		}
	}

	query := fmt.Sprintf(`DELETE FROM newjitsu.%s WHERE id = $1 AND deleted = true`,
		pq.QuoteIdentifier(table))
	_, err = db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("hard-deleting soft-deleted %s %q: %w", table, id, err)
	}
	return nil
}

func (c *Client) configURL(workspaceID, resourceType string) string {
	return fmt.Sprintf("%s/api/%s/config/%s", c.consoleURL, url.PathEscape(workspaceID), url.PathEscape(resourceType))
}

func (c *Client) configItemURL(workspaceID, resourceType, id string) string {
	return fmt.Sprintf(
		"%s/api/%s/config/%s/%s",
		c.consoleURL,
		url.PathEscape(workspaceID),
		url.PathEscape(resourceType),
		url.PathEscape(id),
	)
}

func (c *Client) workspaceURL() string {
	return fmt.Sprintf("%s/api/workspace", c.consoleURL)
}

func (c *Client) workspaceItemURL(idOrSlug string) string {
	return fmt.Sprintf("%s/api/workspace/%s", c.consoleURL, url.PathEscape(idOrSlug))
}

func (c *Client) authenticate(ctx context.Context) error {
	if c.username == "" || c.password == "" {
		return fmt.Errorf("no authentication configured: set username/password")
	}

	c.authMu.Lock()
	defer c.authMu.Unlock()

	if c.authenticated {
		return nil
	}

	csrfURL := fmt.Sprintf("%s/api/auth/csrf", c.consoleURL)
	csrfRespBody, status, err := c.rawRequest(ctx, http.MethodGet, csrfURL, nil, map[string]string{})
	if err != nil {
		return fmt.Errorf("requesting CSRF token: %w", err)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("GET %s returned %d: %s", csrfURL, status, string(csrfRespBody))
	}

	var csrf struct {
		Token string `json:"csrfToken"`
	}
	if err := json.Unmarshal(csrfRespBody, &csrf); err != nil {
		return fmt.Errorf("parsing CSRF response: %w", err)
	}
	if csrf.Token == "" {
		return fmt.Errorf("empty CSRF token in response")
	}

	loginURL := fmt.Sprintf("%s/api/auth/callback/credentials", c.consoleURL)
	form := url.Values{}
	form.Set("username", c.username)
	form.Set("password", c.password)
	form.Set("csrfToken", csrf.Token)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("creating login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// NextAuth credential callback returns redirect; keep cookie side-effects without following that redirect.
	loginClient := *c.httpClient
	loginClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := loginClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing login request: %w", err)
	}
	defer resp.Body.Close()

	loginRespBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading login response: %w", err)
	}
	// NextAuth often responds with 302 on successful credential login.
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("POST %s returned %d: %s", loginURL, resp.StatusCode, string(loginRespBody))
	}

	c.authenticated = true
	return nil
}

func (c *Client) markUnauthenticated() {
	c.authMu.Lock()
	c.authenticated = false
	c.authMu.Unlock()
}

func isAuthFailureStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}

func (c *Client) rawRequest(ctx context.Context, method, requestURL string, reqBody io.Reader, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, requestURL, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("creating request: %w", err)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response body: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

func (c *Client) doRequest(ctx context.Context, method, requestURL string, body interface{}) ([]byte, int, error) {
	if err := c.authenticate(ctx); err != nil {
		return nil, 0, err
	}

	send := func() ([]byte, int, error) {
		var reqBody io.Reader
		if body != nil {
			jsonBytes, err := json.Marshal(body)
			if err != nil {
				return nil, 0, fmt.Errorf("marshaling request body: %w", err)
			}
			reqBody = bytes.NewReader(jsonBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, requestURL, reqBody)
		if err != nil {
			return nil, 0, fmt.Errorf("creating request: %w", err)
		}

		if c.userAgent != "" {
			req.Header.Set("User-Agent", c.userAgent)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		tflog.Debug(ctx, "API request", map[string]interface{}{
			"method": method,
			"url":    requestURL,
		})

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, 0, fmt.Errorf("executing request: %w", err)
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, resp.StatusCode, fmt.Errorf("reading response body: %w", err)
		}

		tflog.Debug(ctx, "API response", map[string]interface{}{
			"method":      method,
			"url":         requestURL,
			"status_code": resp.StatusCode,
		})

		return respBody, resp.StatusCode, nil
	}

	respBody, status, err := send()
	if err != nil {
		return nil, 0, err
	}

	// Session auth uses cookies; if they expire, re-authenticate once and retry.
	if isAuthFailureStatus(status) {
		tflog.Warn(ctx, "API request returned auth failure; re-authenticating and retrying once", map[string]interface{}{
			"method":      method,
			"url":         requestURL,
			"status_code": status,
		})
		c.markUnauthenticated()
		if err := c.authenticate(ctx); err != nil {
			return nil, 0, fmt.Errorf("re-authenticating after %d response: %w", status, err)
		}
		respBody, status, err = send()
		if err != nil {
			return nil, 0, err
		}
	}

	return respBody, status, nil
}

// Create sends POST to create a config object. Returns the response body.
// If the POST fails due to a unique constraint (soft-deleted row), it hard-deletes the row and retries.
func (c *Client) Create(ctx context.Context, workspaceID, resourceType string, payload map[string]interface{}) (map[string]interface{}, error) {
	endpoint := c.configURL(workspaceID, resourceType)
	body, status, err := c.doRequest(ctx, "POST", endpoint, payload)
	if err != nil {
		return nil, err
	}

	// Handle soft-delete conflict: hard-delete the row and retry
	if status == 500 && strings.Contains(string(body), "Unique constraint failed") {
		id, ok := payload["id"].(string)
		if !ok || id == "" {
			return nil, fmt.Errorf("POST %s returned soft-delete conflict but payload has no 'id' field", endpoint)
		}
		table := "ConfigurationObject"
		if resourceType == "link" {
			table = "ConfigurationObjectLink"
		}
		if err := c.hardDeleteSoftDeleted(ctx, id, table); err != nil {
			return nil, fmt.Errorf("POST failed (soft-delete conflict) and cleanup failed: %w", err)
		}
		// Retry
		body, status, err = c.doRequest(ctx, "POST", endpoint, payload)
		if err != nil {
			return nil, err
		}
	}

	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("POST %s returned %d: %s", endpoint, status, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}
	return result, nil
}

// Read sends GET to fetch a config object by ID. Returns nil if not found or soft-deleted.
func (c *Client) Read(ctx context.Context, workspaceID, resourceType, id string) (map[string]interface{}, error) {
	endpoint := c.configItemURL(workspaceID, resourceType, id)
	body, status, err := c.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	if status == 404 {
		return nil, nil
	}

	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("GET %s returned %d: %s", endpoint, status, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	if deleted, ok := result["deleted"].(bool); ok && deleted {
		return nil, nil
	}

	return result, nil
}

// Update sends PUT to update a config object.
func (c *Client) Update(ctx context.Context, workspaceID, resourceType, id string, payload map[string]interface{}) (map[string]interface{}, error) {
	endpoint := c.configItemURL(workspaceID, resourceType, id)
	body, status, err := c.doRequest(ctx, "PUT", endpoint, payload)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("PUT %s returned %d: %s", endpoint, status, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}
	return result, nil
}

// Delete sends DELETE to remove a config object (soft-delete on Jitsu side).
func (c *Client) Delete(ctx context.Context, workspaceID, resourceType, id string) error {
	endpoint := c.configItemURL(workspaceID, resourceType, id)
	body, status, err := c.doRequest(ctx, "DELETE", endpoint, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("DELETE %s returned %d: %s", endpoint, status, string(body))
	}
	return nil
}

// List sends GET to list all config objects of a type.
// The API returns {"objects": [...]} for most types and {"links": [...]} for links.
func (c *Client) List(ctx context.Context, workspaceID, resourceType string) ([]map[string]interface{}, error) {
	endpoint := c.configURL(workspaceID, resourceType)
	body, status, err := c.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("GET %s returned %d: %s", endpoint, status, string(body))
	}

	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	var items json.RawMessage
	if raw, ok := wrapper["links"]; ok {
		items = raw
	} else if raw, ok := wrapper["objects"]; ok {
		items = raw
	} else {
		return nil, fmt.Errorf("unexpected response format: no 'objects' or 'links' key")
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(items, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling items: %w", err)
	}
	return result, nil
}

// DeleteLink deletes a link by query parameter.
func (c *Client) DeleteLink(ctx context.Context, workspaceID, id string) error {
	endpoint := fmt.Sprintf(
		"%s/api/%s/config/link?id=%s",
		c.consoleURL,
		url.PathEscape(workspaceID),
		url.QueryEscape(id),
	)
	body, status, err := c.doRequest(ctx, "DELETE", endpoint, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("DELETE %s returned %d: %s", endpoint, status, string(body))
	}
	return nil
}

// WorkspaceCreate creates a workspace and returns its ID.
func (c *Client) WorkspaceCreate(ctx context.Context, name, slug string) (string, error) {
	payload := map[string]interface{}{
		"name": name,
		"slug": slug,
	}
	endpoint := c.workspaceURL()
	body, status, err := c.doRequest(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		if status == 500 && strings.Contains(string(body), "WorkspaceAccess_userId_fkey") {
			return "", fmt.Errorf(
				"workspace creation failed due to missing/invalid user session context: %s",
				string(body),
			)
		}
		return "", fmt.Errorf("POST %s returned %d: %s", endpoint, status, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("unmarshaling response: %w", err)
	}
	id, _ := result["id"].(string)
	if id == "" {
		return "", fmt.Errorf("POST %s did not return workspace id", endpoint)
	}
	return id, nil
}

// WorkspaceRead fetches a workspace by ID or slug. Returns nil if not found or deleted.
func (c *Client) WorkspaceRead(ctx context.Context, idOrSlug string) (map[string]interface{}, error) {
	endpoint := c.workspaceItemURL(idOrSlug)
	body, status, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if status == 404 {
		return nil, nil
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("GET %s returned %d: %s", endpoint, status, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}
	if deleted, ok := result["deleted"].(bool); ok && deleted {
		return nil, nil
	}
	return result, nil
}

// WorkspaceUpdate updates a workspace name/slug by ID or slug.
func (c *Client) WorkspaceUpdate(ctx context.Context, idOrSlug, name, slug string) (map[string]interface{}, error) {
	payload := map[string]interface{}{
		"name": name,
		"slug": slug,
	}
	endpoint := c.workspaceItemURL(idOrSlug)
	body, status, err := c.doRequest(ctx, http.MethodPut, endpoint, payload)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("PUT %s returned %d: %s", endpoint, status, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}
	return result, nil
}

// WorkspaceDelete soft-deletes a workspace by ID.
func (c *Client) WorkspaceDelete(ctx context.Context, workspaceID string) error {
	payload := map[string]interface{}{
		"workspaceId": workspaceID,
	}
	endpoint := c.workspaceURL()
	body, status, err := c.doRequest(ctx, http.MethodDelete, endpoint, payload)
	if err != nil {
		return err
	}
	if status == 404 {
		return nil
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("DELETE %s returned %d: %s", endpoint, status, string(body))
	}
	return nil
}
