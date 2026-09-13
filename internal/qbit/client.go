package qbit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

type Torrent struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Tags     string  `json:"tags"`
	State    string  `json:"state"`
	Progress float64 `json:"progress"`
	AddedOn  int64   `json:"added_on"`
}
type File struct {
	Name string `json:"name"`
}

type Client struct {
	base, username, password, apiKey, authMode string
	http                                       *http.Client
}

func New(base, username, password, apiKey, authMode string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{base: base, username: username, password: password, apiKey: apiKey, authMode: authMode,
		http: &http.Client{Timeout: 15 * time.Second, Jar: jar}}
}

func (c *Client) Login(ctx context.Context) error {
	if c.useAPIKey() {
		return nil
	}
	values := url.Values{"username": {c.username}, "password": {c.password}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v2/auth/login", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 || strings.TrimSpace(string(body)) != "Ok." {
		return fmt.Errorf("qBittorrent login failed: HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) Torrents(ctx context.Context) ([]Torrent, error) {
	var result []Torrent
	return result, c.getJSON(ctx, "/api/v2/torrents/info?filter=all", &result)
}

func (c *Client) Files(ctx context.Context, hash string) ([]File, error) {
	var result []File
	return result, c.getJSON(ctx, "/api/v2/torrents/files?hash="+url.QueryEscape(hash), &result)
}

func (c *Client) Tag(ctx context.Context, hash, tag string) error {
	return c.postForm(ctx, "/api/v2/torrents/addTags", url.Values{"hashes": {hash}, "tags": {tag}})
}
func (c *Client) Pause(ctx context.Context, hash string) error {
	return c.postForm(ctx, "/api/v2/torrents/stop", url.Values{"hashes": {hash}})
}
func (c *Client) Delete(ctx context.Context, hash string, deleteData bool) error {
	return c.postForm(ctx, "/api/v2/torrents/delete", url.Values{
		"hashes": {hash}, "deleteFiles": {fmt.Sprintf("%t", deleteData)}})
}

func (c *Client) getJSON(ctx context.Context, path string, target any) error {
	return c.getJSONAttempt(ctx, path, target, true)
}

func (c *Client) getJSONAttempt(ctx context.Context, path string, target any, retry bool) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	c.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden && retry && !c.useAPIKey() {
		if err := c.Login(ctx); err != nil {
			return err
		}
		return c.getJSONAttempt(ctx, path, target, false)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("qBittorrent HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *Client) postForm(ctx context.Context, path string, values url.Values) error {
	return c.postFormAttempt(ctx, path, values, true)
}

func (c *Client) postFormAttempt(ctx context.Context, path string, values url.Values, retry bool) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden && retry && !c.useAPIKey() {
		if err := c.Login(ctx); err != nil {
			return err
		}
		return c.postFormAttempt(ctx, path, values, false)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("qBittorrent action HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) useAPIKey() bool {
	return c.authMode == "api_key" || (c.authMode == "auto" && c.apiKey != "")
}

func (c *Client) authorize(req *http.Request) {
	if c.useAPIKey() {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}
