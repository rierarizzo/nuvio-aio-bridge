// Package nuvio is a small client for the Nuvio cloud API.
//
// It signs in with an email and password, keeps the session in memory, and
// refreshes it when it expires. Nuvio rotates the refresh token on every use,
// so the client serialises refreshes behind a mutex and never persists tokens.
package nuvio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// ErrAuth indicates the credentials or session were rejected.
var ErrAuth = errors.New("nuvio authentication failed")

// Client talks to one Nuvio account and profile.
type Client struct {
	baseURL  string
	anonKey  string
	email    string
	password string
	profile  int
	http     *http.Client

	mu           sync.Mutex
	accessToken  string
	refreshToken string
	expiresAt    time.Time

	// libMu serialises the read-modify-write of the whole library snapshot, so
	// concurrent favourite events cannot overwrite each other's changes.
	libMu sync.Mutex
}

// New builds a client. It performs no network calls.
func New(baseURL, anonKey, email, password string, profile int) *Client {
	return &Client{
		baseURL:  baseURL,
		anonKey:  anonKey,
		email:    email,
		password: password,
		profile:  profile,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

type authSession struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// ensureSession signs in when there is no session, and refreshes shortly
// before expiry. Nuvio's access token lasts a week, but the refresh token
// rotates, so this keeps a single live session in memory.
func (c *Client) ensureSession(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-5*time.Minute)) {
		return nil
	}

	if c.refreshToken == "" {
		return c.signInLocked(ctx)
	}
	if err := c.refreshLocked(ctx); err != nil {
		if !errors.Is(err, ErrAuth) {
			// A network or server error: keep the session and report it, so
			// the caller can retry rather than discarding a usable token.
			return err
		}
		// A rejected refresh token means the session is unrecoverable; a full
		// sign-in is the only way back in.
		c.accessToken, c.refreshToken, c.expiresAt = "", "", time.Time{}
		return c.signInLocked(ctx)
	}
	return nil
}

func (c *Client) signInLocked(ctx context.Context) error {
	endpoint := c.baseURL + "/auth/v1/token?" + url.Values{"grant_type": {"password"}}.Encode()
	body := map[string]string{"email": c.email, "password": c.password}
	var session authSession
	if err := c.postJSON(ctx, endpoint, "", body, &session); err != nil {
		if errors.Is(err, errUnauthorized) {
			// Bad credentials are not retryable.
			return fmt.Errorf("%w: sign-in rejected", ErrAuth)
		}
		return err
	}
	c.applySession(session)
	return nil
}

func (c *Client) refreshLocked(ctx context.Context) error {
	if c.refreshToken == "" {
		return fmt.Errorf("%w: no refresh token", ErrAuth)
	}
	endpoint := c.baseURL + "/auth/v1/token?" + url.Values{"grant_type": {"refresh_token"}}.Encode()
	body := map[string]string{"refresh_token": c.refreshToken}
	var session authSession
	if err := c.postJSON(ctx, endpoint, "", body, &session); err != nil {
		if errors.Is(err, errUnauthorized) {
			return fmt.Errorf("%w: refresh rejected", ErrAuth)
		}
		return err
	}
	c.applySession(session)
	return nil
}

func (c *Client) applySession(session authSession) {
	c.accessToken = session.AccessToken
	if session.RefreshToken != "" {
		c.refreshToken = session.RefreshToken
	}
	expires := session.ExpiresIn
	if expires <= 0 {
		expires = 3600
	}
	c.expiresAt = time.Now().Add(time.Duration(expires) * time.Second)
}

// rpc calls one Supabase RPC function with the current session. On a 401/403
// it re-authenticates once and retries.
func (c *Client) rpc(ctx context.Context, name string, payload any) (json.RawMessage, error) {
	if err := c.ensureSession(ctx); err != nil {
		return nil, err
	}

	var raw json.RawMessage
	err := c.postJSON(ctx, c.baseURL+"/rest/v1/rpc/"+name, c.bearer(), payload, &raw)
	if err == nil {
		return raw, nil
	}
	if !errors.Is(err, errUnauthorized) {
		return nil, err
	}

	// The access token was rejected. Refresh; if the refresh token is also
	// rejected, fall back to a full sign-in.
	c.mu.Lock()
	reErr := c.refreshLocked(ctx)
	if errors.Is(reErr, ErrAuth) {
		// The refresh token was rejected; start a fresh session.
		c.accessToken, c.refreshToken, c.expiresAt = "", "", time.Time{}
		reErr = c.signInLocked(ctx)
	}
	c.mu.Unlock()
	if reErr != nil {
		return nil, reErr
	}

	if err := c.postJSON(ctx, c.baseURL+"/rest/v1/rpc/"+name, c.bearer(), payload, &raw); err != nil {
		if errors.Is(err, errUnauthorized) {
			return nil, fmt.Errorf("%w: rpc %s rejected after re-auth", ErrAuth, name)
		}
		return nil, err
	}
	return raw, nil
}

var errUnauthorized = errors.New("unauthorized")

func (c *Client) bearer() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accessToken
}

func (c *Client) postJSON(ctx context.Context, endpoint, bearer string, payload any, out any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("apikey", c.anonKey)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return errUnauthorized
	case resp.StatusCode >= 500:
		return fmt.Errorf("nuvio %s: server error %d", endpoint, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("nuvio %s: status %d: %s", endpoint, resp.StatusCode, truncate(data))
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

func truncate(data []byte) string {
	const max = 200
	if len(data) > max {
		return string(data[:max])
	}
	return string(data)
}
