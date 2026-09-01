---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
derived: true
---

# Stabilization v1 工作包索引

本页只定义执行顺序和边界；所有行为与验收含义由链接的 `docs/` 稳定 ID 决定。WP 状态不得写在这里。

| WP ID | 依赖 | 允许修改范围 | 权威规范 | 验收命令 / 证据 |
|---|---|---|---|---|
| WP-M0-PREFLIGHT | 无 | 只读 Git/GitHub/tool/worktree 核验 | [`REL-WINDOW-001`](../../docs/operations/release.md) | refs/tool hash/secret scan/test 结果；无写入 |
| LAB-DISCOVER | 无 | ZOS 只读能力检查 | [`REL-LAB-002`](../../docs/runbooks/campus-lab.md) | 隔离能力记录；不创建 VM |
| WP-R2-SPEC-A | WP-M0-PREFLIGHT | plan/status/ADR/handoff | [`ADR-0005`](../../docs/architecture/decisions/ADR-0005-docs-authority.md) | 文档 diff 与链接检查 |
| WP-R2-SPEC-B | WP-R2-SPEC-A | release/offline/live/evidence/auth docs | [`ADR-0009`](../../docs/architecture/decisions/ADR-0009-separated-live-test-plane.md) | 稳定 ID 与交叉链接检查 |
| WP-R2-SPEC-C | WP-R2-SPEC-B | revision、doccheck、Agent 派生索引 | [`EVID-REVIEW-001`](../../docs/evidence/README.md) | `go test ./internal/doccheck`; `doccheck --check` |
| WP-M0-FREEZE-AUDIT | WP-R2-SPEC-C | `.gitattributes`、index 审计 | [`SEC-HISTORY-001`](../../docs/architecture/security.md) | EOL/name-status/numstat/secret scan/tests；不提交 |
| WP-M0-FREEZE-COMMIT | WP-M0-FREEZE-AUDIT | staged tree、安全 archive、受限备份 | [`REL-APPROVAL-001`](../../docs/operations/release.md) | index 二次核对、archive scan、backup hash |
| WP-M0-REWRITE-LOCAL | WP-M0-FREEZE-COMMIT | 隔离 mirror 与 replace rules | [`SEC-HISTORY-001`](../../docs/architecture/security.md) | fsck、全 refs/reflog scan、tree/tag 对照 |
| WP-M0-REWRITE-REMOTE | WP-M0-REWRITE-LOCAL | 精确远端 commit/tag refs | [`REL-APPROVAL-001`](../../docs/operations/release.md) | atomic per-ref lease push；远端复核 |
| WP-M0-VERIFY-REHOME | WP-M0-REWRITE-REMOTE | clean clone 与旧工作区隔离记录 | [`SEC-HISTORY-001`](../../docs/architecture/security.md) | fresh clone refs/tags/scan/tests/tree |
| WP-M0-RENAME-GOVERN | WP-M0-VERIFY-REHOME | module/origin、仓库名、rulesets | [`ADR-0002`](../../docs/architecture/decisions/ADR-0002-compatibility-and-cutover.md) | fresh clone；main/tag 保护核验 |
| WP-BASELINE-VERIFY | WP-M0-RENAME-GOVERN | 只读 M1/M2 基线 | [`REL-CI-001`](../../docs/operations/release.md) | test/race/vet/doccheck/secret scan |
| WP-M2-CONFIG-CLOSE | WP-BASELINE-VERIFY | `internal/config` 与迁移测试 | [`MIG-TRANSACTION-001`](../../docs/operations/config-migration.md) | 失败注入、Unix 权限、keyring backend |
| WP-M2-INSTALL-UNIX | WP-M2-CONFIG-CLOSE | `install.sh` 与 Unix 测试 | [`REL-INSTALL-001`](../../docs/operations/offline-install.md) | 离线/路径/权限/failpoint 测试 |
| WP-M2-INSTALL-WINDOWS | WP-M2-CONFIG-CLOSE | `install.ps1` 与 Windows 测试 | [`REL-INSTALL-002`](../../docs/operations/offline-install.md) | ACL/reparse/事务 failpoint 测试 |
| WP-M2-INSTALL-NATIVE | WP-M2-INSTALL-UNIX, WP-M2-INSTALL-WINDOWS | 原生 runner 矩阵 | [`REL-INSTALL-003`](../../docs/operations/offline-install.md) | 六平台 smoke；三代表平台完整故障矩阵 |
| WP-M3-LIVEGATE-SCHEMA | WP-M2-INSTALL-NATIVE | live-gate schema/types/tests | [`REL-LIVEGATE-001`](../../docs/operations/live-validation.md) | schema、枚举、退出码和泄漏测试 |
| WP-M3-LIVEGATE-RUNNER | WP-M3-LIVEGATE-SCHEMA | maintainer-only runner | [`REL-LIVEGATE-002`](../../docs/operations/live-validation.md) | 合成状态机、清理权、durability 测试 |
| WP-M3-CANDIDATE | WP-M3-LIVEGATE-RUNNER | candidate workflow/manifest/packaging | [`REL-ATTEST-001`](../../docs/operations/release.md) | 一次构建、digest、attestation 测试 |
| WP-M3-PROMOTION | WP-M3-CANDIDATE | promotion workflow/lock 校验 | [`REL-PROMOTION-001`](../../docs/operations/release.md) | no-build、签名、draft/re-download 测试 |
| WP-M0-NONBLOCKING-GATES | WP-M3-PROMOTION, LAB-DISCOVER | 决策/发布/安全/状态、Makefile gate 与合成测试 | [`ADR-0011`](../../docs/architecture/decisions/ADR-0011-nonblocking-m0-governance.md) | M0 状态无关性；M1/M2/M3 缺失/重复/错误状态 fail closed |
| LAB-PROVISION | LAB-DISCOVER, WP-M3-PROMOTION | 管理/测试 VM 与匿名预检 | [`REL-LAB-001`](../../docs/runbooks/campus-lab.md) | topology/status 预检；无凭据 |
| RC-BUILD | WP-M0-NONBLOCKING-GATES, LAB-DISCOVER | 受保护 main 的 candidate-set | [`REL-CANDIDATE-001`](../../docs/operations/release.md) | artifact ID/digest/hash/attestation |
| LAB-TRANSFER | RC-BUILD | 本地下载与远端私有目录 | [`REL-TRANSFER-001`](../../docs/operations/live-validation.md) | 本地/远端一致 hash；远端无重建 |
| LAB-PASSWORD-NAS/BHK | LAB-TRANSFER | NAS/BHK 私有 TTY 和私有 evidence | [`REL-LIVE-MATRIX-001`](../../docs/operations/live-validation.md) | 指定 password suites |
| LAB-QR-NAS | LAB-PASSWORD-NAS/BHK | NAS 私有 TTY 和私有 evidence | [`AUTH-QR-002`](../../docs/compatibility/auth-capabilities.md) | terminal-qr suite |
| LAB-EVIDENCE | LAB-QR-NAS | 私有 bundle 审阅、公开摘要/lock | [`EVID-BUNDLE-001`](../../docs/evidence/README.md) | schema/hash/禁止字段人工复核 |
| WP-M3-RELEASE | LAB-EVIDENCE | 签名 tag、draft 与 release metadata | [`REL-PROMOTION-001`](../../docs/operations/release.md) | 签名验证、逐资产下载/hash |
| WP-SECURE-DISPOSAL | WP-M3-RELEASE | 已批准旧工作区/敏感备份目标 | [`REL-APPROVAL-001`](../../docs/operations/release.md) | 精确目标确认与处置记录 |

## 并行与冲突约束

- M0 冻结、历史重写、远端更新、rehome 与仓库治理严格串行；每个高风险窗口重新取得维护者批准。
- Unix 与 Windows 安装器实现可在配置契约稳定后分开执行，但原生矩阵只能测试已冻结的同一实现版本。
- live-gate、candidate 和 promotion 按表中依赖串行；真实实验室不重建候选。
- NAS/BHK 的真实认证窗口由维护者执行私有交互；Agent 只处理固定白名单 evidence。

## 全局完成检查

```text
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
secret scan
go run ./cmd/doccheck --check
六平台原生安装门禁
```

命令尚未在仓库提供时，对应 WP 仍未完成；不得用手工口头确认代替门禁。
