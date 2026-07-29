# Kiro / AWS CodeWhisperer provider discovery gate

**Decision:** Do not add the provider yet.

**Evaluated:** 2026-07-29

This gate evaluates issue #8 against its requirement to use only documented,
stable authentication and upstream flows. AWS documents the authentication
building blocks, but there is no documented third-party Kiro or CodeWhisperer
chat API contract that supports the requested proxy behavior. Importing either
referenced fork would therefore make production behavior depend on observed
Kiro IDE internals.

No provider code, model aliases, login command, or credential importer is
registered by this change.

## Identity paths are distinct

| Path | Intended user | Required input | Supported authentication primitive | Gate result |
| --- | --- | --- | --- | --- |
| AWS Builder ID | Individual developer | No organization start URL | AWS SSO OIDC device authorization against the Builder ID issuer | Authentication is documented; Kiro proxy use is not supported yet. |
| AWS IAM Identity Center | Organization-managed user | Organization start URL and its configured region | Region-specific AWS SSO OIDC device authorization | Authentication is documented; access also depends on administrator assignment and the organization's Kiro subscription. Kiro proxy use is not supported yet. |

Builder ID is not an IAM Identity Center tenant. A future implementation must
not infer the enterprise path from an arbitrary token or silently fall back
from an organization start URL to Builder ID. Login must ask for one mode and,
for IAM Identity Center, require both the start URL and region.

Kiro's authentication documentation explicitly presents Builder ID as an
individual path and IAM Identity Center as an enterprise path. AWS's SSO OIDC
API documents `RegisterClient`, `StartDeviceAuthorization`, and `CreateToken`,
including refresh-token behavior and dynamic client-secret expiry. Those are
suitable foundations for a localhost-safe, headless-friendly device flow.
They do not by themselves grant or document a general Kiro inference API.

## Evidence

Sources were inspected without performing a login or sending credentials.
Permalinks identify the exact third-party revisions evaluated.

### Stable, documented material

- [Kiro CLI authentication methods](https://kiro.dev/docs/cli/authentication/)
  distinguishes AWS Builder ID from AWS IAM Identity Center.
- [AWS IAM Identity Center OIDC API](https://docs.aws.amazon.com/singlesignon/latest/OIDCAPIReference/Welcome.html)
  documents the public device authorization protocol.
- [RegisterClient](https://docs.aws.amazon.com/singlesignon/latest/OIDCAPIReference/API_RegisterClient.html)
  documents dynamically registered client credentials and
  `clientSecretExpiresAt`.
- [StartDeviceAuthorization](https://docs.aws.amazon.com/singlesignon/latest/OIDCAPIReference/API_StartDeviceAuthorization.html)
  documents the verification URI, user code, poll interval, and expiry.
- [CreateToken](https://docs.aws.amazon.com/singlesignon/latest/OIDCAPIReference/API_CreateToken.html)
  documents device-code and refresh-token grants and their terminal errors.
- AWS's open-source [Amazon Q Developer CLI authentication implementation](https://github.com/aws/amazon-q-developer-cli/blob/15cc8f3cd18c4272925ce1c7053268eedff1ea0a/crates/chat-cli/src/auth/builder_id.rs)
  independently keeps Builder ID and IAM Identity Center token types distinct.

### Reference-fork findings

The referenced Plus repositories contain working examples, but they do not
establish a supported contract:

- The qqlightspeed fork registers itself as `Kiro IDE`, requests Kiro-specific
  scopes, and copies an IDE user agent in its
  [SSO OIDC client](https://github.com/qqlightspeed/CLIProxyAPIPlus/blob/60936b51856bd321cb34d918d14d42446c708d91/internal/auth/kiro/sso_oidc.go).
- Its [executor](https://github.com/qqlightspeed/CLIProxyAPIPlus/blob/60936b51856bd321cb34d918d14d42446c708d91/internal/runtime/executor/kiro_executor.go)
  hard-codes Kiro IDE-style user-agent and agent-mode headers, switches between
  observed CodeWhisperer and Amazon Q endpoints, and describes behavior as
  based on other reverse-engineered projects.
- Its [CodeWhisperer client](https://github.com/qqlightspeed/CLIProxyAPIPlus/blob/60936b51856bd321cb34d918d14d42446c708d91/internal/auth/kiro/codewhisperer_client.go)
  logs complete upstream response bodies at debug level. That cannot satisfy
  this repository's no-secret-leakage boundary because response bodies can
  contain account data or service diagnostics.
- The slepoh fork continues to patch refresh, token-file, fingerprint, and
  endpoint behavior after import; its evaluated tree is
  [cbe5695](https://github.com/slepoh/CLIProxyAPIPlus/tree/cbe56955a9250e457f6ea8268a96e6d6e8e59d59/internal/auth/kiro).
  This is evidence of a moving observed implementation, not a stable public
  compatibility promise.
- AWS publishes generated clients and service models in official repositories,
  including
  [GenerateAssistantResponse](https://github.com/aws/aws-toolkit-vscode/blob/bc7a5e84eec5bb66a53d3c206b285686065a5130/src.gen/%40amzn/codewhisperer-streaming/src/commands/GenerateAssistantResponseCommand.ts).
  This makes protocol research possible, but the generated internal package is
  not public documentation that Kiro subscriptions may be exposed through a
  third-party OpenAI-compatible account pool. It also does not document a Kiro
  model registry or compatibility lifecycle.

The issue explicitly excludes undocumented Kiro IDE token import. The same
stability rule must apply to copied IDE identity headers, fingerprints,
private scopes, inferred model aliases, and endpoint fallback behavior.

## Acceptance assessment

| Issue requirement | Current evidence | Result |
| --- | --- | --- |
| Distinguish Builder ID and enterprise authentication | Kiro and AWS documentation identify separate individual and organization flows. | Pass for design; documented above. |
| Store and refresh credentials without logging secrets | SSO OIDC defines required token/client fields, but the reference implementation logs full bodies and no live end-to-end redaction evidence is available without unsupported Kiro use. | Blocked. |
| Provider executor and OpenAI-compatible translation | Reference forks implement observed payloads and event-stream parsing, but no Kiro third-party API compatibility contract was found. | Blocked. |
| Model registry entries | Official clients obtain service models dynamically; the forks also hard-code and heuristically map names. No stable Kiro model contract was found. | Blocked. |
| Account rotation and cooldown | CLIProxyAPI already has generic scheduling and cooldown facilities, but classifying Kiro auth, quota, model, and terminal stream errors requires a supported error contract. | Blocked. |
| Expired/revoked credentials fail once with actionable errors | SSO OIDC terminal errors are documented. Upstream Kiro request and stream error semantics are not documented sufficiently to prove bounded retry and cooldown behavior. | Blocked. |
| Docker login and restart preserve accounts | Device flow needs no inbound callback and is Docker-safe in principle. Persistence cannot be accepted until the credential schema and upstream usage are supportable. | Blocked. |
| Deterministic auth, streaming, tools, expiry, and error tests | Fixtures derived only from reverse engineering would pin undocumented behavior rather than a supported contract. | Blocked. |

## Required security and persistence contract

If the upstream gate is cleared, a completed device login may write exactly one
mode-specific auth record under the configured mounted auth directory with
permissions `0600`. The record needs only:

- provider and authentication mode;
- IAM Identity Center start URL and region only for the enterprise mode;
- OIDC client ID, client secret, and client-secret expiry;
- access token, refresh token, and access-token expiry;
- stable non-secret account label supplied by the service, when available.

CLI and management responses must expose only the mode, account label, expiry,
and redacted token presence. They must never print or log the OIDC `deviceCode`, bearer/refresh tokens,
client secrets, authorization headers, token response bodies, or token-bearing
auth records. The user-facing `userCode` and verification URL may be printed
only during the bounded login session. Auth files,
Kiro IDE caches, and AWS CLI caches must not be imported.

Refresh must be serialized per account and attempted at most once for a failed
request. `invalid_grant`, expired client registration, revocation, or an
unauthorized terminal response must disable that credential with an actionable
re-login message; it must not enter a refresh/request loop. Quota and transient
errors may use the existing cooldown machinery only after supported retry
semantics are known.

## Docker-safe future behavior

Use device authorization for both modes. Print the verification URL and
user-facing code, poll with the confidential `deviceCode` at the
server-provided interval and a bounded credential-acquisition deadline, and
require no inbound port. This works from
Docker without a public callback or host-network assumptions. The auth
directory remains the only mounted persistence boundary, so restart reloads the
same account record and refresh registration.

A localhost callback may be added only if Kiro documents a loopback redirect
for third-party clients. It must bind loopback only, use PKCE and state, and be
opt-in for containers. A public callback must never be the default.

## Unblock criteria

Implementation can proceed when AWS or Kiro publishes all of the following:

1. A supported Kiro or Amazon Q Developer endpoint and authentication profile
   for third-party clients, without impersonating an official IDE/CLI.
2. Supported request, event-stream, tool-call, usage, and error schemas with a
   compatibility or versioning policy.
3. A model-discovery contract or stable model identifiers.
4. Confirmation that Builder ID and assigned IAM Identity Center subscriptions
   may be used by a self-hosted compatibility proxy and account pool.
5. Non-production test credentials or an official mock/conformance fixture
   covering login, refresh, revocation, streaming, tools, quota, and restart.

Until then, adding the provider would violate the issue's documented-and-stable
flow constraint and its secret-safety acceptance criteria. The correct outcome
is this discovery gate, not a partial fork import.
