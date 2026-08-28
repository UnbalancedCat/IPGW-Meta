---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
adr: ADR-0002
status: accepted
---

# ADR-0002：兼容与切换

1.x 兼容用户工作流、配置迁移和命令意图，不兼容旧输出字节、错误退出码或危险行为。旧安装升级后维持 legacy；v1 后新安装默认 meta；已持久化模式永不被升级静默覆盖。模式优先级为显式参数、环境变量、launcher 配置、安装批次默认值。legacy 最早在 2.0 移除。

迁移清单见 [`docs/upgrade/migration-matrix.md`](../../upgrade/migration-matrix.md)，发布门禁见 [`REL-BUNDLE-001`](../../operations/release.md)。
