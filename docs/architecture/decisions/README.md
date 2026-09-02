---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
---

# 架构决策记录

| ADR | 状态 | 决策 |
|---|---|---|
| [ADR-0001](ADR-0001-product-boundaries.md) | accepted | 单 Go SDK、三入口、NEU-first 内部边界 |
| [ADR-0002](ADR-0002-compatibility-and-cutover.md) | accepted | 工作流兼容与 1.x legacy 切换策略 |
| [ADR-0003](ADR-0003-secure-transport.md) | accepted | ticket 截获与 HTTPS-only |
| [ADR-0004](ADR-0004-challenge-model.md) | accepted | QR 与验证码统一人工挑战模型 |
| [ADR-0005](ADR-0005-docs-authority.md) | accepted | `docs/` 唯一规范源、`agent/` 派生索引 |
| [ADR-0006](ADR-0006-transactional-config-migration.md) | accepted | 旧凭据显式决策、无秘密 journal 与可恢复迁移事务 |
| [ADR-0007](ADR-0007-immutable-candidate-promotion.md) | accepted | 一次构建的不可变候选与原样晋升 |
| [ADR-0008](ADR-0008-offline-transactional-installer.md) | accepted | 离线 acquisition 与事务安装共用验证链 |
| [ADR-0009](ADR-0009-separated-live-test-plane.md) | accepted | 真实认证分离管理面、测试面与私有交互面 |
| [ADR-0010](ADR-0010-macos-trusted-system-path-alias.md) | accepted | macOS 固定 `/var` 系统别名作为受验证路径锚点 |
| [ADR-0011](ADR-0011-nonblocking-m0-governance.md) | accepted | M0 残余治理继续跟踪但不阻塞发布流水线 |
| [ADR-0012](ADR-0012-bounded-anonymous-acid-redirect.md) | accepted | `ac_id` 只在无 Cookie 的同网关匿名窗口内有界跟随一次 |

新增或改变公共行为时新建 ADR，不改写已接受 ADR 的历史结论；被替代的记录标为 `superseded` 并链接后继记录。
