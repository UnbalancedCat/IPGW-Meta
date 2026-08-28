# IPGW-Meta Agent 导航

本仓库的人类可读规范唯一来源是 [`docs/`](docs/README.md)。`agent/` 只提供执行索引、工作包依赖和交接状态，不得复制或改写产品规范。

## 开始工作

1. 阅读 [`docs/upgrade/plan.md`](docs/upgrade/plan.md)，核对 `plan_id: IPGW-META-V1` 与 `revision: 2026-08-28-r2`。
2. 阅读 [`docs/upgrade/status.md`](docs/upgrade/status.md) 和 [`docs/upgrade/migration-matrix.md`](docs/upgrade/migration-matrix.md)。
3. 从 [`agent/plans/stabilization-v1.md`](agent/plans/stabilization-v1.md) 选择依赖已满足的工作包。
4. 开始与结束工作时更新 [`agent/handoff.md`](agent/handoff.md)；正式里程碑状态只更新 `docs/upgrade/status.md`。

## 强制约束

- 不把密码、手机号、验证码、Cookie、CAS ticket、TGT、LT、原始认证响应或带查询参数的认证 URL 写入仓库。
- 不以 HTTP 发送任何凭据、Cookie 或 ticket，不禁用 TLS 验证，也不加入不安全降级开关。
- 不把“公网可访问”当作目标账号登录成功；成功判据见 [`PROTO-LOGIN-001`](docs/architecture/protocol-correctness.md#proto-login-001登录成功不变量)。
- 修改公共 SDK、CLI、JSON、退出码、配置或认证能力前，先更新对应 `docs/` 规范和 ADR。
- `agent/` 只能引用稳定 ID、依赖、修改边界和验收命令；发生冲突时一律以 `docs/` 为准。
- 真实网络证据必须遵守 [`docs/evidence/README.md`](docs/evidence/README.md) 的脱敏规则。

## 规范入口

- 架构：[`docs/architecture/overview.md`](docs/architecture/overview.md)
- 协议正确性：[`docs/architecture/protocol-correctness.md`](docs/architecture/protocol-correctness.md)
- 安全边界：[`docs/architecture/security.md`](docs/architecture/security.md)
- CLI / SDK / JSON：[`docs/reference/`](docs/reference/)
- 认证能力：[`docs/compatibility/auth-capabilities.md`](docs/compatibility/auth-capabilities.md)
- 配置迁移 / 发布：[`docs/operations/`](docs/operations/)
- 离线安装：[`docs/operations/offline-install.md`](docs/operations/offline-install.md)
- 真实验收：[`docs/operations/live-validation.md`](docs/operations/live-validation.md)
- 校园实验室：[`docs/runbooks/campus-lab.md`](docs/runbooks/campus-lab.md)
- Evidence：[`docs/evidence/README.md`](docs/evidence/README.md)
