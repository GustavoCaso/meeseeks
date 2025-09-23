package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
)

type Client struct {
	sockPath string
	client   *http.Client
}

func NewClient(ctx context.Context, sockPath string) *Client {
	return &Client{
		sockPath: sockPath,
		client: &http.Client{
			Transport: &http.Transport{
				Dial: func(_, _ string) (net.Conn, error) {
					var d net.Dialer
					raddr := net.UnixAddr{Name: sockPath, Net: "unix"}

					return d.DialContext(ctx, "unix", raddr.String())
				},
			},
		},
	}
}

func (c *Client) sendRequest(endpoint string, params map[string]string) (*Response, error) {
	if _, err := os.Stat(c.sockPath); os.IsNotExist(err) {
		return nil, errors.New("meeseeks server not running (socket not found)")
	}

	// Create URL with query parameters
	reqURL := &url.URL{
		Scheme: "http",
		Host:   "unix",
		Path:   endpoint,
	}

	if len(params) > 0 {
		q := reqURL.Query()
		for key, value := range params {
			q.Set(key, value)
		}
		reqURL.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, readErr
		}

		return nil, fmt.Errorf("response failed: %s", bodyBytes)
	}

	var response Response
	if err = json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

func (c *Client) Statistics(programName string) (*Response, error) {
	params := make(map[string]string)
	if programName != "" {
		params["program"] = programName
	}
	return c.sendRequest("/statistics", params)
}

func (c *Client) Logs(programName string) (*Response, error) {
	params := map[string]string{
		"program": programName,
	}
	return c.sendRequest("/logs", params)
}

func (c *Client) Stop(programName, timeout string) (*Response, error) {
	params := map[string]string{
		"program": programName,
		"timeout": timeout,
	}

	return c.sendRequest("/stop", params)
}

func (c *Client) Reload(timeout string) (*Response, error) {
	params := map[string]string{
		"timeout": timeout,
	}

	return c.sendRequest("/reload", params)
}

func (c *Client) RunProgram(programName string) (*Response, error) {
	params := map[string]string{
		"program": programName,
	}

	return c.sendRequest("/run-program", params)
}
