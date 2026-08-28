---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
---

# JSON CLI 参考

## JSON-ENVELOPE-001：唯一顶层结构

JSON 模式 stdout 恰好输出一个对象并以一个换行结束：

```json
{
  "schema_version": 1,
  "command": "login",
  "ok": false,
  "error": {
    "code": "interaction_required",
    "message": "登录需要人工验证",
    "retryable": false,
    "details": {
      "interaction": {
        "challenge_kind": "sms_otp",
        "origin_method": "password",
        "capability_status": ["observed_anonymous", "detected_only"],
        "session_binding": "cas_session",
        "resume_mode": "official_portal",
        "tty_required": true,
        "help_id": "AUTH-SMS-001"
      }
    }
  }
}
```

成功时出现 `data`，失败时出现 `error`，两者必须且只能出现一个。`schema_version` 为整数 `1`；`command` 使用规范命令名，参数解析前错误使用 `cli`。

## JSON-ERROR-001：错误对象

`error` 固定包含：

- `code`：[`SDK-ERROR-001`](go-sdk.md) 的稳定字符串；
- `message`：可本地化，仅供人阅读；实现必须按稳定 error code／challenge kind 选择固定的脱敏模板，不得拼接上游错误、原始响应或任意详情；
- `retryable`：对同样输入立即重试是否有意义；
- `details`：按 code 限制的对象，没有详情时为 `{}`。

自动化不得依赖 `message`，只能依赖 code、已记录的 details 字段和退出码，并必须忽略未知字段。`interaction_required` 的 details 使用脱敏 `interaction` 对象；其恢复方式只由固定枚举 `resume_mode` 表达，不提供自由文本动作或投递提示。该对象不得包含 QR payload、手机号、验证码、Cookie、ticket、URL 或原始响应。

v1 的 interaction wire 字段固定为 `challenge_kind`、`origin_method`、`capability_status`、可选 `session_binding`、`resume_mode`、`tty_required` 与可选 `help_id`。`resume_mode` 只允许 `retry_in_tty`、`restart`、`official_portal`；`session_binding` 在会话绑定挑战中使用 `cas_session`。未知恢复方式不得作为自由文本透传。

## JSON-VALUE-001：值编码

- 时间：UTC RFC3339 字符串。
- 时长：整数秒，字段以 `_seconds` 结尾。
- 流量：整数 bytes，字段以 `_bytes` 结尾。
- 金额：`{"currency":"CNY","minor_units":1234}`，不得使用浮点数。
- IP：规范字符串；缺失值为 JSON `null`，不得用空字符串冒充未知。
- 枚举使用本文与 SDK 参考定义的小写字符串。

## JSON-STATUS-001：状态示例

离线 status 是成功：

```json
{
  "schema_version": 1,
  "command": "status",
  "ok": true,
  "data": {
    "network": "reachable",
    "session": "offline",
    "username": null,
    "online_ip": null,
    "observed_at": "2026-08-27T00:00:00Z",
    "summary": null
  }
}
```

网络或 TLS 失败必须输出 `ok:false`/`code:"network"`，不能伪装成上述 offline 状态。

## JSON-COMPAT-001：演进规则

- v1 可向对象增加字段和增加非破坏性枚举；消费者必须忽略未知字段。
- 删除、改名、改变类型或改变既有字段语义需要提升 `schema_version`。
- 同一调用不得输出多个 envelope、进度行或 ANSI 控制码到 stdout。
- stderr 可输出脱敏诊断，但脚本不得依赖其内容。
- JSON envelope 的退出码映射必须与 [`CLI-EXIT-001`](cli.md) 一致。
