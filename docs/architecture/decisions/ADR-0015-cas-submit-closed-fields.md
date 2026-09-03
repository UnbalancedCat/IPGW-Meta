---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
adr: ADR-0015
status: accepted
---

# ADR-0015：CAS 密码提交采用六字段闭包

2026-09-03 对 Candidate `v1.0.0-1142977d0f7e-33723917129.1` 的 BHKDesktop Win11 物理有线正式密码窗口证明，RSA-512 兼容加密已经完成且 CAS POST 已发送，但官方 CAS 直接返回 HTTP 500；请求未发生 DNS、连接、TLS 或超时错误，未得到 ticket，未进入 gateway activation，也未产生 logout。随后在明确 offline、Wi-Fi/TUN/IPv4 cover route 均不存在的同一拓扑中执行零凭据匿名 form-shape：页面与动态 form action 均精确保留预期 `service`，action 等于最终登录页 URL，因此 POST target 假设被排除；真实 form 含 11 个具名 input，其中 5 个不属于当前六个已知 CAS 字段。该诊断共执行 4 个匿名 GET 和 3 次 DNS，不读取凭据、不发送 CAS POST、不 activation、不 logout，结束后人工复核仍为 offline。

失败实现把 form 中所有具名 input 克隆到凭据 POST，再覆盖 `rsa`、`ul`、`pl` 与 `_eventId`。这会把不可信页面中新出现的装饰性、浏览器专用或其他认证方式控件自动扩大为秘密请求输入。作为交叉线索，固定的 legacy v0.1.3 与仓库研究快照中的活跃实现都从空表单开始，只发送 `rsa`、`ul`、`pl`、`lt`、`execution`、`_eventId`；两者同时存在其他不安全行为，因而只用于缩小假设，不能替代当前安全规范和真实验证。

决定如下：

- 密码 POST 必须新建 closed-world `application/x-www-form-urlencoded` 值集合，只允许 `rsa`、`ul`、`pl`、`lt`、`execution`、`_eventId` 六个键。
- `lt` 与 `execution` 必须分别来自本次动态 form，且各自恰好出现一次并为非空值；重复、缺失或空值均在读取凭据和 POST 前返回 `protocol_changed`。
- `rsa` 只使用本次动态公钥对 UTF-8 `username + password` 加密后的 Base64 值；`ul`、`pl` 按现有 JavaScript UTF-16 code-unit 契约计算；`_eventId` 固定为 `submit`。页面提供的同名值一律不得覆盖本次计算结果。
- form action 继续动态解析并执行同源 HTTPS 校验；ticket redirect 和最终身份仍分别受 `PROTO-REDIRECT-001` 与 `PROTO-LOGIN-001` 约束。本决策不允许固定 POST URL、放宽 redirect、发送 HTTP 凭据或根据 HTTP 状态宣告成功。
- 其他具名 input、未知隐藏字段、按钮、禁用控件和重复字段不得自动透传。未来官方协议若确实要求新字段，必须先以无凭据匿名 shape 证明、更新规范与显式 allowlist，并重新完成合成和真实验证。
- 回归测试必须证明动态 action 仍被使用、六键集合精确闭合、额外字段与泄漏 canary 不进入请求、重复 `lt`／`execution` 在凭据读取前失败，以及密文和错误不泄漏用户名、密码或动态状态值。

该修复只收窄 CAS 凭据 POST 的输入面，不增加请求、凭据读取、挑战、activation、logout 或成功条件。能力继续保持 `synthetic_covered + live_unverified`，直到新 Candidate 在授权校园窗口完成登录、最终同账号 online 复核与有界 cleanup。
