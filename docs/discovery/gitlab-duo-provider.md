# GitLab Duo provider discovery

Issue: [#10](https://github.com/devkade/CLIProxyAPI/issues/10)
Evaluated: 2026-07-29
Decision: **Do not implement a GitLab Duo provider with the currently documented APIs.**
Discovery gate: **Failed because no generally available third-party inference contract exists.**

## Provider-boundary conclusion

GitLab Duo is not currently a supported general-purpose upstream for CLIProxyAPI. The endpoint whose name most closely resembles an LLM provider API, `POST /api/v4/chat/completions`, is explicitly documented as internal-only on GitLab.com. On GitLab Self-Managed it is disabled behind the `access_rest_chat` feature flag, and the documented prerequisite is membership of the GitLab team. It is therefore not a customer API contract.

The endpoint is also not OpenAI-compatible. It accepts one `content` string and GitLab resource context, and returns one JSON string. It has no request fields for a provider model, message roles, streaming, caller-defined tools, or tool results. Configuring it as an `openai-compatibility` upstream would send the wrong path and schema. Implementing it as a native executor would depend on an internal-only API and violate the issue's no-undocumented-scraping constraint.

GitLab's GraphQL `aiAction` mutation does not change this conclusion. It is marked Experiment, creates an asynchronous GitLab Duo product action, and uses GitLab-specific conversation threads and client subscription IDs. GitLab's contributor documentation describes streamed chunks over user-centric GraphQL subscriptions and routing through private AI Gateway endpoints. That is an implementation surface for GitLab's UI and editor integrations, not a stable provider API with a documented third-party compatibility contract.

**Boundary classification: not viable today**, rather than a native executor or an OpenAI-compatible upstream entry.

## Deployment matrix

| Deployment | Authentication and host | Chat API status | Provider result |
| --- | --- | --- | --- |
| GitLab.com | Bearer access token against `https://gitlab.com`; GitLab's VS Code setup requires PATs to have `api` scope | REST Chat API is internal-only and requires the caller to be a GitLab team member | Unsupported |
| GitLab Self-Managed | OAuth or PAT with `api` scope against the instance URL, not GitLab.com | `/api/v4/chat/completions` is disabled by default behind `access_rest_chat`; the same documented GitLab-team-member prerequisite applies | Unsupported for customers |
| GitLab Dedicated | Tenant instance URL; administrator and namespace policy still apply | GitLab Duo as a product is offered, but the REST Chat API documentation does not offer this endpoint for Dedicated | Unsupported |
| GitLab Dedicated for Government | Tenant instance URL and self-hosted, FedRAMP-approved models are required | No supported Chat provider API is documented | Unsupported |

No separate public regional Duo API base URLs are documented. Enterprise and Dedicated clients connect to their own GitLab instance URL. A future implementation must accept and validate an explicit instance URL, derive API paths from it, and never fall back to GitLab.com for an enterprise credential.

## Capability and security findings

### Authentication and scopes

- The REST Chat example uses `Authorization: Bearer <YOUR_ACCESS_TOKEN>`.
- GitLab documents OAuth 2.0 tokens and personal access tokens as REST API authentication methods. Its official VS Code setup requires a PAT with the broad `api` scope on GitLab.com, Self-Managed, and Dedicated.
- Authentication alone is insufficient: endpoint eligibility is restricted to GitLab team members, and Duo availability additionally depends on license, add-on or usage entitlement, seat assignment, namespace/instance policy, and feature state.
- Tokens must remain secrets. They belong in a secret-backed credential record, must be sent only to the configured instance origin, and must be redacted from logs, diagnostics, errors, and serialized configuration. No credential format was added because there is no supported execution boundary.

### Endpoints and protocol

| Surface | Documented behavior | Compatibility consequence |
| --- | --- | --- |
| `POST /api/v4/chat/completions` | Internal REST endpoint; body uses `content` plus GitLab resource context; response is a JSON string | Neither OpenAI Chat Completions nor a sufficient native provider contract |
| `POST /api/graphql` with `aiAction` | Experimental GitLab Duo action with thread and subscription identifiers | Product workflow, not a stable synchronous provider call |
| GraphQL subscription | GitLab contributor docs describe complete messages and streamed chunks keyed by user or `clientSubscriptionId` | Bespoke asynchronous transport; not SSE/OpenAI streaming and not documented as a third-party provider contract |
| AI Gateway `/v1/prompts/chat` and `/v2/chat/agent` | Internal downstream routes described in GitLab's architecture | Must not be called or scraped directly |

A reproducible unauthenticated boundary probe on the evaluation date confirms that GitLab.com does not expose the REST route as a public API:

```console
$ curl -sS -i --request POST \
    --header 'Content-Type: application/json' \
    --data '{"content":"boundary probe"}' \
    https://gitlab.com/api/v4/chat/completions
HTTP/2 401
content-type: application/json

{"message":"401 Unauthorized"}
```

This probe is supporting evidence only; the documented internal-only policy is the deciding evidence. An authenticated request was deliberately not attempted: no eligible GitLab-team-member credential was available, and using a normal Duo customer token cannot turn an internal endpoint into a supported contract.

### Models

GitLab publishes models by Duo *feature* and can change the default model to optimize the product. For example, the current model table assigns a default to General Chat and lists feature-specific alternatives. The REST Chat request has no `model` attribute and no model-list endpoint. Model choice is controlled by GitLab at the top-level namespace, instance, feature, or user level rather than by each provider request. Consequently CLIProxyAPI cannot truthfully register a stable GitLab Duo model catalog or honor a client-selected model.

### Streaming

The REST Chat API documents a single JSON-string response and no `stream` attribute, SSE media type, chunk schema, cancellation behavior, or terminal event. GitLab's product does stream, but the documented implementation uses a GraphQL mutation plus user-centric subscriptions. The contributor guide also notes places where streaming responses are not supported. This is not enough to define deterministic CLIProxyAPI stream fixtures against a supported external contract.

### Tools

The REST request schema has no tools, tool choice, tool-call, or tool-result fields. GitLab Duo Agentic Chat can execute GitLab, shell, Git, and MCP tools, but tool availability and approval are controlled by GitLab subscriptions and cascading instance/group/project policy. GitLab's server selects and executes those tools as part of its Agentic Chat workflow. There is no documented mapping for caller-supplied OpenAI, Claude, or Gemini tool definitions. Passing tools through would either silently discard client intent or bypass GitLab's approval model, so tools cannot be supported by a provider boundary.

### Enterprise policy and data handling

- Owners and administrators can turn Duo on or off; Dedicated administrators can lock selected subgroups off. An access token does not override policy.
- Self-Managed cloud-connected deployments require licenses, add-ons or usage billing, AI Gateway connectivity, and network/proxy support. Agentic features additionally use the Duo Workflow Service over HTTP/2 and long-lived connections.
- Fully self-hosted deployments can keep prompts and responses in the customer network, while hybrid or GitLab-managed model configurations send feature calls through GitLab's hosted AI Gateway. Data routing therefore depends on administrator-selected deployment policy.
- GitLab documents retention, prompt caching, telemetry, optional expanded logging, and secret-redaction behavior. A proxy must not imply that its own token redaction changes GitLab's configured retention or subprocessors.

## Acceptance mapping

| Issue acceptance criterion | Result |
| --- | --- |
| Supported deployment matrix and security requirements are documented | Met by this report; all evaluated deployments are unsupported as provider boundaries, with host, auth, policy, network, and data constraints recorded above |
| One authenticated request and one streaming request pass deterministic tests | Not applicable after the discovery gate failed; the only REST endpoint is internal-only and has no documented streaming mode, so such tests would validate an unsupported mock rather than an integration |
| Enterprise host configuration does not leak tokens or assume GitLab.com | No credential or runtime code was added; requirements for any future boundary are stated explicitly above |
| Unsupported capabilities return clear errors | No provider is registered, so GitLab Duo models cannot be selected and no request can be silently degraded; tools and client-selected models remain unsupported by design |

## Revisit gate

Reconsider a provider only when GitLab publishes all of the following for third-party customers:

1. A generally available Chat or inference API for GitLab.com and the intended enterprise offerings, without GitLab-team-member eligibility.
2. Explicit OAuth/PAT scopes and entitlement/error semantics for that API.
3. Stable non-streaming and streaming wire schemas, including cancellation and terminal/error events.
4. A request-level model contract or a stable virtual model identifier and discovery/availability behavior.
5. Explicit caller-defined tool semantics, or an explicit statement that tools are unsupported independently of Agentic Chat's server-side tool policy.

At that point the likely boundary is a small native executor because the documented GitLab resource context and asynchronous semantics do not match an OpenAI-compatible upstream.

## Primary and reproducible evidence

- [GitLab Duo Chat completions API](https://docs.gitlab.com/api/chat/) - internal-only status, feature flag, team-member prerequisite, request attributes, endpoint, and scalar response.
- [Immutable GitLab source for the Chat API documentation](https://github.com/gitlabhq/gitlabhq/blob/067d7a3bd9b37e38922ef9a20e8fbf90bfb8fcec/doc/api/chat.md) - reproducible policy snapshot evaluated by this report.
- [GitLab GraphQL API reference: `Mutation.aiAction`](https://docs.gitlab.com/api/graphql/reference/#mutationaiaction) - Experiment status and GitLab-specific action/thread/subscription fields.
- [GitLab Duo Chat development architecture](https://github.com/gitlabhq/gitlabhq/blob/067d7a3bd9b37e38922ef9a20e8fbf90bfb8fcec/doc/development/ai_features/duo_chat.md#graphql-subscription) - mutation/subscription streaming design, tool execution, and internal AI Gateway routing.
- [REST API authentication](https://docs.gitlab.com/api/rest/authentication/) - supported token authentication methods and headers.
- [GitLab for VS Code setup](https://docs.gitlab.com/editor_extensions/visual_studio_code/setup/#authenticate-with-gitlab) - `api` PAT scope and explicit instance URL for GitLab.com, Self-Managed, and Dedicated.
- [GitLab Duo AI models](https://docs.gitlab.com/user/gitlab_duo/model_selection/) - feature-scoped defaults, selectable models, availability behavior, and offering scope.
- [GitLab Duo Agentic Chat](https://docs.gitlab.com/user/gitlab_duo_chat/agentic_chat/) - product deployment scope, model selection, tool capabilities, and approval policy.
- [GitLab Duo availability](https://docs.gitlab.com/user/gitlab_duo/turn_on_off/) - cascading owner/administrator controls and Dedicated locks.
- [Configure GitLab Duo](https://docs.gitlab.com/administration/gitlab_duo/configure/) - enterprise licensing, AI Gateway, DNS, firewall, HTTP/2, proxy, and long-lived connection requirements.
- [GitLab Duo Self-Hosted](https://docs.gitlab.com/administration/gitlab_duo_self_hosted/) - deployment modes, network requirements, billing metadata, and data-routing boundaries.
- [GitLab Duo data usage](https://docs.gitlab.com/user/gitlab_duo/data_usage/) - retention, model training policy, telemetry, prompt caching, expanded logging, and secret redaction.
