---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
status: approved
---

# 实施状态

本页是里程碑状态的正式来源；工作包级执行交接位于 [`agent/handoff.md`](../../agent/handoff.md)。状态仅使用 `not_started`、`in_progress`、`blocked`、`complete`。

| 里程碑 | 状态 | 发布门禁 | 规范入口 |
|---|---|---|---|
| M0 文档与紧急安全 | in_progress | 历史秘密清理、secret scan、日志脱敏、自更新禁用 | [计划](plan.md#34-m0-历史清理)、[安全](../architecture/security.md) |
| M1 协议正确性与 SDK | complete | HTTPS-only、ticket 截获、动态发现、typed errors、最终身份校验 | [协议](../architecture/protocol-correctness.md)、[SDK](../reference/go-sdk.md) |
| M2 三入口、配置与自动化 | in_progress | 三二进制、JSON/退出码、profiles、迁移和原子安装 | [CLI](../reference/cli.md)、[迁移](../operations/config-migration.md) |
| M3 v1 候选与稳定发布 | not_started | 自动化门禁、跨平台构建、校园网人工验收 | [发布](../operations/release.md)、[证据](../evidence/README.md) |
| 1.x 后续功能迁移 | not_started | 会话 → 套餐/当前用量 → 历史用量/账单/充值 | [迁移矩阵](migration-matrix.md) |

## 当前实现快照

截至 2026-08-28，以下内容已进入本地工作树，但“实现存在”不代表对应发布门禁已完成：

- 文档/agent 分层、稳定 ID 索引与 `doccheck` 已建立；工作树中的真实测试 fixture 已移除并由合成 fixture 替代，secret scan 配置及其合成 canary 已接入，自更新代码与可达入口已移除。
- 根 `ipgw` SDK façade、typed errors、接口枚举和 internal CAS/Srun/Dashboard 边界已实现；HTTPS-only、ticket HTTP 下一跳进程内截获、动态 CAS 表单/公钥、JSON/JSONP、最终身份核对、账号冲突、幂等 logout、挑战分类和脱敏 Observer 均已有合成或 `httptest` 覆盖。
- `ipgw`、`ipgw-meta`、`ipgw-legacy` 三入口、模式优先级、稳定 JSON envelope/退出码、named profiles、keyring/env/file/prompt provider、事务化双来源配置迁移及原子 bundle/安装脚本已实现。Config 读取、跨进程 mutation lock、pending-journal 写阻断、preview 目标代次核对和 keyring 提交失败清理均已加固并有确定性 race 覆盖。
- CI/release workflow、六目标交叉构建定义和 release gate 已写入仓库；它们尚未在冻结的远端候选提交上形成完整成功记录，因此不能把 workflow 文件本身当作 M3 证据。

M1 的本地实现与自动化门禁已完成。M2 仍保持 `in_progress`，直到安装/升级/回滚 smoke test 与 Unix 实机权限行为完成相应验证；不得把交叉编译或语法检查外推为实机安装证据。

## 2026-08-28 冻结前只读核验

`WP-M0-PREFLIGHT` 已完成且没有创建提交、修改 refs 或接触远端写操作：

- GitHub CLI 认证和只读 API 请求成功；远端 `main` 与 `v0.1.0`–`v0.1.3` 均与本地记录一致。
- `main`/`v0.1.3` 为 `3721fd60dbdc8e6a35cab9569ec295b6fcc7bdea`；当前 HEAD tree 为 `a61feedcc1c5dd7f11b933e0d6235d5fe8565728`。
- 工作树共有 146 项变化（33 tracked、113 untracked），index 为空；存在两个指向 tree object 的 `refs/codex/**`，它们不得进入 mirror 推送范围。
- `core.autocrlf=true` 且尚无 `.gitattributes`，存在 LF/CRLF 机械污染风险；`WP-M0-FREEZE-AUDIT` 必须先建立明确 EOL 策略。
- 固定 `git-filter-repo 2.47.0` 脚本 SHA-256 为 `67447413E273FC76809289111748870B6F6072F08B17EFE94863A92D810B7D94`；gitleaks v8.30.1 可用且合成 canary 通过。
- 当前源码快照 secret scan 为 0；全历史有且仅有 4 条已知命中，分布在旧测试 fixture，尚未重写清除。
- `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...`、`go run ./cmd/doccheck --check` 均通过。核验机 Go 为 1.26.1；v1 构建基线仍以 `go.mod` 的 Go 1.25.0 和 workflow 的 `go-version-file` 为准。
- 维护者已于 2026-08-28 通过官方门户确认泄露会话失效；未保存截图或认证材料。

## 2026-08-27 本地验收结果

- `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 与 `go run ./cmd/doccheck --check` 均通过。
- `ipgw`、`ipgw-meta`、`ipgw-legacy` 在 Windows/Linux/macOS 的 amd64/arm64 共 18 个二进制构建通过；`internal/config` 测试在同六目标编译通过，Windows/amd64 race 实际执行通过。
- `install.ps1` PowerShell AST、`install.sh`/`scripts/release.sh`/`scripts/test-gitleaks.sh` Git Bash 语法以及 `make -n ci` 均通过。它们不是六平台实际安装、升级、回滚证据。
- gitleaks 正/负合成 canary 与排除 `.git`、`build`、module cache 后的当前源码快照扫描通过。原始 Git 历史仍按 `SEC-HISTORY-001` 保留 4 条已脱敏的已知命中，故 M0 仍未完成。

## 尚未完成与外部条件

- `SEC-HISTORY-001` 未完成：会话已确认失效，但冻结提交、受限历史备份、全 refs 历史重写、v0.1.1–v0.1.3 tag 复核、全历史复扫、逐 ref lease 的远端强制更新、GitHub 缓存/PR 引用处理和协作者重新克隆均未完成。只读 GitHub 认证成功不构成任何远端写入批准。
- 尚未完成 Windows/Linux/macOS 的实际安装、升级、回滚 smoke test；Linux/macOS 的权限与锁行为目前只有交叉编译及 Unix 实现测试源码，仍需相应平台实际执行。
- 尚未实现并冻结符合 [ADR-0007](../architecture/decisions/ADR-0007-immutable-candidate-promotion.md) 的 candidate-set，也未完成 attestation/promotion workflow。因此 M3 保持 `not_started`，M0 未完成前继续禁止新 release。
- `REL-LIVE-MATRIX-001` 未执行：校园有线/无线的 password 场景和至少一种网络的 Terminal QR 扫码闭环都需要在 [ADR-0009](../architecture/decisions/ADR-0009-separated-live-test-plane.md) 的隔离边界内完成，并按证据规范脱敏落盘。
- 离线安装器尚未满足 [ADR-0008](../architecture/decisions/ADR-0008-offline-transactional-installer.md) 的 acquisition、路径/权限和完整 failpoint 门禁。

## 当前已知能力限制

- v1 在具备可靠 network fingerprint 前禁用持久协议缓存 fallback；绑定 IP 不作为网络身份，每次操作执行动态发现。`WithProtocolStateStore` 仅为未来扩展缝，详见 [`SDK-CACHE-001`](../reference/go-sdk.md#sdk-cache-001协议状态存储)。
- `AUTH-SMS-001` 手机验证码仅为 `observed_anonymous + detected_only`，目前没有可触发的真实测试条件，不阻塞 v1。
- `AUTH-QR-001` Terminal QR 在真实校园网验证前保持 `live_unverified`。
- `AUTH-PASSWORD-001` 在当前候选完成有线、无线真实验收前保持 `live_unverified`。
- 当前可用性仅有 2026-08-27 的人工使用报告，不构成发布门禁证据。
- M0 完成前禁止发布新版本。

## 更新规则

完成状态必须同时附带可复现的自动化结果或符合 `EVID-REDACT-001` 的真实网络证据。不得因“暂时没有复现”关闭安全或协议问题。
