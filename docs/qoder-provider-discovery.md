# Qoder provider discovery gate

**Decision:** Do not add a Qoder provider.

**Evaluated:** 2026-07-29

Qoder publishes supported Agent SDK and Cloud Agents interfaces, but neither is
a model-completion provider contract compatible with CLIProxyAPI's executor and
translator boundary. The supported interfaces run a Qoder-owned agent loop,
including model selection, tool execution, sessions, and workspace access. The
direct inference protocol in the issue's reference fork is explicitly based on
reverse engineering and depends on private endpoints and client internals.

The discovery gate therefore fails. This change adds no auth record, login
command, model alias, executor, translator, registry entry, or Docker behavior.
That is the required outcome when stable supported contracts cannot satisfy the
issue without unsafe credential extraction or speculative provider code.

## Compatibility matrix

| Required area | Supported, reproducible Qoder surface | First-class provider result |
| --- | --- | --- |
| Authentication | Qoder CLI and Agent SDK document Personal Access Tokens (PATs), including environment-variable injection. A PAT is created with operator-selected scopes and expiry. The SDK does not refresh PATs; the host starts a new query session after replacing an expired token. Local CLI session reuse is documented but discouraged for stateless CI. | Safe authentication exists for the supported agent products, but no supported auth profile was found for direct model inference. CLI credential import is unnecessary and must not be added. |
| API contract | The TypeScript Agent SDK starts `qodercli` as a child process (or uses its packaged worker runtime). The Cloud Agents REST API manages agents, environments, sessions, events, files, tools, and other agent resources. Its documentation labels the API Beta and warns that signatures may change. | Both contracts are agent runtimes, not a stateless chat/completions API. Adapting either would replace the client's requested completion with a Qoder-controlled multi-call agent session. |
| Model catalog | Agent SDK exposes `getAvailableModels()` at runtime and documents fixed or per-LLM-call dynamic model policy. Product docs expose moving tiers such as Auto, Ultimate, Performance, Efficient, and Lite rather than promising stable backing-model identifiers. | Runtime discovery is supported inside a Qoder agent session. It does not define stable model IDs or a compatibility lifecycle for a direct provider registry. |
| Streaming | Agent SDK documents partial `stream_event` messages for text, reasoning, and incremental tool arguments. Cloud Agents exposes session-event SSE. | These are agent lifecycle/message events. They are not a documented direct inference stream whose deltas, usage, termination, and errors can be translated deterministically at the existing provider boundary. |
| Tool semantics | Agent SDK owns built-in tools, MCP tools, permissions, hooks, and execution results. The Cloud product similarly runs managed agents and their tools. | Tool calls are part of Qoder's agent loop, not a documented pass-through model tool-call protocol. Using this surface could execute tools instead of returning the tool call requested by an OpenAI/Claude client. |
| Quotas | Qoder documents credit metering by successful model calls and total input/output use. Agent requests may make multiple model calls. When premium credits are depleted, the product switches to daily-limited basic models. Actual deductions may vary with real-time usage. | The product quota is documented, but there is no provider-level rate-limit, retry, cooldown, or error contract for safely rotating accounts. One incoming request also need not equal one billed model call. |
| Token lifetime | PAT expiry is selected when the token is created. The Agent SDK reports auth expiry at most once per query session and explicitly performs no automatic PAT refresh. | A future integration could safely accept a PAT from a secret boundary, but cannot implement the issue's requested isolated refresh without an official refresh contract. Browser/device session token lifetime is not a supported provider contract. |

## Primary evidence

The following Qoder-owned pages and packages were inspected without logging in,
reading local application state, or sending credentials:

- [Qoder CLI quick start](https://docs.qoder.com/en/cli/quick-start) documents
  browser login, direct PAT login, and `QODER_PERSONAL_ACCESS_TOKEN` for
  non-interactive use.
- [Agent SDK authentication](https://docs.qoder.com/en/cli/sdk/authentication)
  documents PAT creation with chosen scopes and expiry, environment/secret
  injection, one auth-expiry callback per query session, and the absence of
  automatic PAT refresh. It recommends PATs rather than local CLI session reuse
  for automated environments and says not to log tokens.
- [Agent SDK streaming output](https://docs.qoder.com/en/cli/sdk/streaming-output)
  defines incremental agent messages for text, reasoning, and tool arguments.
- [Agent SDK model selection](https://docs.qoder.com/en/cli/sdk/model-policy)
  documents fixed and dynamic per-LLM-call policy and runtime model discovery.
- [Agent SDK tools](https://docs.qoder.com/en/cli/sdk/tools) documents Qoder's
  built-in/MCP tools and permission-controlled execution.
- Qoder's official [Agent SDK sample repository at `d06b48d`](https://github.com/QoderAI/qoder-agent-sdk-samples/tree/d06b48d050048373a473c5842d9ad368864323c3)
  pins TypeScript SDK 1.0.16 and Python SDK 1.0.10 and demonstrates PAT auth,
  streaming, runtime model discovery, and agent-owned tools.
- The published [`@qoder-ai/qoder-agent-sdk` 1.0.16 package](https://www.npmjs.com/package/@qoder-ai/qoder-agent-sdk/v/1.0.16)
  states that `query()` launches `qodercli` as a child process by default and
  that installation downloads the platform CLI. This is an agent/runtime
  dependency, not a Go HTTP provider contract.
- [Cloud Agents API overview](https://docs.qoder.com/cloud-agents/api/conventions/overview)
  identifies `https://api.qoder.com/api/v1/cloud` as an agent-management API,
  labels it Beta, and warns of possible signature adjustments.
- [Cloud Agents authentication](https://docs.qoder.com/cloud-agents/api/conventions/authentication)
  documents scoped PAT bearer authentication for that API.
- [Credits](https://docs.qoder.com/Credits) documents variable credit use,
  multiple model calls behind an Agent request, successful-call charging, and
  fallback to a daily-limited basic model service.
- [Model Selector](https://docs.qoder.com/user-guide/chat/model-tier-selector)
  documents product tiers and runtime routing rather than a stable backing
  model protocol.
- [Qoder Terms of Service, sections 3.2.6 and 3.2.8-3.2.10](https://qoder.com/product-service)
  prohibit circumventing security/authentication, tampering with service
  software, reverse engineering, and automated scraping/mining. The page was
  last updated 2026-04-29 when evaluated.

The documented Cloud API does not fill the model-provider gap. Qoder describes
it as "Agent as a Service": callers create agents and sessions and receive
session events. It is a plausible future integration at a separate agent or
plugin boundary, but it cannot preserve a single completion request's message,
tool, usage, and retry semantics.

## Reference-fork assessment

The community fork was used only to identify claims to verify. It was not
copied, executed, or treated as a protocol specification. At evaluated revision
[`7c764d5`](https://github.com/kaitranntt/CLIProxyAPIPlus/tree/7c764d58002c09e4690649f8f782626c579ead3c):

- [`internal/auth/qoder/cosy.go`](https://github.com/kaitranntt/CLIProxyAPIPlus/blob/7c764d58002c09e4690649f8f782626c579ead3c/internal/auth/qoder/cosy.go)
  says its RSA public key was extracted from Qoder IDE 0.9, cites another
  reverse-engineered implementation, and recreates private COSY signing with
  fixed client, machine-type, and machine-OS values.
- [`internal/auth/qoder/qoder_auth.go`](https://github.com/kaitranntt/CLIProxyAPIPlus/blob/7c764d58002c09e4690649f8f782626c579ead3c/internal/auth/qoder/qoder_auth.go)
  hard-codes undocumented `.qoder.sh` login, token, refresh, user-info, and
  inference endpoints and comments that server behavior accepts several
  imitated IDE/CLI versions.
- [`internal/auth/qoder/api.go`](https://github.com/kaitranntt/CLIProxyAPIPlus/blob/7c764d58002c09e4690649f8f782626c579ead3c/internal/auth/qoder/api.go)
  calls its source reverse engineering and hard-codes the private
  `agent_chat_generation` route and observed model identifiers.
- [`internal/runtime/executor/helps/qoder_encoding.go`](https://github.com/kaitranntt/CLIProxyAPIPlus/blob/7c764d58002c09e4690649f8f782626c579ead3c/internal/runtime/executor/helps/qoder_encoding.go)
  implements an extracted custom body encoding. The corresponding Qoder commit
  describes its purpose as bypassing Alibaba Cloud WAF.
- The fork's [Qoder auth commit history](https://github.com/kaitranntt/CLIProxyAPIPlus/commits/main/internal/auth/qoder)
  changed token response shapes, client versions, signing, model discovery,
  model configuration, quota parsing, and WAF handling repeatedly between
  March and June 2026. That is reproducible evidence of an observed moving
  target, not a compatibility promise.

Using these internals would conflict with both the issue's non-goals and
Qoder's published terms. It would also require persisting machine tokens and
refresh material for an authentication profile Qoder has not authorized for a
third-party proxy. No credentials were extracted from Qoder CLI/IDE files, a
browser profile, process memory, logs, or network interception during this
evaluation.

## Acceptance assessment

| Issue acceptance criterion | Result |
| --- | --- |
| Written discovery result identifies stable protocol and security assumptions | Satisfied by this report. The stable surfaces are PAT-authenticated Agent SDK and Beta Cloud Agents contracts; neither is a compatible model provider protocol. PATs belong in environment/secret storage, must never be logged, have operator-selected expiry, and are not auto-refreshed by the SDK. |
| No credential or browser workaround is logged or persisted improperly | Satisfied for discovery. No login was performed and no credential source was accessed or written. A browser/device token importer, extracted COSY material, or imitated client identity is explicitly rejected. |
| Minimal authenticated request, streaming response, and tool call pass deterministic tests | Not implementable at the provider boundary from a supported contract. Agent SDK examples establish those behaviors only for a Qoder-owned agent loop. Fixtures for the fork's private inference route would canonize reverse-engineered behavior and violate the discovery gate. No misleading tests were added. |
| Failure to meet the discovery gate produces no partial provider code | Satisfied. Only this issue-specific report is added. |

The broader requested implementation is correspondingly blocked: isolated
refresh has no supported refresh API; account cooldown lacks provider error and
rate-limit semantics; Docker login would either require a supplied PAT or CLI
runtime dependency; and registry/stream/tool fixtures cannot be derived from a
versioned direct inference schema.

## Reopen implementation when

Implementation should be reconsidered only when Qoder provides all of the
following, or explicitly confirms an equivalent supported integration:

1. A versioned model-completion endpoint intended for third-party proxies,
   with an authentication profile that does not impersonate Qoder IDE/CLI or
   extract their credentials.
2. Stable request, non-streaming response, streaming event, tool-call, usage,
   quota, and error schemas with a compatibility/deprecation policy.
3. A stable model-discovery contract and identifiers suitable for a provider
   registry, including capability metadata.
4. A documented token lifecycle for unattended servers: PAT rotation or a
   refresh grant, expiry/revocation errors, scopes, and safe multi-account use.
5. Written support for self-hosted compatibility proxies and account rotation,
   plus official mock/conformance fixtures or test credentials covering an
   authenticated completion, stream, tool call, quota failure, expiry, and
   revocation.

Alternatively, a separately scoped Cloud Agents integration could proceed at
an agent/plugin boundary if its Beta status and multi-step session semantics
are acceptable. It should not be registered as a drop-in Qoder model provider.

## Reproduction notes

These credential-free checks returned HTTP 200 on 2026-07-29:

```sh
curl -fsS https://docs.qoder.com/en/cli/sdk/authentication >/dev/null
curl -fsS https://docs.qoder.com/en/cli/sdk/streaming-output >/dev/null
curl -fsS https://docs.qoder.com/en/cli/sdk/model-policy >/dev/null
curl -fsS https://docs.qoder.com/cloud-agents/api/conventions/overview >/dev/null
curl -fsS https://qoder.com/product-service >/dev/null
gh api repos/QoderAI/qoder-agent-sdk-samples/commits/main --jq .sha
npm view @qoder-ai/qoder-agent-sdk@1.0.16 version description dependencies
```

No OpenAPI document was exposed at the reverse-engineered inference hosts:
credential-free requests to `https://openapi.qoder.sh/openapi.json`,
`https://openapi.qoder.sh/swagger.json`, `https://api3.qoder.sh/openapi.json`,
and `https://api3.qoder.sh/swagger/index.html` each returned HTTP 404. A 404
alone does not prove an API is unsupported; combined with the official agent
documentation, fork comments, protocol churn, and terms, it confirms there is
no public specification at the locations claimed by the fork.
