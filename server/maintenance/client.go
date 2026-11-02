package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Execute uses the private descriptor created by the running server. Its token
// is only sent to a literal loopback address with proxying and redirects disabled.
func Execute(ctx context.Context, runtimePath, action string, minutes int, id string) (Reply, error) {
	if action != "announce" && action != "cancel" && action != "status" && action != "update" {
		return Reply{}, errors.New("unknown maintenance command")
	}
	payload, err := os.ReadFile(filepath.Join(runtimePath, descriptorFilename))
	if err != nil {
		return Reply{}, fmt.Errorf("controlDiscover: %w", err)
	}
	var endpoint descriptor
	err = json.Unmarshal(payload, &endpoint)
	if err != nil {
		return Reply{}, fmt.Errorf("controlDecode: %w", err)
	}
	host, port, err := net.SplitHostPort(endpoint.Address)
	if err != nil || host != "127.0.0.1" || port == "" || len(endpoint.Token) != 64 {
		return Reply{}, errors.New("invalid local maintenance descriptor")
	}
	payload, err = json.Marshal(command{Minutes: minutes, ID: id})
	if err != nil {
		return Reply{}, fmt.Errorf("requestEncode: %w", err)
	}
	method := http.MethodPost
	if action == "status" {
		method = http.MethodGet
		payload = nil
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+endpoint.Address+"/"+action, bytes.NewReader(payload))
	if err != nil {
		return Reply{}, fmt.Errorf("requestCreate: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+endpoint.Token)
	req.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 90 * time.Second, CheckRedirect: rejectRedirect}
	response, err := client.Do(req)
	if err != nil {
		return Reply{}, fmt.Errorf("controlRequest: %w", err)
	}
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, 65536))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		return Reply{}, fmt.Errorf("responseRead: %w", errors.Join(readErr, closeErr))
	}
	var reply Reply
	err = json.Unmarshal(payload, &reply)
	if err != nil {
		return Reply{}, fmt.Errorf("responseDecode: %w", err)
	}
	if response.StatusCode != http.StatusOK || reply.Error != "" {
		return reply, fmt.Errorf("maintenance command failed: status %d: %s", response.StatusCode, reply.Error)
	}
	return reply, nil
}

func rejectRedirect(req *http.Request, previousRequests []*http.Request) error {
	return http.ErrUseLastResponse
}
