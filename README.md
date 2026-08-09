# local-ai

A lightweight Windows CLI + Service that wraps [`llama-server`](https://github.com/ggml-org/llama.cpp)
to turn a machine into a multi-model local LLM host, reachable over the
network as an OpenAI- and Anthropic-compatible API provider — e.g. behind
[LiteLLM](https://github.com/BerriAI/litellm) and Langfuse.

`local-ai` doesn't reimplement inference serving. `llama-server` already has
a "router mode" that loads multiple GGUF models on demand and unloads them
after idle timeouts, and it already speaks both the OpenAI API and the
Anthropic Messages API natively. `local-ai` supervises that process, adds
model acquisition from Hugging Face, and puts its own API-key gateway in
front so `llama-server` itself never has to be exposed to the network.

## Requirements

- Windows
- A `llama-server.exe` build with router-mode support (`--models-dir`,
  `--models-preset`, `--models-max`, `--models-autoload`,
  `--sleep-idle-seconds`)

## Build

```powershell
go build -o bin\local-ai.exe .\cmd\local-ai
```

## Quick start

```powershell
# Point local-ai at your llama-server.exe (defaults to "llama-server.exe" on PATH)
.\bin\local-ai.exe config set llama_server_path "E:\llamaserver\bin\llama-server.exe"

# Download a model from Hugging Face (same owner/repo:quant addressing as
# llama-server's own -hf-repo flag)
.\bin\local-ai.exe pull Qwen/Qwen2.5-0.5B-Instruct-GGUF:q4_k_m

# Run in the foreground to try it out
.\bin\local-ai.exe serve
```

In another shell:

```powershell
.\bin\local-ai.exe status
.\bin\local-ai.exe keys create my-first-key   # save the printed key, shown once
```

Then call the gateway (default port `11535`) exactly like an OpenAI or
Anthropic endpoint:

```powershell
curl http://localhost:11535/v1/chat/completions `
  -H "Authorization: Bearer <key>" -H "content-type: application/json" `
  -d '{"model":"qwen2.5-0.5b-instruct-q4_k_m","messages":[{"role":"user","content":"hi"}]}'

curl http://localhost:11535/v1/messages `
  -H "x-api-key: <key>" -H "content-type: application/json" `
  -d '{"model":"qwen2.5-0.5b-instruct-q4_k_m","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}'
```

Models load on first request and idle back out of memory automatically
(`idle_timeout_seconds`, default 600s) — starting `local-ai` does not, by
itself, load anything or use the GPU.

## Running as a Windows Service

From an **elevated** shell:

```powershell
.\bin\local-ai.exe install-service          # --startup=auto|manual (default: auto)
.\bin\local-ai.exe service start
.\bin\local-ai.exe service status
.\bin\local-ai.exe service stop
.\bin\local-ai.exe uninstall-service
```

Startup type defaults to Automatic: since load/unload is fully on-demand,
having the process start at boot doesn't mean it's using the GPU. Flip it
with `service enable` / `service disable` at any time without reinstalling.

When running as a service, configuration and state live in
`%PROGRAMDATA%\local-ai\` (`config.yaml`, `models\`, `presets.ini`,
`keys.json`, `logs\`). Override with `--data-dir` or the
`LOCAL_AI_DATA_DIR` environment variable for local development.

## Commands

| Command | Purpose |
|---|---|
| `pull <owner>/<repo>[:quant]` | Download a GGUF model from Hugging Face |
| `list` / `list --litellm-config` | List local models, or emit a LiteLLM `model_list:` block |
| `rm <model-id>` | Delete a downloaded model |
| `serve` | Run the supervisor + gateway in the foreground |
| `status` | Show each model's live load state |
| `keys create/list/revoke` | Manage gateway API keys |
| `config get` / `config set` | View/edit gateway port, idle timeout, models-max, etc. |
| `config model set/unset` | Per-model `llama-server` flag overrides (ctx-size, gpu-layers, ...) |
| `logs [-f]` | View/follow the llama-server log |
| `install-service` / `uninstall-service` | Register/remove the Windows Service |
| `service start/stop/restart/status/enable/disable` | Control the installed service |

## Wiring up LiteLLM

LiteLLM doesn't auto-discover backend models, so its config has to be kept in
sync by hand (or by re-running the command below) whenever models are pulled
or removed on the local-ai side.

**1. On the local-ai machine** — make sure it's actually reachable and has
something to serve:

```powershell
.\bin\local-ai.exe serve              # or: install as a service, see above
.\bin\local-ai.exe pull <owner>/<repo>:<quant>
.\bin\local-ai.exe keys create litellm
```

Save the printed key. If LiteLLM runs on a different machine, open the
gateway port on the Windows firewall so it's reachable over the LAN:

```powershell
New-NetFirewallRule -DisplayName "local-ai gateway" -Direction Inbound `
  -Protocol TCP -LocalPort 11535 -Action Allow
```

**2. Generate the model list** — from the local-ai machine:

```powershell
.\bin\local-ai.exe list --litellm-config
```

This prints a ready-to-paste `model_list:` block, e.g.:

```yaml
model_list:
  - model_name: qwen2.5-0.5b-instruct-q4_k_m
    litellm_params:
      model: openai/qwen2.5-0.5b-instruct-q4_k_m
      api_base: http://192.168.1.50:11535/v1
      api_key: os.environ/LOCAL_AI_API_KEY
```

**3. On the LiteLLM machine** — drop that block into a `config.yaml` (add a
`litellm_settings:`/`general_settings:` section as needed for your setup),
export the key from step 1, and start the proxy:

```bash
export LOCAL_AI_API_KEY="la_..."          # the key from `keys create`
pip install 'litellm[proxy]'              # or use LiteLLM's docker image
litellm --config config.yaml --port 4000
```

(Docker equivalent: mount `config.yaml` into the container and pass
`-e LOCAL_AI_API_KEY=la_...` on `docker run`.)

**4. Test it end-to-end** through LiteLLM, not directly against local-ai, to
confirm the whole chain works:

```bash
curl http://<litellm-host>:4000/v1/chat/completions \
  -H "Authorization: Bearer <litellm-master-or-virtual-key>" \
  -H "content-type: application/json" \
  -d '{"model":"qwen2.5-0.5b-instruct-q4_k_m","messages":[{"role":"user","content":"hi"}]}'
```

A response means the request went LiteLLM → local-ai gateway → llama-server
and back. From here, Langfuse traces this the same way it traces any other
LiteLLM-proxied model — no local-ai-specific setup needed on that side.

Whenever you `pull`/`rm` a model, re-run step 2 and update LiteLLM's
`config.yaml` to match.

## Architecture

See [`.plan/design.md`](.plan/design.md) for the full design writeup,
including what was verified live against a real `llama-server.exe` build.
