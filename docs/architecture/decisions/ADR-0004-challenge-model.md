---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
adr: ADR-0004
status: accepted
---

# ADR-0004：人工挑战模型

任意认证方法都可能产生 `interaction_required`。Terminal QR 是 v1 显式方法，由 SDK 管理会话并通过内存 InteractionHandler 呈现；非 TTY/JSON 不展示也不轮询。手机验证码仅识别和安全指引，不发送或提交。所有挑战最终仍受 `ExpectedUsername` 身份校验约束。
