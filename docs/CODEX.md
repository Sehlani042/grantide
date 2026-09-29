# Codex credential handoff

Grantide can be invoked by Codex through its terminal tool using the CLI. This is an explicit integration, not a global interception of Codex login prompts. No MCP server or Codex configuration change is installed.

## Operator setup
1. Run Grantide and open the private operator login link.
2. In Connections, create a distinct Codex agent and give its token only to the agent environment as `GRANTIDE_TOKEN`. Set `GRANTIDE_URL` to the loopback console URL. Do not give Codex the operator token or the state directory in an isolated deployment.
3. Configure a trusted service origin and authentication type (`API Key / Header` or `HTTP Basic`), leaving the credential blank if it will be requested later.
4. Create a policy for the intended method/path and agent.

## Agent workflow
```sh
grantide credential --service example --method GET --path /profile
# Returns a request ID immediately; human fills the Credentials page.
grantide credential --id cred_EXAMPLE
# Optional bounded polling:
grantide credential --id cred_EXAMPLE --wait 30s
# Only after fulfilled, submit the actual operation (normal policy applies):
grantide call --service example --method GET --path /profile
```

Use the returned ID, not the example placeholder. With `--wait 0` (the default), pending is a successful submission, not evidence the credential was filled. A positive wait timing out exits nonzero and leaves the request pending until expiry or `grantide credential --id ID --cancel`. Do not retry an upstream operation automatically merely because credentials changed.

Suggested agent instruction: “When a configured service requires a credential, invoke `grantide credential` and direct me to the local Credentials page. Never request or read the password through chat, screenshots, browser evaluation, files or terminal input. Poll only request status. When fulfilled, submit the intended operation through `grantide call` and follow its approval result.”

The service credential is shared by all policy-authorized callers of that service, is encrypted on disk, and remains until replaced or cleared in Connections. The five-minute limit applies to filling the request, not storage duration. Operation leases separately control time and use counts. The status API never returns the password; this does not isolate secrets from an unrestricted same-OS-user agent.

For ordinary website passwords, QR and MFA, use the [browser login handoff](BROWSER-HANDOFF.md). The human completes entry on the original website; the originating agent verifies the original browser tab afterward. Grantide does not automatically intercept browser tools or wake other conversations. SSH/sudo password entry remains unsupported.

## Automatic saved-credential login (v0.4)

Use `grantide web-login`, not `browser-login`, when the operator wants saved credentials to be filled automatically. See [CONTROLLED-LOGIN.md](CONTROLLED-LOGIN.md). The operator saves an account and creates a scoped grant in **Automatic login**; the agent lists only its own grants and submits the nonsecret reference. MFA/CAPTCHA still requires the human. This uses a separate worker-owned browser and returns only status plus explicitly authorized structured fields. It does not log Safari/IAB into the account or grant general browser access.

When account entry is needed, use `web-login --setup --label "CloudCone · LA1"` and open the returned setup URL. Suggest a nonsecret label from the task context; leave it editable and preserve existing names. Operators can choose no-expiry grants (`seconds: 0`), independently of the use budget. Do not silently grant access when preparing an entry form.
