package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
)

type Client struct {
	sockPath string
	client   *http.Client
}

func NewClient(sockPath string) *Client {
	return &Client{
		sockPath: sockPath,
		client: &http.Client{
			Transport: &http.Transport{
				Dial: func(_, _ string) (net.Conn, error) {
					return net.Dial("unix", sockPath)
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

	resp, err := c.client.Get(reqURL.String())
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var response Response
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
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

func (c *Client) Stop(programName string) (*Response, error) {
	params := make(map[string]string)
	if programName != "" {
		params["program"] = programName
	}
	return c.sendRequest("/stop", params)
}
