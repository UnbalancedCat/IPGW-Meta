---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
derived: true
snapshot_at: 2026-08-28
---

# 当前执行交接

正式里程碑状态以 [`docs/upgrade/status.md`](../docs/upgrade/status.md) 为准，行为规范以 [`docs/`](../docs/README.md) 为准。本文件只记录当前执行者、工作包、阻塞和下一步。

## 当前执行

- 执行者：Codex（macOS trusted-system-alias config blocker）。
- 工作包：`WP-M0-RENAME-GOVERN`（`in_progress`）。
- 修改边界：维护者已授权在 PR #1 上插入独立、docs-first 的 macOS trusted-system-alias config blocker；本窗口只可更新对应 `docs/` 安全语义与 ADR、`internal/config` 句柄化路径遍历及回归测试，创建 SSH-signed commit、普通 fast-forward push、复核 CI，并在六项 checks 全绿后配置已批准的 main/tag ruleset。禁止用 workflow/`TMPDIR` 绕过，禁止 merge、force-push、release/tag/资产和校园网会话操作。
- 停止条件：任意 local/remote ref 或 PR SHA 漂移、规范与实现无法保持“仅信任固定 macOS 顶层系统 alias，继续拒绝用户可控父 symlink 与最终 symlink”、签名或测试失败、六项 CI 未全绿、未知 ruleset 或权限不足均立即停止；不得扩大到其他配置能力或发布动作。

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
- PR #1 的首次 CI 在 macOS 上确定失败：`t.TempDir()` 位于 `/var/folders/...`，而现有 Unix credential 路径遍历拒绝作为 symlink 的 `/var` 父组件，导致 migration journal/snapshot 与 credential tests 报 `credential path contains a symbolic link`；合法私有文件使用同类系统路径时也会被产品拒绝。维护者已授权独立、docs-first 修复该 blocker；不得用测试路径或 CI `TMPDIR` 掩盖产品缺陷。
- 冻结提交 `38fadd1bef3692f52a8c9a1b67db45819b57112c` 本身未签名，且是 `main..codex/v1-freeze` 的唯一 commit；在合入前直接启用 `required_signatures` 会阻断普通 merge，治理 bootstrap 顺序必须显式处理，不能静默改用 squash 或 rebase 改写拓扑。

## 下一步

- 先提交独立的 docs/ADR 决策，再以新的 signed commit 完成 `internal/config` 句柄化 no-follow 修复与回归测试；全套本地验收通过后普通 push。
- 等待 PR #1 最新 head SHA 上 `Documentation, vet, and secrets`、三个 `Tests (...)`、`Race detector` 和 `Cross-build six supported targets` 全部成功。
- 六项实证全绿后，先启用要求 PR、严格六检查、禁止删除/force-push 的 main ruleset；完成获批的合入策略后立即加入 `required_signatures`，并创建限制 `v*` 更新/删除的 tag ruleset。全部复核通过后才完成 `WP-M0-RENAME-GOVERN`，随后进入 `WP-BASELINE-VERIFY`。
