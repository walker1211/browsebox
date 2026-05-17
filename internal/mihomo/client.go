package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Client reads mihomo controller state over a Unix socket.
type Client struct {
	socketPath string
	baseURL    string
	httpClient *http.Client
}

// ProxyGroupInfo is the read-only controller representation of a proxy group.
type ProxyGroupInfo struct {
	Name string   `json:"name"`
	Type string   `json:"type"`
	Now  string   `json:"now"`
	All  []string `json:"all"`
}

// DelayResult is the controller response for a proxy delay check.
type DelayResult struct {
	Delay int    `json:"delay"`
	Mean  int    `json:"meanDelay,omitempty"`
	Error string `json:"message,omitempty"`
}

// NewClient creates a read-only mihomo controller client for socketPath.
func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}

	return &Client{
		socketPath: socketPath,
		baseURL:    "http://mihomo",
		httpClient: &http.Client{Transport: transport},
	}
}

// NewTCPClient creates a mihomo controller client for a localhost TCP controller URL.
func NewTCPClient(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: http.DefaultClient,
	}
}

// GetJSON performs a read-only GET request against the controller and decodes JSON into out.
func (c *Client) GetJSON(ctx context.Context, path string, out any) error {
	requestPath := path
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}

	resp, err := c.doJSON(ctx, http.MethodGet, requestPath, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode GET %s response: %w", requestPath, err)
	}
	return nil
}

// PutJSON performs a PUT request against the controller with a JSON payload.
func (c *Client) PutJSON(ctx context.Context, path string, payload any, out any) error {
	requestPath := path
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}

	body := &bytes.Buffer{}
	if err := json.NewEncoder(body).Encode(payload); err != nil {
		return fmt.Errorf("encode PUT %s payload: %w", requestPath, err)
	}
	resp, err := c.doJSON(ctx, http.MethodPut, requestPath, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode PUT %s response: %w", requestPath, err)
		}
	}
	return nil
}

func (c *Client) doJSON(ctx context.Context, method, requestPath string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+requestPath, body)
	if err != nil {
		return nil, fmt.Errorf("build controller request %q: %w", requestPath, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s via %s: %w", method, requestPath, c.endpointDescription(), err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return nil, fmt.Errorf("%s %s returned %s: %s", method, requestPath, resp.Status, message)
	}
	return resp, nil
}

func (c *Client) endpointDescription() string {
	if c.socketPath != "" {
		return c.socketPath
	}
	return c.baseURL
}

// ProxyGroups lists proxy group entries from the controller.
func (c *Client) ProxyGroups(ctx context.Context) ([]ProxyGroupInfo, error) {
	var response struct {
		Proxies map[string]ProxyGroupInfo `json:"proxies"`
	}
	if err := c.GetJSON(ctx, "/proxies", &response); err != nil {
		return nil, err
	}

	groups := make([]ProxyGroupInfo, 0, len(response.Proxies))
	for name, group := range response.Proxies {
		if len(group.All) == 0 {
			continue
		}
		if group.Name == "" {
			group.Name = name
		}
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})
	return groups, nil
}

// ProxyGroup reads a proxy group by name.
func (c *Client) ProxyGroup(ctx context.Context, name string) (ProxyGroupInfo, error) {
	var group ProxyGroupInfo
	path := "/proxies/" + url.PathEscape(name)
	if err := c.GetJSON(ctx, path, &group); err != nil {
		return ProxyGroupInfo{}, err
	}
	return group, nil
}

// SelectNode selects node in group using the controller's mutating proxy API.
func (c *Client) SelectNode(ctx context.Context, group, node string) error {
	path := "/proxies/" + url.PathEscape(group)
	return c.PutJSON(ctx, path, map[string]string{"name": node}, nil)
}

// Delay checks the delay for node against targetURL with timeoutMS.
func (c *Client) Delay(ctx context.Context, node, targetURL string, timeoutMS int) (DelayResult, error) {
	query := url.Values{}
	query.Set("url", targetURL)
	query.Set("timeout", fmt.Sprintf("%d", timeoutMS))

	var result DelayResult
	path := "/proxies/" + url.PathEscape(node) + "/delay?" + query.Encode()
	if err := c.GetJSON(ctx, path, &result); err != nil {
		return DelayResult{}, err
	}
	return result, nil
}
