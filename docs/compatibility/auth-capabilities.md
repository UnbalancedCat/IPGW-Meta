---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
---

# 认证能力矩阵

## AUTH-STATUS-001：能力状态词汇

| 状态 | 含义 |
|---|---|
| `observed_anonymous` | 可在官方匿名页面或公开脚本观察到，不代表真实账号路径可用 |
| `synthetic_covered` | 合成 fixture 与状态机测试通过，不代表真实网络通过 |
| `live_unverified` | 已实现但尚未完成当前版本真实校园网验证 |
| `live_verified` | 当前候选版本已按证据规范完成真实网络验证 |
| `supported` | 自动化、真实验证、文档和发布门禁均满足 |
| `detected_only` | 只识别挑战并安全失败，不执行挑战 |
| `unknown` | 无足够证据判断 |

状态可组合，但不得从匿名观察或第三方实现直接提升为 `supported`。真实证据失效或协议改变时必须降级。

## 当前矩阵

### AUTH-PASSWORD-001：CAS 用户名密码

- 状态：`synthetic_covered + live_unverified`。
- v1 行为：动态发现 CAS 表单和同源公开脚本公钥；当前官方协议的 RSA-512 PKCS#1 v1.5 字段只作为系统 PKI HTTPS 内的兼容封装，并受 [`ADR-0014`](../architecture/decisions/ADR-0014-cas-rsa-compatibility-envelope.md) 的来源、大小和明文容量约束。密码 POST 按 [`ADR-0015`](../architecture/decisions/ADR-0015-cas-submit-closed-fields.md) 只提交六个明确字段，不透传页面其他控件。经 HTTPS activation 后精确验证最终身份。
- 发布门禁：在校园有线与无线网络各完成一次真实验证。

### AUTH-QR-001：Terminal QR

- 状态：`observed_anonymous + synthetic_covered + live_unverified`。
- v1 行为：仅通过显式方法启用；由 TTY 呈现、SDK 轮询，不自动回退到其他登录方式。
- 发布门禁：至少在一种校园网络完成真实扫码闭环。

### AUTH-SMS-001：手机验证码

- 状态：`observed_anonymous + detected_only`。
- v1 行为：识别后返回 `interaction_required`，不发送、不提交验证码。
- 发布门禁：不阻塞 v1，但禁止声称已支持。

### AUTH-DEVICE-001：设备验证与信任设备

- 状态：`observed_anonymous + detected_only`。
- v1 行为：识别挑战并提供官方门户指引，不代替用户完成验证。
- 发布门禁：不阻塞 v1。

### AUTH-UNKNOWN-001：未识别挑战

- 状态：`unknown`。
- v1 行为：fail closed，返回 `interaction_required` 或 `protocol_changed`，不得误报成功或密码错误。
- 发布门禁：必须证明无秘密泄漏。

### AUTH-CONFLICT-001：异账号冲突与显式切换

- 状态：`synthetic_covered + live_unverified`。
- v1 行为：目标账号与当前在线账号不同时，默认在读取凭据前返回 `session_conflict`；只有显式 switch 才能注销。注销响应和最终 offline 验证任一失败时必须停止，不得继续认证。
- 真实限制：当前只有一个授权校园账号，不借用第二账号、不在真实环境主动制造冲突或切换。
- 发布门禁：完整合成状态机、凭据延迟读取和 logout failure 测试必须通过；缺少第二账号的 live 验证不阻塞 v1，release notes 必须明确 `live_unverified`。

## AUTH-CHALLENGE-001：统一挑战结果

任意认证方法都可能得到以下结果：成功、`authentication`、`interaction_required`、`protocol_changed`。挑战不得被误报为密码错误或登录成功。

普通 CAS HTML 登录页必须先按 HTML DOM 识别，再在移除 dormant script/template/hidden controls 后检查活动挑战；页面内 JavaScript 的括号或对象字面量不构成 JSONP 响应证据。只有覆盖整个响应的严格 JSON/JSONP envelope 才能被接受为结构化挑战，具体边界见 [`ADR-0013`](../architecture/decisions/ADR-0013-cas-html-before-jsonp.md)。

`interaction_required` 的结构化 details 只允许包含：

- `challenge_kind`：`sms_otp`、`device_verification`、`account_setup`、`qr_approval`、`unknown`；
- `origin_method`；
- `capability_status`；
- 可选 `session_binding`；v1 的会话绑定值使用 `cas_session`；
- `resume_mode`：只允许 `retry_in_tty`、`restart`、`official_portal`；
- `tty_required` 与可选稳定 `help_id`。

恢复方式只能由固定 `resume_mode` 表达，不增加未列出的自由文本动作或投递字段。不得包含手机号、验证码、QR payload、Cookie、ticket、TGT、LT、原始页面或认证 URL。

错误 `message` 可本地化，但实现必须按稳定 error code／challenge kind 使用固定脱敏模板，不得拼接上游错误或任意详情；自动化不得依赖 message。

## AUTH-QR-002：Terminal QR 契约

- `ExpectedUsername` 必填，最终在线 username 必须精确匹配。
- QR payload 只在内存中传给 InteractionHandler，SDK 负责轮询、取消、过期和最终页面解析。
- SDK 不启动浏览器、不假定 GUI、不持久化二维码会话。
- TTY 人类模式将二维码和说明写入 `stderr`；stdout 保留业务数据。
- 非 TTY、JSON 模式或无法安全呈现时不显示、不轮询，立即返回 `interaction_required`，CLI 退出码为 7。
- QR redirect 后仍必须解析最终页面；出现 OTP 或设备验证时继续返回相应挑战，不得宣告成功。

## AUTH-SMS-002：不可实测分支

当前没有可可靠触发手机验证码的测试条件。因此 v1 只实现识别、分类、脱敏错误和安全引导，合成测试不能被记录为真实支持。首次真实触发时只允许生成绑定候选、网络类型、来源认证方法、`interaction_required` 结果和固定能力状态的私有证据；不得增加投递目标、手机号、页面、URL、details 或自由文本。该运行保持 blocked，不能替代 password/QR 成功门禁。
