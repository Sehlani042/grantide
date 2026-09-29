package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var extensionID = regexp.MustCompile(`^[a-p]{32}$`)
var extensionRunID = regexp.MustCompile(`^login_[a-f0-9]{16}$`)

type hostMessage struct {
	Action string            `json:"action"`
	ID     string            `json:"id,omitempty"`
	Status string            `json:"status,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
}

// Chrome starts this command through a private native-host manifest. The
// extension receives only results of these three narrow operations, never the
// administrator bearer token used on the loopback hop.
func extensionHost(args []string) error {
	allowed := os.Getenv("GRANTIDE_EXTENSION_ID")
	dir := os.Getenv("GRANTIDE_DATA_DIR")
	if len(args) < 1 || !extensionID.MatchString(allowed) || args[0] != "chrome-extension://"+allowed+"/" || !filepath.IsAbs(dir) {
		return errors.New("unpaired native host")
	}
	var size uint32
	if err := binary.Read(os.Stdin, binary.NativeEndian, &size); err != nil {
		return err
	}
	if size == 0 || size > 65536 {
		return errors.New("invalid native message")
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(os.Stdin, body); err != nil {
		return err
	}
	var in hostMessage
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("invalid native message")
	}
	if in.Action != "poll" && !extensionRunID.MatchString(in.ID) {
		return errors.New("invalid run reference")
	}
	var method, path string
	var payload any
	switch in.Action {
	case "poll":
		if in.ID != "" || in.Status != "" || len(in.Fields) != 0 {
			return errors.New("invalid poll")
		}
		method, path = "GET", "/admin/extension/next"
	case "claim":
		if in.Status != "" || len(in.Fields) != 0 {
			return errors.New("invalid claim")
		}
		method, path = "POST", "/admin/extension/logins/"+in.ID+"/claim"
	case "complete":
		if len(in.Fields) > 8 {
			return errors.New("invalid fields")
		}
		method, path = "POST", "/admin/extension/logins/"+in.ID+"/complete"
		payload = map[string]any{"status": in.Status, "fields": in.Fields}
	default:
		return errors.New("unknown native action")
	}
	portBytes, err := os.ReadFile(filepath.Join(dir, "port"))
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(portBytes)))
	if err != nil || port < 1 || port > 65535 {
		return errors.New("invalid local port")
	}
	tokenBytes, err := os.ReadFile(filepath.Join(dir, "operator.token"))
	if err != nil || len(tokenBytes) != 64 {
		return errors.New("missing local operator credential")
	}
	var requestBody io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		requestBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), requestBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+string(tokenBytes))
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("local gateway unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("local gateway rejected native action")
	}
	result, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil {
		return err
	}
	if len(result) > 65536 || !json.Valid(result) {
		return errors.New("invalid local gateway response")
	}
	if err := binary.Write(os.Stdout, binary.NativeEndian, uint32(len(result))); err != nil {
		return err
	}
	_, err = os.Stdout.Write(result)
	return err
}
