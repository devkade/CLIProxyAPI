# Ollama Cloud

CLIProxyAPI treats direct Ollama Cloud and host-local Ollama as separate upstreams.

## Direct Ollama Cloud

Ollama documents direct cloud access at `https://ollama.com`, Bearer API-key authentication, model listing at `/api/tags`, and chat at `/api/chat`. Ollama's OpenAI-compatible API exposes `/v1/models` and `/v1/chat/completions`; CLIProxyAPI uses the OpenAI-compatible surface at `https://ollama.com/v1` so its existing OpenAI, Claude, and Gemini request formats can use one provider.

Create a key at <https://ollama.com/settings/keys>, then add a dedicated provider entry. Never commit a real key.

```yaml
ollama-cloud-api-key:
  - api-key: "replace-with-your-ollama-key"
    # prefix: "cloud" # optional; clients then request cloud/gpt-oss:120b
    models:
      - name: "gpt-oss:120b"
        alias: "gpt-oss:120b"
        input-modalities: [text]
      - name: "qwen3-vl:235b"
        alias: "ollama-cloud-vision"
        input-modalities: [text, image]
        output-modalities: [text]
```

The explicit model list is the credential's permitted CLIProxyAPI catalog. It makes `/v1/models` deterministic and avoids assuming that every cloud model has the same tools, vision, or reasoning capabilities. Use model identifiers from Ollama's cloud catalog or from either official listing endpoint:

```sh
curl -fsS https://ollama.com/api/tags
curl -fsS https://ollama.com/v1/models
```

CLIProxyAPI sends the configured key only as `Authorization: Bearer ...` to the upstream. Upstream authentication failures retain their HTTP status and do not include the configured key in the returned error or request logs.

Cloud chat, streaming, tools, and multimodal input are passed through according to Ollama's OpenAI compatibility contract and the capabilities of the selected model. Do not declare image input for a model unless Ollama lists that capability.

## Host-local Ollama from Docker

Local Ollama normally listens on `http://localhost:11434` and does not require authentication. Inside the CLIProxyAPI container, `localhost` means the container itself, not the Docker host. The provided Compose file maps `host.docker.internal` to the host gateway, including on Linux.

Configure local Ollama as a generic OpenAI-compatible provider, not as `ollama-cloud-api-key`:

```yaml
openai-compatibility:
  - name: "ollama-local"
    base-url: "http://host.docker.internal:11434/v1"
    models:
      - name: "qwen3:8b"
        alias: "local-qwen3"
```

A container reaches direct Ollama Cloud over normal HTTPS and does not use `host.docker.internal`.

## Smoke test

With CLIProxyAPI running and a frontend API key configured:

```sh
curl -fsS http://127.0.0.1:8317/v1/models \
  -H 'Authorization: Bearer YOUR_CLIPROXY_KEY'

curl -fsS http://127.0.0.1:8317/v1/chat/completions \
  -H 'Authorization: Bearer YOUR_CLIPROXY_KEY' \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-oss:120b","messages":[{"role":"user","content":"Say hello"}]}'
```
