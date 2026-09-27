# Grantide

Read PRD.md, DESIGN.md and NOW.md before changes. Preserve default deny, separate admin/agent authentication, exact pending-request approval and atomic lease consumption. No runtime state/tokens in tracked files. Use fake credentials and upstreams. Run `go test -race ./...` and `go vet ./...` for authorization/execution changes; visually verify GUI. This is an explicit HTTP gateway, not an OS sandbox; preserve that boundary in documentation.
