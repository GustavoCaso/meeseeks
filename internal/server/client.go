package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
)

type Client struct {
	sockPath string
	client   *http.Client
}

type event struct {
	Data []byte
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

func (c *Client) Statistics(ctx context.Context, programName string) (*Response, error) {
	params := make(map[string]string)
	if programName != "" {
		params["program"] = programName
	}
	return c.sendRequest(ctx, "/statistics", params)
}

func (c *Client) Logs(ctx context.Context, programName string) (*Response, error) {
	params := map[string]string{
		"program": programName,
	}
	return c.sendRequest(ctx, "/logs", params)
}

//nolint:gochecknoglobals // This gloabls is convinient
var headerData = []byte("data:")

func (c *Client) FollowLogs(
	ctx context.Context,
	programName string,
	subscribeToPreviousLogs bool,
	logLines chan []byte,
) error {
	if _, err := os.Stat(c.sockPath); os.IsNotExist(err) {
		return errors.New("meeseeks server not running (socket not found)")
	}

	reqURL := &url.URL{
		Scheme: "http",
		Host:   "unix",
		Path:   "/follow-logs",
	}

	q := reqURL.Query()
	q.Set("program", programName)
	q.Set("subscribe_to_previous_logs", strconv.FormatBool(subscribeToPreviousLogs))
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	//nolint:bodyclose // We can not use defer as we close the inside a goroutine
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}

	// Check for HTTP errors before attempting to parse SSE
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var response Response
		if err = json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return fmt.Errorf("server returned status %d: %w", resp.StatusCode, err)
		}
		return fmt.Errorf("failed to follow logs: %s", response.Error)
	}

	go func() {
		defer resp.Body.Close()
		defer close(logLines)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				// Skip empty
				continue
			}
			event := processEvent(line)

			select {
			case <-ctx.Done():
				return
			case logLines <- event.Data:
			}
		}

		if err = scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "Error reading body follow logs:%s\n", err)
		}
	}()

	return nil
}

func (c *Client) Stop(ctx context.Context, programName, timeout string) (*Response, error) {
	params := map[string]string{
		"program": programName,
		"timeout": timeout,
	}

	return c.sendRequest(ctx, "/stop", params)
}

func (c *Client) Reload(ctx context.Context, timeout string) (*Response, error) {
	params := map[string]string{
		"timeout": timeout,
	}

	return c.sendRequest(ctx, "/reload", params)
}

func (c *Client) RunProgram(ctx context.Context, programName string, async bool) (*Response, error) {
	params := map[string]string{
		"program": programName,
		"async":   strconv.FormatBool(async),
	}

	return c.sendRequest(ctx, "/run-program", params)
}

func (c *Client) sendRequest(ctx context.Context, endpoint string, params map[string]string) (*Response, error) {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
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

func processEvent(msg []byte) *event {
	var e event

	// Normalize the crlf to lf to make it easier to split the lines.
	// Split the line by "\n" or "\r", per the spec.
	for _, line := range bytes.FieldsFunc(msg, func(r rune) bool { return r == '\n' || r == '\r' }) {
		switch {
		case bytes.HasPrefix(line, headerData):
			// The spec allows for multiple data fields per event, concatenated them with "\n".
			e.Data = append(e.Data, append(trimHeader(len(headerData), line), byte('\n'))...)
		// The spec says that a line that simply contains the string "data" should be treated as a data field with an empty body.
		case bytes.Equal(line, bytes.TrimSuffix(headerData, []byte(":"))):
			e.Data = append(e.Data, byte('\n'))
		default:
			// We only care about data
		}
	}

	// Trim the last "\n" per the spec.
	e.Data = bytes.TrimSuffix(e.Data, []byte("\n"))

	return &e
}

func trimHeader(size int, data []byte) []byte {
	if data == nil || len(data) < size {
		return data
	}

	data = data[size:]
	// Remove optional leading whitespace
	if len(data) > 0 && data[0] == 32 {
		data = data[1:]
	}
	// Remove trailing new line
	if len(data) > 0 && data[len(data)-1] == 10 {
		data = data[:len(data)-1]
	}
	return data
}
