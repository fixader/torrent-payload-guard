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
}
type File struct {
	Name string `json:"name"`
}

type Client struct {
	base, username, password string
	http                     *http.Client
}

func New(base, username, password string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{base: base, username: username, password: password,
		http: &http.Client{Timeout: 15 * time.Second, Jar: jar}}
}

func (c *Client) Login(ctx context.Context) error {
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
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		if err := c.Login(ctx); err != nil {
			return err
		}
		return c.getJSON(ctx, path, target)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("qBittorrent HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *Client) postForm(ctx context.Context, path string, values url.Values) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("qBittorrent action HTTP %d", resp.StatusCode)
	}
	return nil
}
