package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
)

type Client struct {
	sockPath string
}

func NewClient(sockPath string) *Client {
	return &Client{sockPath: sockPath}
}

func (c *Client) connect() (net.Conn, error) {
	if _, err := os.Stat(c.sockPath); os.IsNotExist(err) {
		return nil, errors.New("meeseeks daemon not running (socket not found)")
	}

	conn, err := net.Dial("unix", c.sockPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	return conn, nil
}

func (c *Client) sendRequest(req Request) (*Response, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &resp, nil
}

func (c *Client) Status(programName string) (*Response, error) {
	req := Request{
		Command: "status",
	}
	if programName != "" {
		req.Args = map[string]interface{}{
			"program": programName,
		}
	}
	return c.sendRequest(req)
}

func (c *Client) Logs(programName string) (*Response, error) {
	req := Request{
		Command: "logs",
		Args: map[string]interface{}{
			"program": programName,
		},
	}
	return c.sendRequest(req)
}

func (c *Client) Stop(programName string) (*Response, error) {
	req := Request{
		Command: "stop",
	}
	if programName != "" {
		req.Args = map[string]interface{}{
			"program": programName,
		}
	}
	return c.sendRequest(req)
}

func (c *Client) Exit() (*Response, error) {
	req := Request{
		Command: "exit",
	}
	return c.sendRequest(req)
}
