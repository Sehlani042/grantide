package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// NewBrowserRunner is configured by the operator at process startup, never by an agent.
func NewBrowserRunner(node, script string) (LoginRunner, error) {
	if !filepath.IsAbs(node) || !filepath.IsAbs(script) {
		return nil, errors.New("browser node and worker paths must be absolute")
	}
	for _, p := range []string{node, script} {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			return nil, errors.New("browser worker runtime not found")
		}
	}
	return func(ctx context.Context, in LoginInput, emit func(LoginEvent) error, resume <-chan struct{}) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		cmd := exec.CommandContext(ctx, node, script)
		// Never inherit DEBUG, NODE_OPTIONS, inspector options, agent/operator tokens or proxies.
		for _, name := range []string{"PATH", "HOME", "TMPDIR", "TEMP", "SystemRoot", "DISPLAY", "XAUTHORITY", "PLAYWRIGHT_BROWSERS_PATH"} {
			if v, ok := os.LookupEnv(name); ok {
				cmd.Env = append(cmd.Env, name+"="+v)
			}
		}
		cmd.Stderr = io.Discard
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return errors.New("worker unavailable")
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return errors.New("worker unavailable")
		}
		var mu sync.Mutex
		send := func(v any) error { mu.Lock(); defer mu.Unlock(); return json.NewEncoder(stdin).Encode(v) }
		cmd.Cancel = func() error { return send(map[string]string{"command": "cancel"}) }
		cmd.WaitDelay = 3 * time.Second
		if cmd.Start() != nil {
			return errors.New("worker unavailable")
		}
		if send(in) != nil {
			cancel()
		}
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-resume:
					if send(map[string]string{"command": "resume"}) != nil {
						cancel()
						return
					}
				}
			}
		}()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 16<<10)
		valid := true
		for scanner.Scan() {
			var ev LoginEvent
			d := json.NewDecoder(strings.NewReader(scanner.Text()))
			d.DisallowUnknownFields()
			if d.Decode(&ev) != nil || d.Decode(new(any)) != io.EOF || emit(ev) != nil {
				valid = false
				cancel()
				break
			}
		}
		if scanner.Err() != nil {
			valid = false
			cancel()
		}
		err = cmd.Wait()
		_ = stdin.Close()
		if !valid || err != nil {
			return errors.New("controlled browser failed")
		}
		return nil
	}, nil
}
