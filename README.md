# Anthropic Cloak

独立的 Magpie 网关中间件，由 MIT 授权的 [Antigravity Cloak](https://github.com/magpie-community/plugins/tree/main/packages/antigravity-cloak) 派生，增加按供应商 ID 显式启用的 `providers` 配置。

本仓库已将插件放在根目录，包名为 `anthropic-cloak`，入口为 `anthropic-cloak.middleware.js`。仅更名不会自动启用 Anthropic API Key 路由，仍需配置实际供应商 ID。

## 安装

将此提交推送到 GitHub 后，在 Magpie 的添加插件输入框填写：

```text
github:Lixsp11/anthropic-cloak
```

或使用命令：

```sh
magpie plugin add github:Lixsp11/anthropic-cloak
```

也可以在仓库目录内直接加载本地副本：

```sh
magpie plugin add .
```

不要追加 `/packages/antigravity-cloak`，也不要同时启用原版和此副本。此包尚未发布到 npm，直接使用 GitHub 地址或本地目录。

## 配置

在已安装的 **Anthropic Cloak** 的 Options 中设置：

```json
{
  "providers": ["my-api"],
  "extraWords": [],
  "off": false
}
```

把 `my-api` 替换为你在 Magpie 中的实际供应商 **ID**，不是显示名称、域名或模型名称。只有实际 ID 为 `anthropic` 时才填 `anthropic`。

| 配置 | 默认值 | 含义 |
| --- | --- | --- |
| `providers` | `[]` | 精确匹配 `ctx.provider.id`，可用于 API Key 路由；区分大小写，忽略空字符串和非字符串元素 |
| `accounts` | `["antigravity"]` | 匹配订阅登录类型 `ctx.provider.account`；API Key 路由没有该字段 |
| `extraWords` | `[]` | 额外要拆分的文本，不区分大小写 |
| `off` | `false` | 为真时完全跳过 |

`accounts` 或 `providers` 满足任意一种匹配即生效。保留原版默认行为：不配置 `providers` 时，只按 Antigravity 订阅类型匹配。`accounts: []` 仍回退到这个默认值，不表示关闭全部匹配。

中间件在 `onProvider` 阶段，只对系统指令中命中的文本在首字符后插入 `U+200B` 零宽空格；普通用户消息保持不变。支持 Anthropic、Chat、Responses 和 Gemini 格式，不修改 HTTP 请求头。需要支持 `onProvider` 的 Magpie（上游说明为 v0.1.1283 或更新版本）。

## 限制与风险

- 这是请求文本改写，不会增加余额或改变服务商规定的额度适用范围。
- 可能违反目标服务商条款，并带来账号限制风险。
- 本地测试仅验证匹配和改写逻辑，未验证真实 Anthropic 请求、赠送额度可用性或 Magpie 在线安装流程。
- 本仓库与 Anthropic 官方无关联，也未发布到 `@magpie-community` 的 npm 命名空间。

## 测试

需要支持 `node:test` 和 `structuredClone` 的现代 Node.js（例如 Node.js 20+），无需安装依赖或 Bun：

```sh
npm test
```

测试包含原版 15 个用例、新增的 12 个供应商匹配用例，以及独立包入口检查。

## 来源与许可证

源自 [magpie-community/plugins](https://github.com/magpie-community/plugins) 的 `packages/antigravity-cloak`；原项目说明其基于 [hexiaolv/magpie-antigravity-cloak](https://github.com/hexiaolv/magpie-antigravity-cloak)。保留原 MIT 许可证和版权声明，详见 [LICENSE](LICENSE)。
