# Anthropic Cloak

A standalone Magpie gateway middleware plugin derived from the MIT-licensed [Antigravity Cloak](https://github.com/magpie-community/plugins/tree/main/packages/antigravity-cloak), with explicit provider-ID matching through the `providers` option.

The plugin lives at the repository root. Its package name is `anthropic-cloak`, and its entry point is `anthropic-cloak.middleware.js`. Renaming the plugin does not automatically enable Anthropic API-key routes: you must configure the actual provider ID.

## Installation

Enter the following in Magpie's **Add a plugin** field:

```text
github:Lixsp11/anthropic-cloak
```

Or use the CLI:

```sh
magpie plugin add github:Lixsp11/anthropic-cloak
```

To load a local checkout, run this from the repository directory:

```sh
magpie plugin add .
```

Do not append `/packages/antigravity-cloak`, and do not enable this plugin and the original at the same time. This package has not been published to npm; use the GitHub spec or a local directory.

## Configuration

Open **Options** for the installed **Anthropic Cloak** plugin and set:

```json
{
  "providers": ["my-api"],
  "extraWords": [],
  "off": false
}
```

Replace `my-api` with the actual provider **ID** in Magpie, not its display name, hostname, or model name. Use `anthropic` only if that is the provider's actual ID.

| Option | Default | Meaning |
| --- | --- | --- |
| `providers` | `[]` | Exact, case-sensitive matches against `ctx.provider.id`, including API-key routes. Empty strings and non-string entries are ignored. |
| `accounts` | `["antigravity"]` | Matches the subscription kind in `ctx.provider.account`. This field is absent on API-key routes. |
| `extraWords` | `[]` | Additional text to split, matched case-insensitively. |
| `off` | `false` | When true, skips all rewriting. |

A match in either `accounts` or `providers` enables the middleware. The original default behavior is preserved: without `providers`, only the Antigravity subscription kind is selected. An empty `accounts: []` falls back to that default; it does not disable all matching.

At the `onProvider` stage, the middleware inserts a zero-width space (`U+200B`) after the first character of each matched string in system instructions. Ordinary user messages are left unchanged. Anthropic, Chat, Responses, and Gemini formats are supported. HTTP headers are not modified. Magpie must support the `onProvider` hook; the upstream project specifies v0.1.1283 or later.

## Limitations and risks

- This rewrites request text. It does not increase your balance or change a provider's credit eligibility rules.
- Its use may violate the target provider's terms and could result in account restrictions.
- Local tests cover matching and rewriting only. Live Anthropic requests, promotional-credit eligibility, and installation through Magpie's online flow have not been verified.
- This project is not affiliated with Anthropic and is not published under the `@magpie-community` npm namespace.

## Testing

Use a modern Node.js version with `node:test` and `structuredClone`, such as Node.js 20 or later. No dependency installation or Bun runtime is required.

```sh
npm test
```

The suite includes the original 15 cases, 12 provider-matching regression cases, and a standalone package entry-point check.

## Attribution and license

Based on `packages/antigravity-cloak` from [magpie-community/plugins](https://github.com/magpie-community/plugins). The upstream project credits [hexiaolv/magpie-antigravity-cloak](https://github.com/hexiaolv/magpie-antigravity-cloak) as its basis. The original MIT license and copyright notice are preserved in [LICENSE](LICENSE).
