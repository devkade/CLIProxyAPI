# Issue 13: KiloCode provider evaluation

Evaluated on 2026-07-29 against Kilo's public service and the official
`Kilo-Org/kilocode` sources at commit
[`1b134cef71ee264f98d680358c03a51b4b7d226f`](https://github.com/Kilo-Org/kilocode/tree/1b134cef71ee264f98d680358c03a51b4b7d226f).

## Conclusion

**Kilo has a distinct upstream service, Kilo AI Gateway, but it does not need a
distinct CLIProxyAPI provider implementation.** Its supported external interface
is an OpenAI-compatible API at `https://api.kilo.ai/api/gateway`, so the existing
`openai-compatibility` configuration is sufficient for chat completions,
streaming, and tools.

This conclusion separates two different meanings of "KiloCode support":

- **Client support:** the Kilo editor/CLI is an AI client that can connect to
  many third-party providers. Supporting that client is not a new upstream
  provider integration.
- **Provider support:** Kilo AI Gateway is Kilo's own hosted routing, billing,
  model-catalog, and organization-control service. CLIProxyAPI can use it as an
  existing OpenAI-compatible upstream.

Do not port the dedicated `kilo` implementation from the referenced
`HALDRO/CLIProxyAPI-Extended` fork. It duplicates the generic executor and uses
the older `/api/openrouter` route plus a Kilo client device token. Kilo's public
Gateway documentation directs external integrations to a dashboard-issued API
key and `/api/gateway`; no editor credential extraction is needed.

## Supported configuration

Copy the personal Gateway API key from the Kilo dashboard as documented by
Kilo, store it as a secret, and configure only the models that should be exposed:

```yaml
openai-compatibility:
  - name: "kilo-gateway"
    prefix: "kilo"
    base-url: "https://api.kilo.ai/api/gateway"
    api-key-entries:
      - api-key: "<KILO_API_KEY>"
    models:
      - name: "anthropic/claude-sonnet-4.6"
        alias: "claude-sonnet-4.6"
        display-name: "Claude Sonnet 4.6 via Kilo"
        input-modalities: [text, image]
        output-modalities: [text]
```

For an organization-scoped credential, add the documented
`X-KiloCode-OrganizationId` header. Anonymous free-model access can use
`api-key: "anonymous"`, matching Kilo's first-party provider behavior, but is
subject to the limits and data-handling warning below.

## Discovery results

### Service and protocol

Kilo documents the Gateway as a unified hosted service for hundreds of models.
The API reference specifies:

- `POST /chat/completions` with the OpenAI message, generation, structured
  output, and function/tool fields;
- `GET /models` and `GET /providers`, without authentication;
- JSON non-streaming responses and SSE streaming responses;
- OpenAI-style HTTP errors (`400`, `401`, `402`, `403`, `429`, `500`, `502`,
  and `503`).

The official SDK guide explicitly says any OpenAI-compatible client can use the
Gateway by changing its base URL. This matches CLIProxyAPI's generic executor,
which appends `/chat/completions`, adds `Authorization: Bearer ...`, translates
requests and responses, and handles SSE.

Kilo also documents `POST /api/fim/completions` for Mistral Codestral. That path
is not part of CLIProxyAPI's generic chat surface and is intentionally not
claimed as supported by this configuration.

### Authentication and token lifetime

The stable external integration is a Bearer API key copied from the personal
account profile. Kilo describes these API keys as JWTs tied to the account but
does not publish a personal-key expiry or refresh contract. Treat a key as an
opaque, revocable secret and replace it when Kilo rejects it; do not decode it
or infer refresh behavior.

Organization tokens have a documented 15-minute expiry and carry organization
policy. They are not suitable as static `openai-compatibility` credentials
unless another supported system refreshes them. Kilo's first-party device-auth
helper records its resulting client token with a one-year expiry, but that
client login lifecycle is not required for the documented external API-key
flow. A dedicated CLIProxyAPI login implementation would therefore add token
and account lifecycle complexity without adding protocol capability.

No paid-account secret was available during this evaluation. Consequently, a
paid authenticated request was not executed. Authentication was checked from
the official contract and at the live boundary: a paid-model request without a
valid credential returned HTTP 401 with `PAID_MODEL_AUTH_REQUIRED`. This gap
does not justify handling or extracting an existing Kilo client credential.

### Model catalog

On 2026-07-29, both the documented `/api/gateway/models` endpoint and the legacy
`/api/openrouter/models` endpoint returned HTTP 200 and the same catalog of 346
models. Entries included provider-qualified IDs, pricing, context length,
modalities, and `supported_parameters`. The sampled
`stepfun/step-3.7-flash:free` entry advertised a 262,144-token context and tool
support.

The catalog is server-controlled and changes over time. CLIProxyAPI's generic
provider configuration uses an explicit model allow list, which is preferable
to introducing a stale hard-coded Kilo registry. Operators can select current
IDs from `GET https://api.kilo.ai/api/gateway/models`.

### Streaming and tools

Reproducible anonymous free-model probes against the documented Gateway route
verified the protocol without using private credentials:

- A non-streaming request returned HTTP 200, `object: "chat.completion"`,
  choices, and token usage.
- A streaming request returned HTTP 200 with `Content-Type:
  text/event-stream`, OpenAI `chat.completion.chunk` events, a final usage
  chunk, and `data: [DONE]`.
- A forced `get_issue` function request returned HTTP 200 with
  `finish_reason: "tool_calls"` and arguments `{"number": 13}`.

The tool probe used `inclusionai/ling-3.0-flash:free`; model-specific support
must still be checked through each catalog entry's `supported_parameters`.

### Usage and limits

- Anonymous and authenticated free-model traffic is limited to 200 requests
  per hour per IP and returns HTTP 429 when limited.
- Kilo does not impose a Gateway-level rate limit on paid traffic; upstream
  provider limits, account balance, and organization daily spending limits can
  still reject requests.
- Paid requests require sufficient credits and can return HTTP 402.
- Organization policy can restrict models/providers and return HTTP 403.
- Context and output limits are model-specific and published in the live
  catalog. The FIM endpoint additionally caps output at 1,000 tokens.
- Streaming cancellation may not stop every underlying provider immediately.

Free routing can select providers that log prompts and outputs and use them for
model improvement. Do not send personal, confidential, or repository-secret
content through anonymous/free routing. Normal API keys, organization IDs, and
BYOK keys must remain secrets and must not be logged.

## Reproducible evidence

Primary documentation, pinned to the evaluated official commit:

- [Gateway overview](https://github.com/Kilo-Org/kilocode/blob/1b134cef71ee264f98d680358c03a51b4b7d226f/packages/kilo-docs/pages/gateway/index.md)
- [Authentication and BYOK](https://github.com/Kilo-Org/kilocode/blob/1b134cef71ee264f98d680358c03a51b4b7d226f/packages/kilo-docs/pages/gateway/authentication.md)
- [API reference and tools](https://github.com/Kilo-Org/kilocode/blob/1b134cef71ee264f98d680358c03a51b4b7d226f/packages/kilo-docs/pages/gateway/api-reference.md)
- [Models and free-model handling](https://github.com/Kilo-Org/kilocode/blob/1b134cef71ee264f98d680358c03a51b4b7d226f/packages/kilo-docs/pages/gateway/models-and-providers.md)
- [SSE behavior](https://github.com/Kilo-Org/kilocode/blob/1b134cef71ee264f98d680358c03a51b4b7d226f/packages/kilo-docs/pages/gateway/streaming.md)
- [Usage, billing, and limits](https://github.com/Kilo-Org/kilocode/blob/1b134cef71ee264f98d680358c03a51b4b7d226f/packages/kilo-docs/pages/gateway/usage-and-billing.md)
- [External API-key setup](https://github.com/Kilo-Org/kilocode/blob/1b134cef71ee264f98d680358c03a51b4b7d226f/packages/kilo-docs/pages/getting-started/setup-authentication.md)
- [First-party endpoint and token constants](https://github.com/Kilo-Org/kilocode/blob/1b134cef71ee264f98d680358c03a51b4b7d226f/packages/kilo-gateway/src/api/constants.ts)

Live probes can be repeated with:

```bash
curl -sS https://api.kilo.ai/api/gateway/models | jq '.data | length'

curl -sS https://api.kilo.ai/api/gateway/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer anonymous' \
  -d '{"model":"stepfun/step-3.7-flash:free","messages":[{"role":"user","content":"Reply OK"}],"stream":false}'

curl -sS -N https://api.kilo.ai/api/gateway/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer anonymous' \
  -d '{"model":"stepfun/step-3.7-flash:free","messages":[{"role":"user","content":"Reply OK"}],"stream":true}'
```

## Acceptance mapping

| Issue requirement | Result |
|---|---|
| Distinguish client support from provider support | Done above; the client is multi-provider, while Gateway is Kilo's distinct hosted upstream. |
| Verify distinct service/auth/catalog/protocol | Confirmed from pinned official docs/source and live endpoints. |
| Assess device/auth, token lifetime, streaming, tools, and limits | Documented above, including what is and is not a stable external contract. |
| Minimal authenticated request | Paid-account request not run because no secret was available; Bearer behavior and rejection boundary verified. Dashboard API-key configuration is the supported path. |
| Minimal streaming request | Passed live against an anonymous free model, including final usage and `[DONE]`. |
| Security and unsupported capabilities | Explicit above: no token extraction, opaque API keys, free-route data warning, no FIM claim, and no static-org-token refresh. |
| No speculative implementation | Satisfied. Existing OpenAI compatibility is sufficient, so no provider code was added. |
