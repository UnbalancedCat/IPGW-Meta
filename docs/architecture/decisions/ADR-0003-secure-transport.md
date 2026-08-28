---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
adr: ADR-0003
status: accepted
---

# ADR-0003：安全传输

保留服务端注册所需的 HTTP service 文本，但在发送 redirect 下一跳前严格校验并截获 ticket；ticket 只用于通过系统 PKI 验证的 HTTPS activation。status、activation、logout 均 HTTPS-only，不提供 TLS 绕过、自动明文降级或不安全覆盖开关。匿名 HTTP 发现只产生不可信提示。
