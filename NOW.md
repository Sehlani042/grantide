# Current state

2026-09-29: v0.3.0-alpha.1 adds browser login coordination. The GUI has a browser login inbox and operator-configured HTTPS sites with an agent allowlist; CLI `browser-login` binds agent/site/browser/profile/tab/conversation. Human confirmation and agent verification are separate; handoffs expire after 15 minutes, cancel, invalidate on config changes and disappear on restart. No browser password/OTP/cookie collection; no independent browser lock or automatic cross-chat messaging. Existing HTTP API rules and credential entry remain functional.

Verified this increment: race tests, vet, JS syntax and build; operator/agent separation, exact context/origin, duplicate/concurrency handling, failed-save behavior, lifecycle and no dispatch. In the local GUI, a clearly labeled fake handoff moved from waiting to verification_required, and the agent CLI observed that state. This is protocol/UI verification, not a real website sign-in.

Real scenario: the user selected the existing Tencent Cloud registration conversation. Its last recorded state is expired login. A separate local agent identity and Tencent console destination are prepared. Cross-chat message authorization has been requested; do not mark Tencent login as verified until the owning conversation performs the handoff, the human finishes and that agent sees the authenticated original tab. No real password has been entered, read or saved by Grantide.

Next: finish real original-tab integration after user authorization and login. Continue to distinguish local simulation, actual browser authentication and follow-on operations. Mobile soft keyboard and phone hardware have not been tested.

## Previous evidence

2026-09-27: v0.1.0-alpha.1 is implemented and locally verified. The standalone gateway, bilingual GUI, CLI, encrypted state, scoped policies, one-time approvals, temporary grants and audit are functional. Fake-upstream race/integration tests, vet and JavaScript syntax checks passed. Browser verification covered all four modes, approval, revocation, policy creation/editing, agent creation, credential configuration, language switching and a 390px viewport without horizontal overflow. CLI and real HTTP integration were exercised with fake credentials. macOS/Linux amd64+arm64 and Windows amd64 archives build successfully.

Public distribution: `Sehlani042/grantide`, experimental alpha, MIT. GitHub Actions checks each code push on Linux, macOS and Windows. The local preview uses simulated accounts only. Next product work: user feedback on the console, agent integration ergonomics, operation-specific adapters and an independently reviewed isolation/deployment model. SSH, shell and MCP are not implemented. Existing Agent Vault contribution remains a separate project.

2026-09-28: v0.2.0-alpha.1 adds human credential handoff: an agent CLI request appears in the bilingual GUI, the operator saves/rejects, and the agent polls only metadata. HTTP Basic and header tokens are supported. Saving is separate from authorization; it never executes the proposed operation. Credential scope is service-wide. Unit/race/integration verification passed, including failed-save rollback and fake Basic upstream authentication. Generic browser login, MCP, SSH and sudo adapters remain unimplemented. Local Codex demo credentials use private ignored files only.

GUI verification: fake Basic username/password entered and saved, modal password fields removed after close, CLI status returned fulfilled with no credential, subsequent operation waited for policy approval. CLI wait timeout cancelled the pending operation.
