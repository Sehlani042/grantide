package gateway

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var blockedRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("64:ff9b::/96"),
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range blockedRanges {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

func executeHTTP(ctx context.Context, s Service, c Call) (*Result, error) {
	u, err := url.Parse(s.Origin)
	if err != nil {
		return nil, errors.New("invalid service origin")
	}
	u.Path = c.Path
	q := url.Values{}
	for k, v := range c.Query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	transport := &http.Transport{TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, DisableKeepAlives: true, MaxResponseHeaderBytes: 32 << 10}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid destination")
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("destination DNS lookup failed")
		}
		for _, ip := range ips {
			if !s.AllowPrivate && !publicIP(ip) {
				return nil, errors.New("private or reserved destination blocked")
			}
		}
		// Dial the checked address directly; do not perform a second DNS lookup.
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, c.Method, u.String(), strings.NewReader(c.Body))
	if err != nil {
		return nil, errors.New("cannot construct request")
	}
	req.Header.Set("Accept", "application/json")
	if c.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "Grantide/"+Version)
	if s.Secret != "" {
		if s.AuthType == "basic" {
			req.SetBasicAuth(s.Username, s.Secret)
		} else {
			req.Header.Set(s.AuthHeader, s.AuthPrefix+s.Secret)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("upstream connection failed or was blocked; check origin, network policy and TLS")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, errors.New("upstream redirect blocked; configure the final origin explicitly")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return nil, errors.New("upstream response read failed; request may have executed")
	}
	truncated := len(body) > MaxBody
	if truncated {
		body = body[:MaxBody]
	}
	text := string(body)
	if s.Secret != "" {
		if s.AuthType == "basic" {
			text = strings.ReplaceAll(text, base64.StdEncoding.EncodeToString([]byte(s.Username+":"+s.Secret)), "[REDACTED]")
		}
		text = strings.ReplaceAll(text, s.Secret, "[REDACTED]")
	}
	return &Result{Status: resp.StatusCode, Body: text, Truncated: truncated}, nil
}
