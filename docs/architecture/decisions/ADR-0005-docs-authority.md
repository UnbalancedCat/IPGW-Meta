---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
adr: ADR-0005
status: accepted
---

# ADR-0005：文档权威边界

`docs/` 是产品、接口、安全和发布规范的唯一来源；`agent/` 只能保存 work package ID、依赖、修改范围、验收命令引用与短期交接。正式状态位于 `docs/upgrade/status.md`。CI 将通过 doccheck 校验 revision、稳定 ID、链接和 agent 引用，避免双重事实源漂移。
