package gateway

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkerPipeAndOutputBoundary(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node not available")
	}
	node, _ = filepath.Abs(node)
	for _, bad := range []bool{false, true} {
		script := filepath.Join(t.TempDir(), "fake.mjs")
		output := `process.stdout.write(JSON.stringify({status:'completed',authenticated:true})+'\n');`
		if bad {
			output = `process.stdout.write(input.password+'\n');`
		}
		code := `import readline from 'node:readline';const rl=readline.createInterface({input:process.stdin});rl.once('line',line=>{const input=JSON.parse(line);if(process.argv.join(' ').includes(input.password)||Object.values(process.env).includes(input.password)||process.env.DEBUG){process.exit(2)};console.error(input.password);` + output + `rl.close();process.stdin.destroy()});`
		if err := os.WriteFile(script, []byte(code), 0600); err != nil {
			t.Fatal(err)
		}
		runner, err := NewBrowserRunner(node, script)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		var statuses []string
		err = runner(ctx, LoginInput{Password: "fake-process-secret"}, func(ev LoginEvent) error { statuses = append(statuses, ev.Status); return nil }, make(chan struct{}))
		cancel()
		if bad {
			if err == nil || strings.Contains(err.Error(), "fake-process-secret") || len(statuses) != 0 {
				t.Fatal("raw worker output escaped")
			}
		} else if err != nil || len(statuses) != 1 || statuses[0] != "completed" {
			t.Fatal("pipe protocol failed", err, statuses)
		}
	}
}
