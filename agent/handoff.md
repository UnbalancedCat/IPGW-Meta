---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
derived: true
snapshot_at: 2026-08-28
---

# 当前执行交接

正式里程碑状态以 [`docs/upgrade/status.md`](../docs/upgrade/status.md) 为准，行为规范以 [`docs/`](../docs/README.md) 为准。本文件只记录当前执行者、工作包、阻塞和下一步。

## 当前执行

- 执行者：无（PR #1 bootstrap merge / `required_signatures` 安全停点）。
- 工作包：`WP-M0-RENAME-GOVERN`（`in_progress`）。
- 修改边界：本窗口已完成独立、docs-first 的 macOS trusted-system-alias blocker、SSH-signed commits、普通 fast-forward push、实现 head 六项 CI 复核，以及获批的 main/tag ruleset 创建；未 merge PR，未 force-push、创建 release/tag/资产、修改校园网会话或处理隔离工作区/备份。
- 停止条件：冻结提交 `38fadd1bef3692f52a8c9a1b67db45819b57112c` 未签名；在维护者明确批准 bootstrap 合入策略前，不得 merge、改写分支拓扑或提前启用会阻断当前 PR 的 `required_signatures`。任意 refs/ruleset 漂移、签名或 CI 失败同样停止。

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
- `WP-M0-RENAME-GOVERN`（当前停点）：仓库已小写改名并完成 canonical `origin` 与独立 fresh-clone 复核；docs-first [`ADR-0010`](../docs/architecture/decisions/ADR-0010-macos-trusted-system-path-alias.md) 和句柄化 no-follow 修复已由 signed commits `f1ca77c1096a84d5048af72395fd4449a34a9ffc`、`907b3e754b67cf759421eb4326c44367b14fe78a` 落盘，GitHub 均验证为 `valid`。实现 head 的 CI run `33173324862` 六项成功且来源均为 Actions App `15368`。
- active main ruleset `main-v1-protection`（`21733128`）要求 PR、严格六检查并禁止删除/force-push；active tag ruleset `v-tag-protection`（`21733211`）禁止 `v*` 更新/删除。两者均无 bypass，创建后分支与四个既有 tag SHA 未漂移。
- Signing key 登记后，`15fb31d`、`c562483`、`5bee31b`、`f1ca77c` 与 `907b3e7` 当前均由 GitHub 验证为 `verified=true, reason=valid`；不得为追溯修正签名状态而改写历史。

## 阻塞

- GitHub 对象 API 仍可访问 3 个已失去 ref 可达性的旧 commit；维护者需按 [`SEC-HISTORY-001`](../docs/architecture/security.md#sec-history-001历史泄露响应) 向 GitHub Support 请求清理缓存/悬空对象。
- PR #1 仍为 open、未 merge；macOS blocker 已解决。任何后续状态/交接提交在普通 push 后都必须以自身 head 完成六项 CI，不能沿用实现 head 的成功状态冒充最新门禁。
- 冻结提交 `38fadd1bef3692f52a8c9a1b67db45819b57112c` 本身未签名，且是 `origin/main..codex/v1-freeze` 范围内唯一未签名 commit；在合入前直接启用 `required_signatures` 会阻断普通 merge，治理 bootstrap 顺序必须显式处理，不能静默改用 squash 或 rebase 改写拓扑。

## 下一步

- 维护者明确选择并批准 PR #1 的 bootstrap 合入策略；不得默认为 squash/rebase，不得使用普通或 lease force-push。
- 获批合入完成后立即把 `required_signatures` 加入 main ruleset，复核 main、rulesets、PR/merge SHA 和 tag refs，再完成 `WP-M0-RENAME-GOVERN`。
- 维护者另行联系 GitHub Support 清理三个仍可 API 访问的旧 commit；该事项完成前 [`SEC-HISTORY-001`](../docs/architecture/security.md#sec-history-001历史泄露响应) 与 M0 保持 `in_progress`。治理工作包完成后下一工作包为 `WP-BASELINE-VERIFY`。
