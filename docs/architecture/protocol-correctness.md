---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
---

# 协议正确性规范

## PROTO-DISCOVERY-001：发现优先

- 每次认证动态解析 CAS form action、`lt`、`execution`、公开登录脚本和 RSA 公钥。
- `ac_id` 发现只允许匿名 GET，最多读取两个有界响应：初始 captive 响应，以及至多一个经过严格校验的中间 redirect 响应。中间 Location 必须解析后仍为同一网关 host、`http` 或 `https` 默认端口、无 userinfo、fragment 和 query，路径不得含控制字符、反斜杠或点段；发现客户端不得创建 Cookie Jar，也不得发送账号、Cookie、ticket 或凭据。每个响应先检查同网关 Location 中唯一的 1–10 位十进制 `ac_id`，再检查有界正文中的唯一候选；只有首个响应允许继续一次，第二个响应后无论是否还有 Location 都必须停止。非法 Location、冲突/过多候选、第三跳需求或无法证明的形状统一返回 `protocol_changed`，不得按网卡类型、历史常量或公网可达性猜测 `ac_id`。
- 状态解析必须明确区分 JSON、JSONP、HTML 和未知响应；先判断内容类型/结构，再做业务解析。
- legacy CSV 仅在去除首尾空白后为单个无内嵌 CR/LF 的 CSV record、字段数至少为 9、位置 0 是合法 username 且位置 8 是全局单播 IPv4 时，才构成 online 身份证据；任一身份条件缺失或无效都返回 `protocol_changed`。其他位置不得覆盖或冲突推断 username/IP，也不得参与 online/offline 判定。
- legacy CSV 的位置 6/7/11 只形成一个 all-or-nothing 的可选摘要候选：位置 6 和 7 必须同时是 `int64` 范围内的非负十进制整数，且位置 11 在存在且非空时必须可精确转换为余额 minor units，才返回完整 `OnlineSummary`；否则整个摘要为 unavailable（`nil`），不得返回部分摘要或因可选摘要不可用而否定已经成立的 online 身份。未解释的非身份列保持 opaque，不得猜测其语义。
- 上述可选摘要降级只适用于 positional legacy CSV。JSON/JSONP 的显式命名字段一旦出现就承诺对应语义；无效、部分或 alias 冲突的显式 summary 仍返回 `protocol_changed`。
- 协议缓存不包含秘密，按可靠的网络上下文隔离，最长 7 天；只有经过业务成功和最终身份验证的值可写入。绑定 IP 不是网络身份，不能单独作为 cache key 或“同一网络”的证据。
- v1 尚无可靠 network fingerprint，因此禁用持久协议缓存的读取、写入与 fallback；每次操作都执行动态发现，发现失败时返回 `protocol_changed`。未来只有在能够可靠判定同一网络后，才可启用已验证缓存 fallback。
- 猜测式恢复只能作为显式诊断，不能默认启用，也不能提升证据等级。

## PROTO-REDIRECT-001：CAS ticket 截获

CAS 注册的 service 可保持 HTTP 字符串以兼容服务端，但客户端必须在真正发送下一跳之前：

1. 解析 Location；
2. 校验 host 精确为允许的 NEU gateway、端口符合策略、路径精确匹配 activation 入口；
3. 确认只含预期 ticket 参数；
4. 返回 `http.ErrUseLastResponse` 阻止 HTTP 请求；
5. 从内存读取 ticket，并仅通过正常系统 PKI 验证的 HTTPS activation 使用。

恶意 host、userinfo、非预期端口、路径穿越、额外敏感参数或不可解析 Location 一律返回 `protocol_changed`。ticket 不得进入日志、Observer、错误、JSON、缓存或 fixture。

## PROTO-TRANSPORT-001：HTTPS-only

status、activation 和 logout 必须使用 HTTPS；禁止 `InsecureSkipVerify`、自动 HTTP 降级和不安全覆盖开关。HTTP 仅可用于完全匿名、不携带 Cookie/ticket/账号/凭据的 captive portal 或 `ac_id` 发现，并只生成不可信提示。HTTPS 不可用时返回 `network` 或 `protocol_changed`，不得伪装为“离线”或“校外”。

## PROTO-LOGIN-001：登录成功不变量

登录成功必须同时满足：

1. 认证/activation 业务响应明确成功；
2. 最终状态为 online；
3. 最终 username 与请求的 `ExpectedUsername` 精确相等。

公网可访问、任意账号已在线、HTTP 2xx 或页面含“成功”字符串都不是充分条件。同账号在线返回 `AlreadyOnline` 且不得读取凭据；异账号默认返回 `session_conflict`。只有显式 switch 才能先注销，注销响应和离线复核任一步失败都必须中止。

## PROTO-LOGOUT-001：幂等注销

Logout 在原本离线时返回成功结果 `AlreadyOffline`；在线时必须检查 HTTPS 响应、网关业务结果并复核离线状态。不得仅因请求发出或返回 2xx 宣告成功。

## PROTO-IO-001：网络边界

所有网络方法接受 `context.Context`，设置连接/头/总时限，限制响应体大小，并保留 `context.Canceled` 与 `context.DeadlineExceeded`。Observer 只接收固定脱敏事件，不能接收原始 URL、header、body 或任意 map。
