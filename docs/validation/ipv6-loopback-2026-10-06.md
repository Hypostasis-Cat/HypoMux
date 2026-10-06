# IPv6 回环失败与 Teredo 分类检查

## 结论

两个既有桌面测试在本机仍失败，不能声明 IPv6 验收通过。系统自身的 IPv6 回环也不可用：

- `ping -6 ::1 -n 1` 返回 General failure；沙箱外结果相同。
- 桌面 ICMP 探测返回 `Icmp6SendEcho2 failed: winapi error #11050`。
- 从 `::1` 直接连接本机 TCP6 监听器，被 Windows 以 socket access permissions 拒绝；连接完全绕过 HypoMux Core。
- 本机有活动的 Mihomo TUN，配置为 `ipv6: false`、`strict-route: true`；以太网 IPv6 绑定关闭。它们是环境线索，尚不能据此确认具体拦截组件。
- 用户需要保留当前 TUN 才能使用 ChatGPT，本轮没有改动系统网络或代理配置。

## 代码修改

- 从 Windows 接口类型识别隧道，并补充 Teredo、ISATAP、6to4 名称识别，纳入现有虚拟网卡隐藏规则。即使接口重命名，原生隧道类型仍可识别。
- 保留 ICMPv6 创建句柄、发送和回复状态的具体错误，避免只显示无原因的丢包。
- IPv6 Core 恢复测试先验证独立的本机 TCP6 连接能力；环境不可用仍然 FAIL，不改成 SKIP，也不移除原有恢复验收。
- 隐藏只是显示分类，沿用既有选择保留策略，不自动清除已选网卡，不禁用系统 Teredo。

## 验证

- 网卡分类、隐藏设置持久化、IPv6 首选源地址、ICMP 原生错误与句柄清理、真实 Core 的 IPv4 恢复与重新加入测试通过。
- 前端 `adapterVisibility.test.ts` 三项通过。
- `go vet ./internal/services`、桌面构建、`git diff --check` 通过。
- 两项真实 IPv6 回环验收仍受上述系统限制影响；需要在允许 IPv6 回环的环境重新验证。
- 未替换已安装应用，未提交或推送 GitHub。
