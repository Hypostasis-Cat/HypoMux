# AI 服务与中转站接入

模型配置按服务实际提供的接口选择协议，模型名称不决定协议：

| 服务提供的接口 | 协议选项 |
| --- | --- |
| `/chat/completions` | OpenAI compatible · Chat Completions |
| `/responses` | OpenAI compatible · Responses |
| `/messages` | Anthropic · Messages |

API Base URL 支持域名根地址、版本/自定义前缀和完整接口地址。根地址默认补 `/v1`；例如 `https://relay.example`、`https://relay.example/v1` 和 `https://relay.example/v1/chat/completions` 都可以用于 Chat Completions。自定义前缀不额外添加版本号；无版本接口可以填写完整地址，例如 `https://relay.example/chat/completions`。模型列表使用对应前缀下的 `/models`，服务不支持列表时可手动填写模型 ID。

认证方式默认随协议选择：OpenAI 两种协议使用 Bearer，Anthropic 使用 `x-api-key`。Anthropic 中转站如果要求 `Authorization: Bearer`，在“认证方式”中选择 Bearer。模型列表和对话使用相同的认证设置。远程地址要求 HTTPS；本机回环地址支持 HTTP。

“保存并测试工具调用”使用正常对话的自动工具模式，验证模型能否正确调用一个无副作用的测试工具并原样返回参数。模型需要支持工具调用才能执行网络操作；模型列表中可见不代表它一定支持工具调用。不会因为请求失败自动切换运营商或重试操作。

切换地址、协议、认证方式、模型或实际密钥时，保存成功后自动开始新的上下文。旧记录保留在界面中，不再发送到新配置；切回之前的配置也从新上下文开始。相同配置重复保存或等价地址写法不会重置上下文。跨地址或协议不沿用旧密钥，需重新填写。

上下文标识与配置一起加密保存，重启后继续有效。旧版本没有归属标记的记录仅供查看，升级时不会自动发送给当前服务。损坏的配置可以在设置中重新保存，无需删除 C 盘缓存。

单次任务中的 Chat Completions `reasoning_content`、Anthropic thinking/signature 内容块、Responses 输出项会保留到下一轮工具结果请求，避免推理模型因缺少前序字段拒绝请求。这些协议字段仅保存在本次任务内存中。Responses 使用 `store: false` 并回传输出项，不依赖服务端会话 ID。

实现参考：[CC Switch 协议与认证适配](https://github.com/farion1231/cc-switch/blob/main/src-tauri/src/proxy/providers/claude.rs)、[OpenAI Responses 迁移文档](https://developers.openai.com/api/docs/guides/migrate-to-responses)。验证使用本机模拟服务，真实运营商的权限、额度及私有协议扩展仍需用实际配置测试。
