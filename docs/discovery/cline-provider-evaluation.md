# Cline client and provider evaluation

**Issue:** [#12 - Evaluate Cline provider integration](https://github.com/devkade/CLIProxyAPI/issues/12)

**Decision date:** 2026-07-29

**Decision:** Configure Cline as an OpenAI-compatible client. Do not add a
Cline-specific provider.

Cline and the Cline API are two different integration directions:

- The Cline editor extension is a client of CLIProxyAPI. Cline already has an
  **OpenAI Compatible** provider option with user-supplied base URL, API key,
  and model ID.
- Cline also publishes a supported hosted inference service, the **Cline API**.
  It is explicitly an OpenAI-compatible Chat Completions API authenticated by a
  bearer API key, so CLIProxyAPI can use it through the existing
  `openai-compatibility` upstream configuration.

Neither direction has a Cline-specific transport, credential lifecycle, or
translation boundary. A native provider would duplicate supported generic
behavior and incorrectly couple the Cline client identity to the Cline-hosted
API.

No provider, OAuth command, credential importer, executor, translator, or
model aliases are registered by this evaluation.

## Evidence boundary

The Cline repository was inspected at commit
[`7d63376d9824b07ebed4fd72c17d59c8960cad2c`](https://github.com/cline/cline/tree/7d63376d9824b07ebed4fd72c17d59c8960cad2c).
CLIProxyAPI was evaluated at commit
[`c9417c8a`](https://github.com/devkade/CLIProxyAPI/tree/c9417c8a).
These pinned revisions make the result reproducible.

No browser storage, editor secret storage, account token, keychain, network
capture, or undocumented endpoint was inspected. The issue's referenced
CLIProxyAPIPlus repository contains only a client-facing Cline mention at
[its evaluated revision](https://github.com/HsnSaboor/CLIProxyAPIPlus/blob/9df1ae6dd489bee0434549ca37e389a2affb5db6/README.md#L268);
it does not establish a separate Cline provider contract.

## Direction 1: Cline as a CLIProxyAPI client

Cline's official configuration guide says that any OpenAI-compatible endpoint
can be used and identifies the required fields as base URL, API key, and model
ID. See
[the supported endpoint scope and required settings](https://github.com/cline/cline/blob/7d63376d9824b07ebed4fd72c17d59c8960cad2c/docs/provider-config/openai-compatible.mdx#L6-L27).
The extension implements those same editable fields and can load model IDs from
the endpoint or accept a custom ID. See
[the OpenAI Compatible settings UI](https://github.com/cline/cline/blob/7d63376d9824b07ebed4fd72c17d59c8960cad2c/apps/vscode/webview-ui/src/components/settings/providers/OpenAICompatible.tsx#L303-L403).

CLIProxyAPI exposes authenticated `GET /v1/models` and
`POST /v1/chat/completions` routes, including streaming and non-streaming
handling. See
[the registered OpenAI-compatible routes](https://github.com/devkade/CLIProxyAPI/blob/c9417c8a/internal/api/server_routes.go#L59-L84) and
[the chat handler's stream dispatch](https://github.com/devkade/CLIProxyAPI/blob/c9417c8a/sdk/api/handlers/openai/openai_handlers.go#L97-L132).

### Exact Cline extension configuration

First configure a non-template client key in CLIProxyAPI:

```yaml
api-keys:
  - "replace-with-a-local-client-key"
```

Then open Cline settings and enter:

| Cline field | Value |
| --- | --- |
| API Provider | `OpenAI Compatible` |
| Base URL | `http://127.0.0.1:8317/v1` |
| API Key | the same CLIProxyAPI `api-keys` value |
| Model ID | an ID returned by `GET http://127.0.0.1:8317/v1/models` |

The `/v1` suffix belongs in the base URL. Cline's OpenAI-compatible runtime
passes that value as `baseURL` to its standard compatible client, which appends
the Chat Completions operation. See
[Cline's provider construction](https://github.com/cline/cline/blob/7d63376d9824b07ebed4fd72c17d59c8960cad2c/sdk/packages/llms/src/providers/vendors/openai-compatible.ts#L107-L151).

If CLIProxyAPI runs from this repository's Docker Compose service and Cline
runs on the host, the same loopback URL works because port 8317 is published.
If Cline itself runs in another container on the same Compose network, use
`http://cli-proxy-api:8317/v1`; container-local `127.0.0.1` would point back to
the Cline container. No OAuth callback or additional mounted credential is
needed for the client connection.

For a quick preflight, replace the key and run:

```bash
curl -fsS http://127.0.0.1:8317/v1/models \
  -H 'Authorization: Bearer replace-with-a-local-client-key'
```

Successful model listing proves the base URL, reachability, and local API-key
gate before Cline sends a task.

## Direction 2: the Cline API as an upstream

Cline now documents a distinct hosted service at
`https://api.cline.bot/api/v1/chat/completions`. Its supported contract is
OpenAI Chat Completions, including SSE streaming and OpenAI-format tool calls.
See
[the API identity and endpoint](https://github.com/cline/cline/blob/7d63376d9824b07ebed4fd72c17d59c8960cad2c/docs/api/overview.mdx#L7-L15),
[request and streaming schemas](https://github.com/cline/cline/blob/7d63376d9824b07ebed4fd72c17d59c8960cad2c/docs/api/chat-completions.mdx#L7-L32), and
[tool-call round trips](https://github.com/cline/cline/blob/7d63376d9824b07ebed4fd72c17d59c8960cad2c/docs/api/chat-completions.mdx#L144-L210).

Programmatic access uses a Cline API key created in Cline's settings and sent
as `Authorization: Bearer`. Although the extension and CLI also receive an
automatically managed account token after sign-in, Cline explicitly recommends
API keys for programmatic access. See
[the supported authentication methods](https://github.com/cline/cline/blob/7d63376d9824b07ebed4fd72c17d59c8960cad2c/docs/api/authentication.mdx#L7-L26) and
[account-token ownership](https://github.com/cline/cline/blob/7d63376d9824b07ebed4fd72c17d59c8960cad2c/docs/api/authentication.mdx#L58-L70).
CLIProxyAPI must not extract or import Cline's managed account token.

The existing upstream configuration is sufficient. For example:

```yaml
openai-compatibility:
  - name: "cline-api"
    prefix: "cline"
    base-url: "https://api.cline.bot/api/v1"
    api-key-entries:
      - api-key: "replace-with-a-cline-api-key"
    models:
      - name: "anthropic/claude-sonnet-4-6"
        alias: "claude-sonnet-4-6"
```

A client then requests model `cline/claude-sonnet-4-6` from CLIProxyAPI. The
prefix keeps the route explicit, while the upstream receives Cline's documented
`anthropic/claude-sonnet-4-6` identifier. Cline documents the
`provider/model-name` convention in
[its model guide](https://github.com/cline/cline/blob/7d63376d9824b07ebed4fd72c17d59c8960cad2c/docs/api/models.mdx#L7-L33).
Use a currently available model ID from Cline rather than treating this example
as a permanent catalog.

This maps exactly to CLIProxyAPI's generic configuration fields for base URL,
API-key entries, models, aliases, and optional prefix. See
[the existing example](https://github.com/devkade/CLIProxyAPI/blob/c9417c8a/config.example.yaml#L429-L463).
The executor appends `/chat/completions`, sets the bearer credential, and uses
the existing OpenAI translators for streaming and non-streaming requests. See
[the generic executor request path](https://github.com/devkade/CLIProxyAPI/blob/c9417c8a/internal/runtime/executor/openai_compat_executor.go#L85-L153).

The Cline API does not require provider-specific OAuth in CLIProxyAPI. API-key
rotation can use multiple `api-key-entries`, and the existing scheduler and
cooldown behavior apply without a new Cline credential type. The API's model
IDs can be declared in configuration; a native hard-coded catalog would age
faster and add no protocol capability.

## Acceptance mapping

| Issue #12 requirement | Evidence | Result |
| --- | --- | --- |
| Separate client configuration from an upstream provider | The two directions and their independent keys are documented above. | Pass |
| Determine whether Cline-specific authentication is required | Client access uses a CLIProxyAPI local key; hosted API access uses a supported Cline API key. Managed extension account tokens are not imported. | No Cline-specific auth implementation |
| Determine whether a distinct provider API exists | Cline publishes a supported hosted Chat Completions API. | Yes, but it is already covered by `openai-compatibility` |
| Determine whether protocol translation is required | Cline emits and accepts the OpenAI-compatible contract used at both existing boundaries. | No new translation |
| Define model behavior | Cline client selects `/v1/models` output; hosted Cline API uses documented `provider/model` IDs configured as generic upstream models. | Existing model mechanisms suffice |
| Minimal end-to-end request | The client settings target CLIProxyAPI's existing model and chat routes; the same path is covered by deterministic route/executor tests and the local protocol exercise recorded with this change. | Pass without new provider code |
| Docker and security constraints | Host/container URLs are explicit; only dedicated local and upstream API keys are used; no browser-token workaround is involved. | Pass |

## Re-evaluation trigger

Add a native provider only if Cline publishes a supported capability that
cannot be represented by OpenAI-compatible configuration, such as a different
request/stream protocol, a required refreshable application OAuth grant, or a
provider-only operation needed by CLIProxyAPI. A new model name, hosted model,
or Cline client release is not such a trigger.

Until then, the correct integration is configuration, not provider code.
