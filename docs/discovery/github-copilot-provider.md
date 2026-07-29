# GitHub Copilot provider discovery gate

**Issue:** [#7 - Add GitHub Copilot OAuth provider](https://github.com/devkade/CLIProxyAPI/issues/7)

**Decision date:** 2026-07-29

**Decision:** Do not add a first-class GitHub Copilot provider yet.

GitHub now offers supported Copilot integration surfaces, but none is a supported
canonical chat-completions provider contract suitable for CLIProxyAPI. The
supported Copilot SDK is an agent runtime: it accepts a prompt, owns the tool-use
loop, may make multiple model calls, and emits agent events. Treating that runtime
as an upstream executor would change the meaning of OpenAI-compatible chat and
tool requests. Calling Copilot's model endpoint directly would avoid that semantic
mismatch, but GitHub does not publish the required direct authentication and wire
contract. Implementations in the referenced community forks fill that gap with
undocumented token exchange and client behavior, which issue #7 explicitly rules
out.

This is a narrow compatibility gate, not a claim that the Copilot SDK is unsafe or
unsupported. The SDK is a supported option for building an agent application; it
is not currently a drop-in provider transport.

## Evidence boundary

Only GitHub-owned sources were used. Source links below are pinned to the exact
commits reviewed so that the result is reproducible:

- GitHub Copilot SDK commit
  [`7f1f8478eb72ad8e879fea2a7ab512c95a6d3cf9`](https://github.com/github/copilot-sdk/tree/7f1f8478eb72ad8e879fea2a7ab512c95a6d3cf9)
- GitHub Copilot CLI commit
  [`425b68cdb22db404eb9e9978f72a8f4c425bee45`](https://github.com/github/copilot-cli/tree/425b68cdb22db404eb9e9978f72a8f4c425bee45)
- GitHub Docs commit
  [`424216534cce9ce19bb83cd188c1551105220948`](https://github.com/github/docs/tree/424216534cce9ce19bb83cd188c1551105220948)

No browser storage, editor installation, keychain, process memory, network capture,
or user token was inspected.

## What GitHub supports

### Accounts and authentication

The SDK authentication guide says that interactive users authenticate through the
Copilot CLI's OAuth device flow and that credentials are stored in the operating
system keychain. It separately supports an application's OAuth GitHub App by
passing a user access token to the SDK. Supported token forms are OAuth user
access tokens (`gho_`), GitHub App user-to-server tokens (`ghu_`), and fine-grained
personal access tokens (`github_pat_`); classic PATs are not supported. See
[authentication lines 15-22 and 107-234](https://github.com/github/copilot-sdk/blob/7f1f8478eb72ad8e879fea2a7ab512c95a6d3cf9/docs/auth/authenticate.md#L15-L22).

The supported OAuth application path is conventional: an operator creates a
GitHub OAuth App or GitHub App, the application exchanges the authorization code,
stores the resulting user token, and passes it to the SDK. GitHub documents that
Copilot usage is charged to the authenticated user's subscription and applies to
individual, organization, and enterprise identities. See
[the OAuth architecture and flow](https://github.com/github/copilot-sdk/blob/7f1f8478eb72ad8e879fea2a7ab512c95a6d3cf9/docs/setup/github-oauth.md#L1-L40) and
[token exchange and SDK handoff](https://github.com/github/copilot-sdk/blob/7f1f8478eb72ad8e879fea2a7ab512c95a6d3cf9/docs/setup/github-oauth.md#L75-L140).

Account entitlement and model availability should not be hardcoded by plan name.
The supported SDK exposes `listModels()` with capability, billing, and policy data,
so the authenticated account's runtime catalog is authoritative. See
[the SDK feature matrix](https://github.com/github/copilot-sdk/blob/7f1f8478eb72ad8e879fea2a7ab512c95a6d3cf9/docs/troubleshooting/compatibility.md#L39-L44).

For multi-user services, GitHub requires `mode: "empty"`, unique sessions,
explicit tools, and a per-session `gitHubToken`; it warns that the default mode can
expose ambient host filesystem capabilities. See
[the multi-tenancy safety requirements](https://github.com/github/copilot-sdk/blob/7f1f8478eb72ad8e879fea2a7ab512c95a6d3cf9/docs/setup/multi-tenancy.md#L18-L54).

### Deployment

The supported server topology is an SDK connected over JSON-RPC to a Copilot CLI
running in headless mode. The CLI can run in a separate container and defaults to
a loopback listener. GitHub does not publish a prebuilt Docker image, but documents
building one from an official CLI release. See
[backend service topology](https://github.com/github/copilot-sdk/blob/7f1f8478eb72ad8e879fea2a7ab512c95a6d3cf9/docs/setup/backend-services.md#L1-L39) and
[headless/Docker guidance](https://github.com/github/copilot-sdk/blob/7f1f8478eb72ad8e879fea2a7ab512c95a6d3cf9/docs/setup/backend-services.md#L59-L105).
The CLI license permits redistribution only unmodified and as part of an
application with material additional functionality; any future bundled-runtime
implementation must preserve the license and notices. See
[CLI license lines 3-20](https://github.com/github/copilot-cli/blob/425b68cdb22db404eb9e9978f72a8f4c425bee45/LICENSE.md#L3-L20).

These sources establish a supported agent-application path, including
multi-account isolation and container operation. They do not establish a provider
executor path.

## Why the SDK cannot preserve CLIProxyAPI semantics

CLIProxyAPI's provider boundary receives a canonical request and must return the
corresponding provider response or stream. In particular, a client-owned tool call
must be returned to the client, and a later request supplies the tool result.

The Copilot SDK has a different boundary:

1. The application sends one prompt over JSON-RPC.
2. The Copilot CLI orchestrates one or more model calls.
3. If the model requests a tool, the CLI executes the registered tool itself,
   feeds the result into another model call, and continues until final text.

GitHub documents this behavior explicitly in
[the agent-loop architecture](https://github.com/github/copilot-sdk/blob/7f1f8478eb72ad8e879fea2a7ab512c95a6d3cf9/docs/features/agent-loop.md#L5-L39) and
[the responsibility table](https://github.com/github/copilot-sdk/blob/7f1f8478eb72ad8e879fea2a7ab512c95a6d3cf9/docs/features/agent-loop.md#L97-L106).
The public messaging API is prompt/session based, while custom tools are handlers
that the assistant executes automatically. It is not a public API for submitting
an arbitrary canonical role/content/tool history and receiving exactly one raw
model turn.

Consequences of wrapping the SDK as an executor would include:

- client tool calls becoming server-executed agent tools rather than
  OpenAI-compatible `tool_calls` responses;
- one local API request potentially causing multiple billable model calls;
- canonical role and tool-result history being flattened or reconstructed rather
  than translated losslessly;
- stream chunks being agent lifecycle events rather than a single provider turn;
- upstream error and usage mapping describing an agent run instead of the exact
  requested model operation.

Those are observable protocol changes, not implementation details. Fixtures could
make an adapter deterministic without making its behavior compatible.

## Why direct HTTP is not an acceptable fallback

GitHub's docs repository names
`https://api.githubcopilot.com/chat/completions`, but only as a documentation
variable at the reviewed commit. See
[`chat_completions_api`](https://github.com/github/docs/blob/424216534cce9ce19bb83cd188c1551105220948/data/variables/copilot.yml#L56-L57).
The supported SDK documentation instead routes requests through the Copilot CLI
and describes the CLI as the component that calls the Copilot API. See
[the official OAuth sequence](https://github.com/github/copilot-sdk/blob/7f1f8478eb72ad8e879fea2a7ab512c95a6d3cf9/docs/setup/github-oauth.md#L11-L33).

The reviewed first-party material does not specify a standalone provider contract
for that endpoint covering all of the following:

- whether an OAuth/GitHub App user token may be sent directly or must first be
  exchanged for a Copilot service token;
- that exchange endpoint, request fields, response fields, expiry, and refresh;
- required client identification and feature headers;
- complete request, response, streaming, tool, reasoning, and error schemas;
- compatibility/version guarantees for third-party proxy clients.

The SDK's direct API-token environment variables do not fill this gap: its
authentication priority merely accepts `GITHUB_COPILOT_API_TOKEN` together with
`COPILOT_API_URL`; it does not document how a user application obtains or refreshes
such a token. See
[authentication priority](https://github.com/github/copilot-sdk/blob/7f1f8478eb72ad8e879fea2a7ab512c95a6d3cf9/docs/auth/authenticate.md#L303-L314).

Using reverse-engineered values from an editor, CLI binary, keychain, or community
fork would therefore make the implementation depend on precisely the undocumented
token extraction and client emulation excluded by issue #7. No such behavior was
copied or tested here.

## Acceptance decision

| Issue #7 requirement | Supported evidence | Gate result |
|---|---|---|
| Authenticate without a token in config or logs | CLI device flow/keychain is supported; application OAuth is supported | Available for SDK applications, but SDK login/logout is CLI-only and does not provide CLIProxyAPI's isolated auth-file flow ([matrix](https://github.com/github/copilot-sdk/blob/7f1f8478eb72ad8e879fea2a7ab512c95a6d3cf9/docs/troubleshooting/compatibility.md#L135-L138)) |
| Route through normal local API/API-key gate | No canonical provider transport is published | Blocked |
| Rotate multiple accounts and handle expiry/refresh | Per-session tokens and isolation are supported; acquisition/lifecycle remains application-owned | Insufficient for a self-contained provider |
| Deterministic streaming/tools/errors | SDK exposes agent events and an internally executed tool loop | Incompatible with client-owned provider semantics |
| Docker localhost callback/device auth | Headless CLI/container operation is supported; SDK device login remains a direct CLI operation | Not an embeddable provider auth surface |

The only implementation paths presently available are therefore either
semantically incompatible (adapt the SDK agent loop) or unsupported by published
protocol evidence (call the model endpoint directly). Adding provider code under
either condition would fail the issue's architecture and safety constraints.

## Re-evaluation trigger

Re-open implementation when GitHub publishes either:

1. a supported single-turn chat API specification for third-party applications,
   including OAuth/token lifecycle, streaming, tools, reasoning, models, errors,
   and compatibility guarantees; or
2. an SDK operation that accepts canonical message/tool history and returns one
   model turn without executing tools or injecting an agent loop.

At that point, use application-owned OAuth/device credentials, `mode: "empty"` if
the runtime remains involved, per-account isolated storage, runtime model discovery,
and the existing CLIProxyAPI scheduler/cooldown machinery. Until then, no Copilot
provider should be registered.
