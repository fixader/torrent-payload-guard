package arr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type queueResponse struct {
	Records []struct {
		ID         int    `json:"id"`
		DownloadID string `json:"downloadId"`
	} `json:"records"`
}

type Client struct {
	base, key string
	http      *http.Client
}

func New(base, key string) *Client {
	return &Client{base: base, key: key, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) ReportFailed(ctx context.Context, hash string) (bool, error) {
	if c.base == "" || c.key == "" {
		return false, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v3/queue?page=1&pageSize=100", nil)
	req.Header.Set("X-Api-Key", c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return false, fmt.Errorf("queue lookup HTTP %d", resp.StatusCode)
	}
	var queue queueResponse
	if err := json.NewDecoder(resp.Body).Decode(&queue); err != nil {
		return false, err
	}
	for _, item := range queue.Records {
		if equalHash(item.DownloadID, hash) {
			path := c.base + "/api/v3/queue/" + strconv.Itoa(item.ID) +
				"?removeFromClient=false&blocklist=true&skipRedownload=false&changeCategory=false"
			deleteReq, _ := http.NewRequestWithContext(ctx, http.MethodDelete, path, nil)
			deleteReq.Header.Set("X-Api-Key", c.key)
			deleteResp, err := c.http.Do(deleteReq)
			if err != nil {
				return false, err
			}
			deleteResp.Body.Close()
			if deleteResp.StatusCode/100 != 2 {
				return false, fmt.Errorf("queue blocklist HTTP %d", deleteResp.StatusCode)
			}
			return true, nil
		}
	}
	return false, nil
}

func equalHash(a, b string) bool {
	ua, _ := url.QueryUnescape(a)
	return len(ua) > 0 && len(b) > 0 && (ua == b || normalize(ua) == normalize(b))
}

func normalize(value string) string {
	result := ""
	for _, char := range value {
		if char >= 'A' && char <= 'Z' {
			char += 'a' - 'A'
		}
		result += string(char)
	}
	return result
}
