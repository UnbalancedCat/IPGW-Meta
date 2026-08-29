---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
derived: true
snapshot_at: 2026-08-29
---

# 当前执行交接

正式里程碑状态以 [`docs/upgrade/status.md`](../docs/upgrade/status.md) 为准，行为规范以 [`docs/`](../docs/README.md) 为准。本文件只记录当前执行者、工作包、阻塞和下一步。

## 当前执行

- 执行者：Codex。
- 工作包：`WP-M3-LIVEGATE-SCHEMA`（`in_progress`）。
- 修改边界：只实现 [`REL-LIVEGATE-001`](../docs/operations/live-validation.md#rel-livegate-001runner-接口与信任边界) 与 [`EVID-AUTH-001`](../docs/evidence/README.md#evid-auth-001认证证据字段) 固定的 live-gate schema、封闭枚举、验证和泄漏测试，范围限于 `docs/operations/live-validation.md`、`docs/evidence/README.md`、`internal/livegate/**`、对应测试、本文件及正式状态的最小事实更新；不得实现 maintainer-only runner、执行候选、修改产品 SDK/CLI、workflow、安装器或 ruleset。
- 停止条件：任一远端 ref/SHA、ruleset、签名或工作树漂移；规范无法唯一确定 schema/枚举而需要扩大产品契约；需要启动网络/认证/QR/校园网会话、接触凭据或证据原始输出；需要创建 candidate、release、tag、发布资产、force-push，或接触隔离工作区、敏感备份、rewrite mirror 时立即停止。

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
- Signing key 登记后，`15fb31d`、`c562483`、`5bee31b`、`f1ca77c` 与 `907b3e7` 当前均由 GitHub 验证为 `verified=true, reason=valid`；不得为追溯修正签名状态而改写历史。

## 阻塞

- GitHub 对象 API 仍可访问 3 个已失去 ref 可达性的旧 commit；维护者需按 [`SEC-HISTORY-001`](../docs/architecture/security.md#sec-history-001历史泄露响应) 向 GitHub Support 请求清理缓存/悬空对象。
- 三个旧对象按维护者决定暂时搁置；该外部事项不阻塞本轮仓库治理与 baseline，但完成前 [`SEC-HISTORY-001`](../docs/architecture/security.md#sec-history-001历史泄露响应) 和 M0 必须继续保持 `in_progress`。
- 既有 `.github/workflows/release.yml` 在 push 上仍出现无 job 的即时失败；它不是当前 main required check，也非本工作包引入，但进入 WP-M3-CANDIDATE / promotion 前必须按发布规范诊断。

## 下一步

- 完成 `WP-M3-LIVEGATE-SCHEMA` 的 schema、封闭枚举、退出码与泄漏测试后，再进入 `WP-M3-LIVEGATE-RUNNER`；既有 `.github/workflows/release.yml` 无 job 即时失败仍须在进入 WP-M3-CANDIDATE / promotion 前按发布规范诊断。
- 三个旧对象继续作为外部 GitHub Support 待办搁置；完成前 [`SEC-HISTORY-001`](../docs/architecture/security.md#sec-history-001历史泄露响应) 与 M0 保持 `in_progress`，且不创建新 release。
