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

func browserLogin(args []string) error {
	f := flag.NewFlagSet("browser-login", flag.ContinueOnError)
	site := f.String("site", "", "operator-configured website ID")
	browser := f.String("browser", "", "original browser/profile identity; include owning chat for in-app browsers")
	tab := f.String("tab", "", "original tab ID")
	thread := f.String("thread", "", "owning conversation ID")
	title := f.String("title", "", "owning conversation title (no secrets)")
	id := f.String("id", "", "poll or finish an existing handoff")
	cancel := f.Bool("cancel", false, "cancel --id")
	verify := f.Bool("verify", false, "record verification after the human finishes")
	authenticated := f.Bool("authenticated", false, "the original tab visibly shows authenticated state")
	origin := f.String("origin", "", "observed final HTTPS origin only, without path or query")
	wait := f.Duration("wait", 0, "bounded wait for human confirmation; default returns immediately")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *cancel && *verify {
		return errors.New("choose cancel or verify")
	}
	if (*cancel || *verify) && *id == "" {
		return errors.New("cancel/verify requires --id")
	}
	if *id != "" && (strings.ContainsAny(*id, "/?#%") || !strings.HasPrefix(*id, "browser_")) {
		return errors.New("invalid handoff ID")
	}
	base := strings.TrimSuffix(os.Getenv("GRANTIDE_URL"), "/")
	token := os.Getenv("GRANTIDE_TOKEN")
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || len(token) != 64 {
		return errors.New("set GRANTIDE_URL to http://127.0.0.1:PORT and GRANTIDE_TOKEN to the agent token")
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	send := func(method, path string, input any) ([]byte, error) {
		var b []byte
		if input != nil {
			b, _ = json.Marshal(input)
		}
		req, err := http.NewRequest(method, base+path, bytes.NewReader(b))
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
		b, err = io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("gateway error (%d): %s", resp.StatusCode, b)
		}
		return b, nil
	}
	endpoint := "/v1/browser-handoffs"
	if *cancel {
		_, err := send("POST", endpoint+"/"+*id+"/cancel", map[string]any{})
		return err
	}
	target := gateway.BrowserTarget{SiteID: *site, Browser: *browser, TabID: *tab, ThreadID: *thread, ThreadTitle: *title}
	var b []byte
	if *verify {
		b, err = send("POST", endpoint+"/"+*id+"/verify", map[string]any{"target": target, "origin": *origin, "authenticated": *authenticated})
	} else if *id != "" {
		b, err = send("GET", endpoint+"/"+*id, nil)
	} else {
		b, err = send("POST", endpoint, target)
	}
	deadline := time.Now().Add(*wait)
	for {
		if err != nil {
			return err
		}
		var r gateway.BrowserHandoff
		if err = json.Unmarshal(b, &r); err != nil {
			return err
		}
		if r.Status != "waiting" || *wait <= 0 || time.Now().After(deadline) {
			fmt.Println(string(b))
			switch r.Status {
			case "waiting":
				if *wait > 0 {
					return errors.New("wait elapsed; handoff remains waiting; do not inspect the browser")
				}
			case "verification_required":
				fmt.Fprintln(os.Stderr, "Human finished. Inspect the ORIGINAL tab for visible authenticated state, then record --verify. This does not approve purchases or other operations.")
			case "verified":
			default:
				return fmt.Errorf("browser handoff %s; do not resume browser actions", r.Status)
			}
			return nil
		}
		time.Sleep(time.Second)
		b, err = send("GET", endpoint+"/"+r.ID, nil)
	}
}
