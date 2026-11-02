# Anthropic Cloak

An experimental local HTTPS audit and compatibility proxy written in Go.
The client endpoint remains https://api.anthropic.com.

## Behavior

For POST requests to /v1/messages and /v1/messages/count_tokens:
- Remove x-anthropic-billing-header attribution lines from system text.
  Remove the entire text block when it becomes empty, or omit system when
  no blocks remain.
- Insert U+200B after the first character of Claude Agent SDK matches in
  system text, case-insensitively.
- Remove only claude-code-20250219 from the anthropic-beta list.

User messages, cache metadata on retained blocks and other beta values are
preserved. Gzip bodies are supported up to 32 MiB after decompression.
Response bodies, including server-sent events, are streamed.

This is not an authentication or billing workaround. A valid credential
and sufficient account access are required. Rewriting requests is not
guaranteed to resolve upstream errors.

## Downloads

In the Actions tab, select a successful Build run and download the artifact
for your platform. Linux, Windows and macOS builds include amd64 and arm64.
Artifacts are unsigned. Windows and macOS may require additional approval
before running a downloaded executable.

## Build

Go 1.24 or later is required. Only the Go standard library is used.

    go test -timeout 30s ./...
    go build -trimpath -o anthropic-cloak .

On Windows, use anthropic-cloak.exe as the output name.
Keep repository-local temporary files under tmp/.

## Run

    ./anthropic-cloak -listen 127.0.0.1:18080 -cert-dir tmp/certs

Windows PowerShell:

    .\anthropic-cloak.exe -listen 127.0.0.1:18080 -cert-dir tmp/certs

The proxy accepts CONNECT only for api.anthropic.com:443 and binds only to
loopback. It creates a local test CA certificate and a leaf certificate
valid for 24 hours. Private key files have owner-only permissions on Unix.
On Windows, protect the certificate directory with an appropriate ACL.

Configure only the client process you intend to inspect.

Linux or macOS:

    export HTTPS_PROXY=http://127.0.0.1:18080
    export NODE_EXTRA_CA_CERTS="$PWD/tmp/certs/ca.pem"
    claude

Windows PowerShell:

    $env:HTTPS_PROXY = "http://127.0.0.1:18080"
    $env:NODE_EXTRA_CA_CERTS = "$PWD\tmp\certs\ca.pem"
    claude

Ensure lowercase proxy variables do not override this setting and NO_PROXY
does not bypass api.anthropic.com. Configure authentication separately.
Never commit credentials. No API endpoint override is required.

Do not disable TLS validation or install the test CA into system trust.
After expiry, stop the proxy and choose a fresh directory under tmp/ using
-cert-dir.

## Options

| Flag | Default | Purpose |
| --- | --- | --- |
| -listen | 127.0.0.1:18080 | Loopback HTTP proxy listener |
| -cert-dir | tmp/certs | Local test certificates |
| -rewrite | true | Enable the three transformations |
| -mock | false | Synthetic upstream; no Anthropic requests |

Use -rewrite=false for pass-through comparison, or -mock for local testing.
Stop the foreground proxy with Ctrl-C. It installs no service.

## Security and limitations

The proxy decrypts requests in memory and can see credentials and content.
Inspect only traffic you are authorized to inspect. Audit logs contain
transformation booleans and HTTP status, not bodies or credential values.
Upstream TLS certificates are verified normally.

Tests cover transformations, gzip, TLS CONNECT, destination restrictions,
idempotency and delivery of the first SSE chunk before upstream completion.
Compatibility with all client features, signed thinking histories, long
conversations and production workloads is not established. Inbound TLS uses
HTTP/1.1. This project is not affiliated with Anthropic.

## License

MIT. See LICENSE.
