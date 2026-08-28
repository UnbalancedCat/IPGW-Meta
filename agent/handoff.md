---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
derived: true
snapshot_at: 2026-08-28
---

# 当前执行交接

正式里程碑状态以 [`docs/upgrade/status.md`](../docs/upgrade/status.md) 为准，行为规范以 [`docs/`](../docs/README.md) 为准。本文件只记录当前执行者、工作包、阻塞和下一步。

## 当前执行

- 执行者：无（macOS CI 门禁安全停点）。
- 工作包：`WP-M0-RENAME-GOVERN`（`in_progress`）。
- 修改边界：本窗口已完成 GitHub 小写改名、canonical `origin`、独立 fresh-clone 复核、登记后的 SSH-signed follow-up commit `c562483cc21239246367d65a08687e20ea9c5356`、普通 fast-forward branch push、GitHub `verified=true, reason=valid` 门禁及 PR #1 bootstrap；未修改产品/测试代码，main/tag ruleset 均未创建。
- 停止条件：PR CI run `33166812911` 的六项精确 context 已实证为 GitHub Actions App `15368`，但 `Tests (macos-latest)` 失败，只有 5/6 成功；不得在 required checks 全绿前启用 ruleset，也不得在 `WP-M0-RENAME-GOVERN` 中擅自修改属于 `WP-M2-CONFIG-CLOSE` 的 `internal/config` 安全路径或迁移测试。

## 已完成

- `WP-M0-PREFLIGHT`：只读核对完成；结果已在当前任务中报告，未创建提交或修改 refs。
- `WP-R2-SPEC-A`：r2 完整计划、正式状态以及 [ADR-0007](../docs/architecture/decisions/ADR-0007-immutable-candidate-promotion.md)、[ADR-0008](../docs/architecture/decisions/ADR-0008-offline-transactional-installer.md)、[ADR-0009](../docs/architecture/decisions/ADR-0009-separated-live-test-plane.md) 已落盘；未修改产品代码、Git refs、远端或发布状态。
- `WP-R2-SPEC-B`：发布、离线安装、live validation、evidence、认证能力、校园实验室和无 GUI 专题规范已落盘；新增稳定 ID 均有唯一 docs 标题声明和有效交叉链接。
- `WP-R2-SPEC-C`：全仓 docs/agent revision、根导航、派生工作包索引、doccheck required paths/测试和生成的 stable-ID 索引已统一到 r2；文档门禁与全仓 Go test/vet 通过。
- `WP-M0-FREEZE-AUDIT`：153 项计划内变更已暂存；name/status、numstat、EOL、diff-check、staged secret scan、test/race/vet/doccheck 均通过，工作树无 unstaged 变更；未创建提交或修改 refs/远端。
- `WP-M0-FREEZE-COMMIT`：本地 `codex/v1-freeze`、单一冻结提交、安全 tree archive 和受限完整历史备份已创建并完成 tree/secret/ACL/refs/hash 验证；未重写历史或访问远端写接口。
- `WP-M0-REWRITE-LOCAL`：受限隔离 mirror 已完成 7 个 commits 的一一重写；全历史扫描由 4 条已知命中降至 0，fsck、tree、tag、父图和消息门禁通过。
- `WP-M0-REWRITE-REMOTE`：GitHub 上 6 条批准 refs 已通过 per-ref lease 一次 atomic push 更新并完成 ls-remote/branch API 复验；无 PR/codex 隐藏 refs。
- `WP-M0-VERIFY-REHOME`：`D:\project\Go\ipgw-meta-clean` 已从远端 fresh clone；旧重写 commit 对象不存在，secret scan/fsck/test/race/vet/doccheck 全部通过，该路径成为后续唯一权威工作区。

## 阻塞

- GitHub 对象 API 仍可访问 3 个已失去 ref 可达性的旧 commit；维护者需按 [`SEC-HISTORY-001`](../docs/architecture/security.md#sec-history-001历史泄露响应) 向 GitHub Support 请求清理缓存/悬空对象。
- 登记后创建的 `c562483cc21239246367d65a08687e20ea9c5356` 已由 GitHub 验证为 `verified=true, reason=valid`；既有 `15fb31d` 仍保持首次记录的 `unknown_key`，不得依赖追溯修正或 force-push 改写。
- PR #1 的首次 CI 在 macOS 上确定失败：`t.TempDir()` 位于 `/var/folders/...`，而现有 Unix credential 路径遍历拒绝作为 symlink 的 `/var` 父组件，导致 migration journal/snapshot 与 credential tests 报 `credential path contains a symbolic link`；合法私有文件使用同类系统路径时也会被产品拒绝。仅改测试路径或 CI `TMPDIR` 会掩盖真实产品缺陷；正确修复需先澄清 docs 安全语义，再调整 `internal/config`，这超出当前 work package 的 repository/ruleset 修改边界。
- 冻结提交 `38fadd1bef3692f52a8c9a1b67db45819b57112c` 本身未签名，且是 `main..codex/v1-freeze` 的唯一 commit；在合入前直接启用 `required_signatures` 会阻断普通 merge，治理 bootstrap 顺序必须显式处理，不能静默改用 squash 或 rebase 改写拓扑。

## 下一步

- 维护者决定是否授权在当前 PR 上插入一个独立、docs-first 的 macOS trusted-system-alias config blocker 修复，或调整工作包顺序转入 `WP-M2-CONFIG-CLOSE`；不得仅改 `TMPDIR`/测试路径，也不得用删除 macOS check、降低检查范围或强设 ruleset 绕过失败。
- 获得授权后以新的 signed commit 修复并普通 push，待 PR #1 最新 head SHA 上 `Documentation, vet, and secrets`、三个 `Tests (...)`、`Race detector` 和 `Cross-build six supported targets` 全部成功。
- 六项实证全绿后，先启用要求 PR、严格六检查、禁止删除/force-push 的 main ruleset；完成获批的合入策略后立即加入 `required_signatures`，并创建限制 `v*` 更新/删除的 tag ruleset。全部复核通过后才完成 `WP-M0-RENAME-GOVERN`，随后进入 `WP-BASELINE-VERIFY`。
