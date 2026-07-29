# CodeBuddy provider viability evaluation (issue #11)

Evaluated on 2026-07-29. **Outcome: stop at discovery; do not add a native
CodeBuddy provider.** CodeBuddy has supported integration surfaces, but its
supported surfaces are the CodeBuddy CLI, a Preview Agent SDK, and a local
agent HTTP gateway. The raw hosted model endpoint used by the reference fork
is not a documented or compatibility-guaranteed provider API.

This distinction matters: wrapping the supported Agent SDK would expose an
agent that reads files and runs tools, not a stateless model provider. Copying
the fork's hosted `/v2/chat/completions` behavior would instead make this
project depend on private implementation details.

## Stable and reproducible evidence

### Primary sources

1. The official [quick start][quick-start] documents browser login and names
   four environments: China (`copilot.tencent.com`), International
   (`codebuddy.ai`), enterprise/self-hosted, and Tencent-internal iOA.
2. The official [Agent SDK guide][sdk] supports existing browser credentials,
   user-created `CODEBUDDY_API_KEY` credentials, and OAuth 2.0 Client
   Credentials for enterprise applications. It labels the SDK **Preview** and
   says interfaces and behavior may change. The [SDK reference][sdk-ts]
   exposes an async message stream, tool permission callbacks, model discovery,
   and login/logout, but also labels direct authentication APIs unstable.
3. The official [models guide][models] documents OpenAI-format custom-model
   inputs and per-model `supportsToolCall`; it does not document CodeBuddy's
   hosted model service as an OpenAI-compatible public API. The built-in model
   catalog and credit multipliers are shipped as product configuration rather
   than exposed as a stable provider catalog.
4. The official [cost guide][costs] documents token accounting and plan usage.
   It does not define hosted-provider rate limits, quota headers, or retry
   semantics.
5. The official [HTTP API guide][http-api] documents a **local** server started
   with `codebuddy --serve`. Its public contracts are agent-oriented
   `/api/v1/*` REST and ACP-over-SSE endpoints. It explicitly marks
   `/internal/*` as having no compatibility guarantee. It does not document a
   hosted chat-completions or token-refresh endpoint.
6. Tencent's public npm artifact [`@tencent-ai/codebuddy-code@2.129.0`][npm]
   independently reproduces the docs and regional product configuration. The
   package records International `https://www.codebuddy.ai` and China
   `https://copilot.tencent.com` as separate environments and ships different
   model catalogs for them.

The official artifact can be pinned and inspected without reading any user
credentials:

```bash
mkdir /tmp/codebuddy-evidence && cd /tmp/codebuddy-evidence
npm pack @tencent-ai/codebuddy-code@2.129.0
shasum -a 256 tencent-ai-codebuddy-code-2.129.0.tgz
# bb843c5e55793645dd48337ace4d6ee5e8af597a4451a7bc66ae4a1a3ad02b66
tar -xzf tencent-ai-codebuddy-code-2.129.0.tgz
jq '{version:.version,name:.name}' package/package.json
jq '{endpoint:.endpoint,officialEndpoints:.officialEndpoints,
     auth:.authentication.type,
     models:[.models[]|{id,credits,supportsToolCall}]}' \
  package/product.json
jq '{models:[.models[]|{id,credits,supportsToolCall}]}' \
  package/product.cloudhosted.json
```

### Hosted endpoint probes

The community reference, pinned at [commit `9df1ae6`][reference], assumes:

- China base URL `https://copilot.tencent.com` and International base URL
  `https://www.codebuddy.ai`;
- browser polling under `/v2/plugin/auth/*`;
- bearer token plus CodeBuddy-specific identity/product headers;
- model traffic at `/v2/chat/completions` using an OpenAI-shaped SSE payload;
- refresh through `/v2/plugin/auth/token/refresh` with a refresh token in a
  proprietary header.

Safe unauthenticated probes confirmed only that both regional hosts currently
route those paths. They do **not** establish a supported contract:

```text
POST https://copilot.tencent.com/v2/plugin/auth/state?platform=CLI
POST https://www.codebuddy.ai/v2/plugin/auth/state?platform=CLI
=> HTTP 200; JSON code=0; data keys authUrl,state

GET https://copilot.tencent.com/v2/chat/completions
GET https://www.codebuddy.ai/v2/chat/completions
=> HTTP 401; WWW-Authenticate: Bearer realm="copilot"
```

No token was obtained, extracted from an installed application, printed, or
committed. The probes created no account and did not complete either browser
flow.

## Discovery matrix

| Dimension | Verified behavior | Provider viability |
| --- | --- | --- |
| Regional variants | China and International use different official hosts and product/model configuration. Enterprise and iOA are additional contracts. | Must be separate configurations; host substitution alone is not a complete regional contract. |
| Authentication | Browser login, user-created API keys, and enterprise OAuth Client Credentials are supported through official CLI/SDK paths. | Safe credentials exist, but the raw hosted API's required headers and scopes are not documented. No token extraction is justified. |
| API/protocol | Supported programmatic paths are the Preview Agent SDK and local `/api/v1`/ACP agent gateway. The fork's hosted `/v2/chat/completions` path is undocumented. | **Blocking.** Neither supported surface has CLIProxyAPI provider semantics. |
| Model catalog | Official packaged catalogs differ by region and include IDs, limits, modalities, tool support, and credit multipliers. The SDK can discover models for the authenticated account. | A copied static list would drift; no stable hosted catalog endpoint or versioning promise is documented. |
| Streaming | SDK queries are async streams; the local gateway streams agent runs and ACP events over SSE. The fork assumes OpenAI-shaped SSE from the hosted endpoint. | Streaming exists, but the hosted wire format required by an executor is not supported documentation. |
| Tool calls | Official SDK supports built-in/custom tools and permission callbacks; packaged models declare `supportsToolCall`. | Agent tool events are not a promised hosted chat-completions tool schema. Deterministic translation cannot be specified safely. |
| Quotas | Product configuration publishes per-model credit multipliers; `/cost` and plan limits expose consumption. | No public raw-API quota/rate-limit contract, headers, or account-health endpoint was found. |
| Token lifecycle | Official SDK supports cached browser login/logout, API keys, and enterprise access tokens. Its direct auth API is unstable. The fork adds undocumented polling and refresh behavior. | Safe lifecycle is available only behind official clients; implementing refresh directly would copy a private contract. |

## Acceptance criteria mapping

- **Protocol and auth assumptions documented from reproducible evidence:**
  satisfied by the pinned official docs/artifact, regional probes, and pinned
  reference comparison above.
- **No unsafe token extraction or secret logging:** satisfied. Evaluation used
  only public artifacts and unauthenticated requests. Any future integration
  must accept an explicitly supplied API key or use a documented OAuth flow.
- **Minimal authenticated, streaming, and tool requests pass tests:** not
  applicable after the discovery stop. There is no supported hosted protocol
  against which those tests can assert provider behavior, and no evaluation
  credential was supplied. Mocking the fork's assumptions would test our copy,
  not CodeBuddy's contract.
- **Unsupported capabilities fail clearly:** satisfied at the product decision
  level: CodeBuddy is not registered, so clients cannot select a partially
  working provider. A speculative executor with unclear failure boundaries was
  deliberately not added.

## Decision and reopening conditions

Do not merge the reference implementation or implement credentials, executor,
translation, registry, health, or Docker login support now. Unit tests around
mocked fork payloads cannot repair the missing vendor contract, and live tests
would be nondeterministic, account-dependent integration tests.

Re-evaluate when Tencent publishes at least one of:

1. a versioned hosted inference API covering authentication, request/response
   and SSE schemas, tool calls, errors, quota/rate-limit behavior, and token
   refresh/revocation; or
2. a stable, redistributable Go-compatible SDK that provides stateless model
   calls rather than full agent execution.

At that point, obtain dedicated test credentials through the documented path
and require authenticated text, streaming, tool-call, refresh, quota/error,
and regional contract tests before registering the provider.

[quick-start]: https://www.codebuddy.ai/docs/cli/quickstart
[sdk]: https://www.codebuddy.ai/docs/cli/sdk
[sdk-ts]: https://www.codebuddy.ai/docs/cli/sdk-typescript
[models]: https://www.codebuddy.ai/docs/cli/models
[costs]: https://www.codebuddy.ai/docs/cli/costs
[http-api]: https://www.codebuddy.ai/docs/cli/http-api
[npm]: https://registry.npmjs.org/@tencent-ai%2fcodebuddy-code/2.129.0
[reference]: https://github.com/HsnSaboor/CLIProxyAPIPlus/tree/9df1ae6dd489bee0434549ca37e389a2affb5db6
