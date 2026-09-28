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
	"regexp"
	"strings"
	"time"
)

func webLogin(args []string) error {
	f := flag.NewFlagSet("web-login", flag.ContinueOnError)
	grant := f.String("grant", "", "operator-issued non-secret grant reference")
	id := f.String("id", "", "poll an existing run")
	list := f.Bool("list", false, "list grants issued to this agent")
	cancel := f.Bool("cancel", false, "cancel --id")
	wait := f.Duration("wait", 0, "bounded wait, at most 60s; does not cancel on timeout")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *wait < 0 || *wait > 60*time.Second {
		return errors.New("wait must be between 0 and 60s")
	}
	ref := regexp.MustCompile(`^login_[a-z0-9_]+$`)
	if (*id != "" && !ref.MatchString(*id)) || (*grant != "" && !ref.MatchString(*grant)) || (*cancel && *id == "") || (*list && (*id != "" || *grant != "")) || (*grant != "" && *id != "") {
		return errors.New("choose --list, --grant REF or --id RUN [--cancel]")
	}
	base := strings.TrimSuffix(os.Getenv("GRANTIDE_URL"), "/")
	token := os.Getenv("GRANTIDE_TOKEN")
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || len(token) != 64 {
		return errors.New("configure the loopback GRANTIDE_URL and agent GRANTIDE_TOKEN")
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	send := func(method, path string, v any) ([]byte, error) {
		var b []byte
		if v != nil {
			b, _ = json.Marshal(v)
		}
		req, _ := http.NewRequest(method, base+path, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		b, err = io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("gateway error (%d): %s", resp.StatusCode, b)
		}
		return b, err
	}
	var b []byte
	if *list {
		b, err = send("GET", "/v1/login-grants", nil)
	} else if *cancel {
		b, err = send("POST", "/v1/logins/"+*id+"/cancel", map[string]any{})
	} else if *id != "" {
		b, err = send("GET", "/v1/logins/"+*id, nil)
	} else if *grant != "" {
		b, err = send("POST", "/v1/logins", map[string]string{"grant_id": *grant})
	} else {
		return errors.New("choose --list, --grant REF or --id RUN")
	}
	deadline := time.Now().Add(*wait)
	for err == nil {
		var result struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if *list || *cancel {
			break
		}
		if json.Unmarshal(b, &result) != nil {
			return errors.New("invalid gateway response")
		}
		if result.Status != "executing" || *wait == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Second)
		b, err = send("GET", "/v1/logins/"+result.ID, nil)
	}
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
