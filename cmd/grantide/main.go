package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Sehlani042/grantide/internal/gateway"
	"github.com/Sehlani042/grantide/internal/webui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "grantide:", err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		fmt.Println("Grantide · 允界\n\n  grantide serve [--demo] [--data-dir PATH] [--port PORT]\n  grantide call --service ID --method GET --path /metrics [--body JSON]\n  grantide version\n\nAgent CLI reads GRANTIDE_URL and GRANTIDE_TOKEN from the environment.")
		return nil
	}
	switch os.Args[1] {
	case "serve":
		return serve(os.Args[2:])
	case "call":
		return call(os.Args[2:])
	case "version":
		fmt.Println(gateway.Version)
		return nil
	default:
		return errors.New("unknown command")
	}
}

func serve(args []string) error {
	base, _ := os.UserConfigDir()
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	dir := f.String("data-dir", filepath.Join(base, "grantide"), "private state directory")
	demo := f.Bool("demo", false, "seed fake demo services on empty state")
	port := f.Int("port", 0, "loopback port; 0 remembers an available random port")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *port < 0 || *port > 65535 {
		return errors.New("invalid port")
	}
	if err := os.MkdirAll(*dir, 0700); err != nil {
		return err
	}
	lockPath := filepath.Join(*dir, "server.lock")
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("data directory locked; stop its server first (after a crash, verify no process is running before removing %s)", lockPath)
	}
	_, _ = fmt.Fprint(lock, os.Getpid())
	_ = lock.Close()
	defer os.Remove(lockPath)
	store, err := gateway.OpenStore(*dir)
	if err != nil {
		return err
	}
	operator, err := gateway.OperatorToken(*dir)
	if err != nil {
		return err
	}
	engine, err := gateway.NewEngine(store, *demo)
	if err != nil {
		return err
	}
	selected := *port
	portFile := filepath.Join(*dir, "port")
	if selected == 0 {
		if b, e := os.ReadFile(portFile); e == nil {
			selected, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", selected))
	if err != nil && *port == 0 {
		listener, err = net.Listen("tcp4", "127.0.0.1:0")
	}
	if err != nil {
		return err
	}
	defer listener.Close()
	actual := listener.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(portFile, []byte(strconv.Itoa(actual)), 0600); err != nil {
		return err
	}
	address := "http://" + listener.Addr().String()
	// Keep the operator token out of terminal logs and process arguments.
	loginPath := filepath.Join(*dir, "operator-url")
	if err := os.WriteFile(loginPath, []byte(address+"/#token="+operator), 0600); err != nil {
		return err
	}
	fmt.Printf("Grantide %s\nConsole: %s\nOperator login link (private file): %s\nDemo: %t\n", gateway.Version, address, loginPath, *demo)
	server := &http.Server{Handler: gateway.Handler(engine, operator, listener.Addr().String(), webui.Handler()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func call(args []string) error {
	f := flag.NewFlagSet("call", flag.ContinueOnError)
	service := f.String("service", "", "configured service ID")
	method := f.String("method", "GET", "HTTP method")
	path := f.String("path", "/", "canonical path")
	body := f.String("body", "", "JSON request body")
	query := f.String("query", "", "URL-encoded query")
	wait := f.Duration("wait", 5*time.Minute, "maximum wait for review")
	if err := f.Parse(args); err != nil {
		return err
	}
	base := strings.TrimSuffix(os.Getenv("GRANTIDE_URL"), "/")
	token := os.Getenv("GRANTIDE_TOKEN")
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return errors.New("set GRANTIDE_URL to the loopback console origin")
	}
	if u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
		return errors.New("this alpha CLI connects only to http://127.0.0.1")
	}
	if len(token) != 64 {
		return errors.New("set GRANTIDE_TOKEN to your agent token")
	}
	q, err := url.ParseQuery(*query)
	if err != nil {
		return err
	}
	flat := map[string]string{}
	for k, v := range q {
		if len(v) != 1 {
			return errors.New("duplicate query keys are unsupported")
		}
		flat[k] = v[0]
	}
	payload, _ := json.Marshal(gateway.Call{ServiceID: *service, Method: strings.ToUpper(*method), Path: *path, Query: flat, Body: *body})
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	send := func(verb, endpoint string, data []byte) (gateway.Request, error) {
		req, _ := http.NewRequest(verb, base+endpoint, bytes.NewReader(data))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return gateway.Request{}, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		if err != nil {
			return gateway.Request{}, err
		}
		var out gateway.Request
		if err = json.Unmarshal(b, &out); err != nil {
			return out, err
		}
		if out.ID == "" {
			return out, fmt.Errorf("gateway error (%d): %s", resp.StatusCode, b)
		}
		return out, nil
	}
	out, err := send("POST", "/v1/requests", payload)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(*wait)
	if out.Status == "pending" {
		fmt.Fprintln(os.Stderr, "Waiting for operator approval:", out.ID)
	}
	for out.Status == "pending" || out.Status == "executing" {
		if time.Now().After(deadline) {
			_, _ = send("POST", "/v1/requests/"+out.ID+"/cancel", []byte(`{}`))
			return errors.New("wait expired; cancellation requested (an in-flight call may already have executed)")
		}
		time.Sleep(time.Second)
		out, err = send("GET", "/v1/requests/"+out.ID, nil)
		if err != nil {
			return err
		}
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	if out.Status != "completed" {
		return fmt.Errorf("request %s: %s", out.Status, out.Reason)
	}
	if out.Result != nil && out.Result.Status >= 400 {
		return fmt.Errorf("upstream returned HTTP %d", out.Result.Status)
	}
	return nil
}
