# Grantide / 允界 · v0.1

## Goal
An independently developed, local-first permission gateway with a browser GUI. An operator can let an AI agent call an API automatically, require approval for each call, grant temporary access, or forbid an operation. Demonstrate every mode against a fake upstream with observable call counts.

## Users and flow
Developers running coding agents and automation. The operator adds an agent and an HTTP service, configures scoped rules, and gives the agent its own gateway token. The agent submits an exact request. The gateway executes, queues for review, or denies it. The operator reviews method, destination, query and body, grants one call or a bounded lease, and can revoke access. The agent polls for the result.

## MVP scope
- Self-contained Go binary, embedded responsive bilingual Web GUI.
- Agent identities/revocation; encrypted service credentials; service enable/disable.
- Rules scoped by agent, service, method and path prefix: auto, approval, lease, deny.
- Deny overrides other rules. Overlapping other rules select the most restrictive mode, then the narrowest scope. No match denies.
- Lease bound to agent and configuration revision, with expiry and atomic call limit; optional rule expiry for temporary pre-authorized access.
- Exact held-request approval, rejection, timeout, cancellation and result polling.
- GUI rules, approval inbox, active leases, services, agents, audit and sandbox playground.
- CLI integration, safe demo, tests and independent public GitHub repository.

## Boundaries
v0.1 brokers explicit JSON HTTP API requests. It does not intercept all desktop traffic, execute SSH/shell commands, provide MCP transport or protect against an agent with access to the operator's OS account/data directory. Keep the gateway and its files outside the agent sandbox. Hosted multi-user management and distributed leases are future work. No real provider credentials in development/demo.

## Acceptance
Auto calls reach upstream once. Approval calls reach it only after approval, with exactly the displayed payload. Denied/rejected/expired calls never reach it. Leases enforce actor, scope, time and concurrent use counts. Configuration changes invalidate pending authorizations and leases. Agents cannot use admin endpoints or see other agents' requests. No credentials in snapshots. Redirects cannot forward credentials. Private destinations require operator opt-in. Configuration persists; pending calls and leases do not survive restart. GUI exercises all modes. Race tests, vet and browser verification pass.

## Decisions / DoR
Go standard library plus native browser modules; no cloud dependency. Encrypted private local state; loopback binding. MIT license, experimental alpha. Project name changed from Agent Permit to Grantide at the user's request for a product name. Goal, scope, flow, constraints, risks and acceptance are defined; no blocking unanswered requirement.
