---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
---

# 无 GUI、SSH 与无人值守认证手册

本手册适用于无桌面环境、SSH、systemd、NAS、容器和其他非交互调用。SDK 永远不启动浏览器，也不要求系统存在 GUI。

## HEADLESS-001：选择认证方式

- 无人值守环境优先使用 password 方法，并通过 `env` 或权限受限文件 provider 提供凭据；不要把密码放进命令行参数。
- SSH 且存在真实 TTY、终端能够完整呈现二维码时，可显式执行 `ipgw-meta login --method qr`，直接在终端扫码；不要求远端或本机存在 GUI。
- 如果终端尺寸、字体、颜色、复用器或转录策略导致二维码无法安全呈现，应停止该轮；在可信本地 TTY 重试或使用官方门户，不复制 QR payload、不生成图片文件，也不转发到聊天或日志。
- systemd、管道、CI、JSON 模式及 stdin/stderr 不具备安全终端呈现条件时，不尝试 QR。
- OS keyring 只适用于已正确解锁并可访问相应用户会话的环境；服务器默认不假定 keyring 可用。

## HEADLESS-002：非 TTY 挑战行为

当认证要求二维码、手机验证码、设备验证或其他人工操作时：

1. 不输出挑战 payload、手机号、原始页面或 URL；
2. 不启动浏览器，不隐式等待，不从 QR 回退密码；
3. stderr 给出脱敏的人类指引；JSON stdout 只给稳定 `interaction_required` envelope；
4. 立即以退出码 `7` 结束，`retryable` 默认 `false`，明确说明“需要安全 TTY 或官方门户”；不得只提示“打开浏览器”而假定设备有 GUI。

脚本应依据错误 code/退出码分支，不匹配本地化 message。自动重试 `interaction_required` 会制造噪声且不能解决人工挑战。

## HEADLESS-003：systemd 建议

- 使用 `EnvironmentFile=` 或权限受限 credential file，文件仅允许运行用户读取；不得把密码直接写入 unit、参数或日志。
- 使用明确 profile 和 bind IP，避免多网卡环境选择漂移。
- 对退出码 3 可做有上限、带退避的网络重试；对 4、5、6、7 必须停止并告警。
- stdout 可收集 JSON 业务结果，stderr 只收集脱敏诊断；日志系统仍需设置合理保留期与访问权限。
- 不将真实认证输出附加到 issue 或公共 evidence。

## HEADLESS-004：处理常见结果

| 结果 | 操作 |
|---|---|
| exit 0 / offline status | 命令成功；offline 本身不是错误 |
| exit 3 / `network` | 检查网络、DNS、系统时间与 TLS；不要启用 HTTP 降级 |
| exit 4 / `authentication` | 停止自动重试，核对凭据或由用户处理账号状态 |
| exit 5 / `session_conflict` | 人工确认目标账号；仅显式 switch 才允许注销现有会话 |
| exit 6 / `protocol_changed` | 保留脱敏版本/网络信息并上报；不得猜测页面或绕过验证 |
| exit 7 / `interaction_required` | 转到安全 TTY 或官方门户；OTP 当前仅 detected-only |
| exit 130 | 用户取消或 context 取消，不应当作认证失败重试 |

## HEADLESS-005：安全排障清单

只记录程序版本、操作系统、网络类型、有无 TTY、认证方法、错误 code、challenge kind 与发生时间。提交证据前遵守 [`EVID-REDACT-001`](../evidence/README.md)，不得通过 verbose 模式获取原始认证材料。
