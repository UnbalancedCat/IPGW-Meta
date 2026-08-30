---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
derived: true
snapshot_at: 2026-08-30
---

# 当前执行交接

正式里程碑状态以 [`docs/upgrade/status.md`](../docs/upgrade/status.md) 为准，行为规范以 [`docs/`](../docs/README.md) 为准。本文件只记录当前执行者、工作包、阻塞和下一步。

## 当前执行

- 执行者：Codex。
- 工作包：`WP-M3-PROMOTION`（implementation `complete`；signed head `466276fd28a855370a10ca8421117811b4e4ef13` 已由 PR #14 普通合入 main `e542f108a32e37f8313c9673cbd68254af25968c`；当前仅提交收尾事实记录，M0 gate 继续阻断正式 Candidate 与发布）。
- 修改边界：本收尾分支只允许更新 `docs/upgrade/status.md` 与 `agent/handoff.md`，记录 Promotion 契约/实现、本地门禁、签名提交、PR/merge 与 merge 后 CI/Native 事实；禁止修改 workflow、产品/测试代码、公共契约、candidate/live-gate/安装器或 ruleset，禁止 dispatch Candidate/Promotion，禁止创建或使用正式 artifact、attestation、tag、draft、release 或发布资产，禁止启动真实网络/认证/QR/校园网会话。
- 停止条件：任一 ref/SHA、ruleset、签名、required check 或工作树漂移；收尾事实无法由已完成日志复现；需要修改两文件以外内容、dispatch workflow、创建正式 Candidate/evidence/tag/draft/public release、改变 main/tag 保护、启动真实会话、接触凭据/原始输出、force-push，或接触旧工作区、敏感备份、rewrite mirror 时立即停止。

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
- `WP-M0-RENAME-GOVERN`：仓库已小写改名并完成 canonical `origin`、独立 fresh-clone、PR #1 普通 merge 与 main/tag ruleset 核验。merge commit `f927d7316885a26c8289ba77bc04ed27e379d3c8` 双亲和 tree 精确匹配、GitHub 签名状态为 `valid`；main ruleset `21733128` 已要求 PR、签名提交、严格六检查并禁止删除/force-push，tag ruleset `21733211` 禁止 `v*` 更新/删除，两者均无 bypass。
- `WP-BASELINE-VERIFY`：在精确 `main` `f927d7316885a26c8289ba77bc04ed27e379d3c8` 上完成 test/race/vet/doccheck、六目标三入口 cross-build、`internal/config` 六目标编译、合成 gitleaks canary、14 commits 全历史/refs/reflog 与工作树扫描、`fsck`；全部通过且临时目录已清理。
- `WP-M2-CONFIG-CLOSE`：signed commit `598850195a65167d121c2fc86477cf56676bb8df` 补齐五个 journal phase 的逐事务失败注入、两类来源 backup 原字节/固定路径/跨平台权限验证，以及 keyring 写后报错与补偿失败恢复门禁；PR #3 首轮 CI run `33224027909` 六项 required checks 全部成功，本地全局门禁、固定 gitleaks `8.30.1` 扫描与 `fsck` 通过，临时目标已清理。
- `WP-M2-INSTALL-UNIX`：固定离线 bundle/SHA 接口、私有副本外层哈希、七成员类型/大小/压缩比与 canonical manifest 共用验证链、路径/权限约束、受限 journal、active 分离和逆序回滚均已实现；9 个前向与 3 个回滚 failpoint、fresh/upgrade/三入口、launcher、路径攻击与权限测试在 Ubuntu 和 macOS 原生 CI 通过。PR #4 implementation head `5dfe60ea152e4fd23677fe8cc18f4e2b59e151f5` 的 CI run `33227444605` 六项 required checks 全部成功；这不代表 `WP-M2-INSTALL-NATIVE` 的六平台 release-asset 矩阵完成。
- `WP-M2-INSTALL-WINDOWS`：固定离线 bundle/SHA 接口、已打开来源到私有副本的外层哈希、Users/Authenticated Users/Everyone 写 ACL 拒绝、固定磁盘/同卷/逐祖先 reparse 校验、七成员 bounded extraction 与 canonical manifest 共用验证链均已实现；受保护 ACL、`active` junction、三个 hard-link 入口、受限原子 journal、PATH 测试隔离和逆序回滚覆盖 9 个前向与 3 个回滚 failpoint。首轮 CI run `33237097662` 暴露 pwsh 7 → Windows PowerShell 5.1 的模块路径继承差异；signed fix `b5a4d5c4c6e4f4e6fb48d3361fdb94a7b26905c0` 后 run `33237394630` 六项 required checks 全部成功。这不代表 `WP-M2-INSTALL-NATIVE` 的六平台 release-asset 矩阵完成。
- `WP-M2-INSTALL-NATIVE`：六个官方原生 runner 均消费单一打包 job 产生的 release-shaped artifact；验收 artifact ID 为 `9713909266`、digest 为 `sha256:51341d93b6eb09e758e2a62450312abca18400294d385d8a6e986a39dead9d5d`，source 为 `05035df77c4e75586e9cd5b03d569cc17a0a5e78`、tree 为 `5eb94413504bda1d8231ec99a1081aaf7435666f`。首轮实现的 Windows 测试严格环境 allowlist 遗漏 GitHub runner 预热的 `PSModuleAnalysisCachePath`，导致每个短生命周期 Windows PowerShell 子进程重复承担模块分析冷启动并使测试退化至超过 10 分钟；signed fix `d54a30d085d9663c03970cd66b76f5df13216b0b` 仅恢复该缓存路径。native run `33249498529` 七个 jobs 全部成功，CI run `33249498526` 六项 required checks 全部成功；signed commits `d20d01228e8305314025c0d057dc8c98db90fb22` 与 `d54a30d085d9663c03970cd66b76f5df13216b0b` 均由 GitHub 验证为 `verified=true, reason=valid`。本工作包已完成，并已通过 PR #6 以普通 merge 合入 merge commit `989cfad32aaed7352c50fb9e80233ac137362616`；其双亲、tree 与 GitHub 签名均已精确复验，merge 后 CI run `33254131004` 六项检查和 native run `33254130929` 七个 jobs 全部成功。
- `WP-M3-LIVEGATE-SCHEMA`：docs-first schema version 1 已固定严格 18/5 JSON、封闭枚举与环境矩阵、ID/hash/time、capability transition、primary prefix/cleanup/result 和产品/runner 退出码映射，并实现 direct JSON guards 与泄漏测试；focused coverage 为 94.3%。signed implementation commit `03ffa893dce68bf83886ca047b2c9c5760a351b9` 由 GitHub 验证为 `valid`；PR #8 已以普通 merge 合入 `01e4dc59bd7787cb382e9d2392f7e6c3052a569b`，双亲为 `0aaff5da9ac691bcb56538074a0b3c178b140808` 与 `03ffa893dce68bf83886ca047b2c9c5760a351b9`，tree 为 `6fa99e2f23efa0ef2ac6d1b269b52bcda0514148`，GitHub 签名为 `valid`。merge 后 CI run `33259519538` 六项和 native run `33259519515` 七项全部成功；本地 test/race/vet/doccheck、固定 gitleaks `8.30.1` 合成 canary/28 commits 全历史/全部 refs/reflog/工作树零命中与 `git fsck --full` 均通过，fsck 仅报告四个已知 dangling tree。未实现 runner、candidate、release/tag，也未启动校园网或其他网络会话。
- `WP-M3-LIVEGATE-RUNNER`：maintainer-only runner、冻结 candidate/manifest/hash 绑定、固定 suite 状态机与清理权、私有三文件 evidence bundle、durability 与泄漏门禁已实现。signed commits `e5528f46792f7d9d3d087b2b59196106d6856976` 与 `80462712f872519da73526927476fdad69edee32` 均由 GitHub 验证为 `valid`；PR #10 head CI run `33271001499` 六项和 native run `33271001474` 七项全部成功，并以普通 merge 合入 `6196234374f72089affd5442d0b5c2c0193cf62d`，双亲、tree `b9383072cd8c32bf1f0aafd6596b4a6c0ae86077` 与 GitHub 签名均已精确复验。merge 后 CI run `33271253178` 六项和 native run `33271253166` 七项全部成功；本地 test/race/vet/doccheck、跨平台编译、固定 gitleaks canary/全历史/工作树扫描和 `fsck` 通过。未创建 candidate/release/tag，也未启动校园网或其他真实会话。
- `WP-M3-CANDIDATE`：旧 release workflow 零-job 根因已由 actionlint v1.7.12 精确复现并关闭；full/release manifest、build-input、确定性六平台归档、公开资产/私有 helper 分区、同一 artifact 原生安装及 11-subject attestation 路径已实现。signed commits `74cb8f6e8dab02b2d1d935640acdb885ea19ff77` 与 `73f5aa30a29aebb60970319ea378bb20b62bdf06` 均由 GitHub 验证为 `valid`；PR #12 head CI `33282461292` 六项与 Native `33282461260` 七项成功，并以普通 merge 合入 `382a8f994761a4a5d73f578d08f263718eac958c`，双亲、tree `0206c0a06d3c8c46fc1225c5440d8a61b7ad2554` 与签名已精确复验。merge 后 CI `33282708893` 六项与 Native `33282708924` 七项再次成功；本地 test/race/vet/doccheck、actionlint、跨平台编译、固定 gitleaks canary/全历史/205 文件工作树扫描和 `fsck` 通过。M0 gate 按预期阻断正式 dispatch，未创建 candidate artifact/attestation/release/tag，也未启动真实会话。
- `WP-M3-PROMOTION`：closed-world lock/public summary/canonical notes 与 no-build 原样晋升契约、Promotion workflow、stdlib verifier、7 组合成测试和 workflowguard 已实现；本地 test/race/vet/doccheck/actionlint/六目标 cross-build、固定 gitleaks canary/50 commits 全历史/全部 refs/reflog/209 文件工作树零 finding 与 `fsck` 均通过。signed commit `466276fd28a855370a10ca8421117811b4e4ef13` 由 GitHub 验证为 `valid`；PR #14 head CI `33290209456` 六项和 Native `33290209465` 七项成功，并以普通 merge 合入 `e542f108a32e37f8313c9673cbd68254af25968c`，双亲、tree `0d2d0dcb9439bccc67945d1b94d367374a90f776` 与签名已精确复验。merge 后 CI `33290459305` 六项与 Native `33290459311` 七项再次成功；M0 gate 按预期拒绝，未 dispatch workflow，未创建/使用正式 Candidate、artifact、attestation、tag、draft、release 或真实会话。
- Signing key 登记后，`15fb31d`、`c562483`、`5bee31b`、`f1ca77c` 与 `907b3e7` 当前均由 GitHub 验证为 `verified=true, reason=valid`；不得为追溯修正签名状态而改写历史。

## 阻塞

- GitHub 对象 API 仍可访问 3 个已失去 ref 可达性的旧 commit；维护者需按 [`SEC-HISTORY-001`](../docs/architecture/security.md#sec-history-001历史泄露响应) 向 GitHub Support 请求清理缓存/悬空对象。
- 三个旧对象按维护者决定暂时搁置；该外部事项不阻塞本轮仓库治理与 baseline，但完成前 [`SEC-HISTORY-001`](../docs/architecture/security.md#sec-history-001历史泄露响应) 和 M0 必须继续保持 `in_progress`。

## 下一步

- 只暂存 `docs/upgrade/status.md` 与 `agent/handoff.md`，创建并验证 SSH 签名收尾提交；正常 push 当前分支、创建 PR，等待严格六项 required checks 与被触发的 Native workflow 后普通合并。
- 收尾 merge 精确复验后，按 docs-first 开始 `LAB-DISCOVER`；该工作包只做匿名、只读、无凭据的实验室能力/拓扑发现，任何真实认证或需维护者私有交互的步骤前停止。
- 三个旧对象继续作为外部 GitHub Support 待办搁置；完成前 [`SEC-HISTORY-001`](../docs/architecture/security.md#sec-history-001历史泄露响应) 与 M0 保持 `in_progress`，且不创建新 release。
