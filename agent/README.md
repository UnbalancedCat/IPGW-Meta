---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
derived: true
---

# Agent 执行区

`agent/` 是模型和自动化执行者的派生索引，不是产品规范。唯一规范源是 [`docs/`](../docs/README.md)。

## 使用顺序

1. 核对 [`docs/upgrade/plan.md`](../docs/upgrade/plan.md) 的 `plan_id` 与 `revision`。
2. 从 [`plans/stabilization-v1.md`](plans/stabilization-v1.md) 选择依赖已满足的 WP。
3. 阅读该 WP 引用的全部 `docs/` 稳定 ID；若实现与规范冲突，停止并先更新 docs/ADR。
4. 在 [`handoff.md`](handoff.md)登记执行者、WP、修改边界和下一步。
5. 运行 WP 验收命令；正式完成状态只写入 [`docs/upgrade/status.md`](../docs/upgrade/status.md)。

## 内容边界

允许：

- WP ID、依赖和并行限制；
- 允许修改的目录/子系统；
- 对 `docs/` 稳定 ID 的链接；
- 验收命令和简短交接状态。

禁止：

- 复制公共 API、CLI、JSON、协议、安全、迁移或发布规则；
- 在此维护第二份里程碑状态、需求或兼容矩阵；
- 存放任何真实认证材料、原始网络响应或未脱敏日志。

发生冲突时，权威顺序为：已接受 ADR → 专题规范 → 批准计划 → `agent/` 执行索引。若这些 docs 之间互相冲突，应暂停对应 WP 并通过新 ADR/修订消除冲突。
