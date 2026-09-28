package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Sehlani042/grantide/internal/gateway"
)

func credential(args []string) error {
	f := flag.NewFlagSet("credential", flag.ContinueOnError)
	service := f.String("service", "", "configured service ID")
	method := f.String("method", "GET", "intended HTTP method")
	path := f.String("path", "/", "intended canonical path")
	id := f.String("id", "", "poll an existing credential request")
	cancel := f.Bool("cancel", false, "cancel the request specified by --id")
	wait := f.Duration("wait", 0, "wait for entry (default: return immediately)")
	if err := f.Parse(args); err != nil {
		return err
	}
	base := strings.TrimSuffix(os.Getenv("GRANTIDE_URL"), "/")
	token := os.Getenv("GRANTIDE_TOKEN")
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return errors.New("set GRANTIDE_URL to http://127.0.0.1:PORT")
	}
	if len(token) != 64 {
		return errors.New("set GRANTIDE_TOKEN to your agent token")
	}
	if *id != "" && (strings.ContainsAny(*id, "/?#%") || !strings.HasPrefix(*id, "cred_")) {
		return errors.New("invalid credential request ID")
	}
	if *cancel && *id == "" {
		return errors.New("--cancel requires --id")
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	send := func(verb, endpoint string, data []byte) ([]byte, error) {
		req, err := http.NewRequest(verb, base+endpoint, bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("gateway error (%d): %s", resp.StatusCode, b)
		}
		return b, nil
	}
	endpoint := "/v1/credential-requests"
	if *cancel {
		_, err := send("POST", endpoint+"/"+*id+"/cancel", []byte(`{}`))
		return err
	}
	var b []byte
	if *id != "" {
		b, err = send("GET", endpoint+"/"+*id, nil)
	} else {
		payload, _ := json.Marshal(gateway.Call{ServiceID: *service, Method: strings.ToUpper(*method), Path: *path})
		b, err = send("POST", endpoint, payload)
	}
	deadline := time.Now().Add(*wait)
	for {
		if err != nil {
			return err
		}
		var out gateway.CredentialRequest
		if err := json.Unmarshal(b, &out); err != nil {
			return err
		}
		if out.Status != "pending" || *wait <= 0 || time.Now().After(deadline) {
			fmt.Println(string(b))
			if out.Status != "pending" && out.Status != "fulfilled" {
				return fmt.Errorf("credential request %s", out.Status)
			}
			if out.Status == "pending" && *wait > 0 {
				return errors.New("wait elapsed; request remains pending until cancelled or expired")
			}
			return nil
		}
		time.Sleep(time.Second)
		b, err = send("GET", endpoint+"/"+out.ID, nil)
	}
}
