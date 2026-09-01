---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
status: approved
---

# 实施状态

本页是里程碑状态的正式来源；工作包级执行交接位于 [`agent/handoff.md`](../../agent/handoff.md)。状态仅使用 `not_started`、`in_progress`、`blocked`、`complete`。

| 里程碑 | 状态 | 发布门禁 | 规范入口 |
|---|---|---|---|
| M0 文档与紧急安全 | in_progress | 残余历史对象治理（非发布状态门禁） | [计划](plan.md#34-m0-历史清理)、[安全](../architecture/security.md)、[ADR-0011](../architecture/decisions/ADR-0011-nonblocking-m0-governance.md) |
| M1 协议正确性与 SDK | complete | HTTPS-only、ticket 截获、动态发现、typed errors、最终身份校验 | [协议](../architecture/protocol-correctness.md)、[SDK](../reference/go-sdk.md) |
| M2 三入口、配置与自动化 | complete | 三二进制、JSON/退出码、profiles、迁移和原子安装 | [CLI](../reference/cli.md)、[迁移](../operations/config-migration.md) |
| M3 v1 候选与稳定发布 | in_progress | 自动化门禁、跨平台构建、校园网人工验收 | [发布](../operations/release.md)、[证据](../evidence/README.md) |
| 1.x 后续功能迁移 | not_started | 会话 → 套餐/当前用量 → 历史用量/账单/充值 | [迁移矩阵](migration-matrix.md) |

## 当前实现快照

截至 2026-08-28，以下内容已进入重写后的 `codex/v1-freeze` 和已验证的 fresh clone，但“实现存在”不代表对应发布门禁已完成：

- 文档/agent 分层、稳定 ID 索引与 `doccheck` 已建立；工作树中的真实测试 fixture 已移除并由合成 fixture 替代，secret scan 配置及其合成 canary 已接入，自更新代码与可达入口已移除。
- 根 `ipgw` SDK façade、typed errors、接口枚举和 internal CAS/Srun/Dashboard 边界已实现；HTTPS-only、ticket HTTP 下一跳进程内截获、动态 CAS 表单/公钥、JSON/JSONP、最终身份核对、账号冲突、幂等 logout、挑战分类和脱敏 Observer 均已有合成或 `httptest` 覆盖。
- `ipgw`、`ipgw-meta`、`ipgw-legacy` 三入口、模式优先级、稳定 JSON envelope/退出码、named profiles、keyring/env/file/prompt provider、事务化双来源配置迁移及原子 bundle/安装脚本已实现。Config 读取、跨进程 mutation lock、pending-journal 写阻断、preview 目标代次核对和 keyring 提交失败清理均已加固并有确定性 race 覆盖。
- CI/release workflow、六目标交叉构建定义和 release gate 已写入仓库；实现与治理已通过 PR #1 以普通 merge 合入 `main` 的 `f927d7316885a26c8289ba77bc04ed27e379d3c8`（tree `4c5f253efebd5cea7bc85b0b5de5c2af84ed54f3`），但尚未形成 candidate-set、安装或真实校园网证据，因此不能外推为 M3 完成。

M1 的本地实现与自动化门禁已完成。M2 配置迁移、三入口、离线事务安装器与六平台原生 release-shaped asset 门禁均已完成；这些临时 PR artifact 不是 M3 candidate-set 或公开 release，不得外推为 M3 完成。

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

## 2026-08-28 历史重写与 rehome 核验

- 冻结提交 `ba7897d142a5a3d5a8a7c6219a6b735823d14bb8` 已映射为 `38fadd1bef3692f52a8c9a1b67db45819b57112c`，冻结 tree 保持 `9b3765dba53fe8e98d4c3f9b7c5d78c6edb26b91`。
- 隔离 mirror 的全历史扫描由 4 条已知命中降至 0；7 个 commit 的父图、元数据与消息保持一致，`fsck`、不可达对象、reflog、tag 和 tree 门禁通过。
- GitHub 上 `main`、`codex/v1-freeze` 与 `v0.1.0`–`v0.1.3` 已通过 6 条 per-ref lease 的单次 atomic push 更新并完成 refs/branch API 复核；无 PR 或 `refs/codex/**` 隐藏引用。
- `D:\project\Go\ipgw-meta-clean` 已从重写后远端 fresh clone；4 个重写前 commit 对象均不存在，全历史 secret scan、`fsck`、test/race/vet/doccheck 全部通过，成为后续唯一权威工作区。
- GitHub 对象 API 仍可直接访问 3 个已失去 ref 可达性的旧 commit，因此仍需维护者向 GitHub Support 请求清理缓存/悬空对象；该外部步骤完成前 `SEC-HISTORY-001` 和 M0 不得标记 complete。

## 2026-08-28 仓库小写改名、治理与 baseline 核验

- GitHub repository ID `1186323753`（node ID `R_kgDORrXdKQ`）已从 `UnbalancedCat/IPGW-Meta` 小写改名为 `UnbalancedCat/ipgw-meta`；default branch、两个 branch refs、`v0.1.0`–`v0.1.3` 四个 lightweight commit tags 及全部 SHA 均未漂移。当前权威工作区的 `origin` fetch/push URL 已更新为小写 canonical URL。
- 冻结 tree 原已统一使用 `github.com/UnbalancedCat/ipgw-meta`：Go module/import、源码 URL、文档、workflow、安装器和 release script 中没有旧 GitHub 仓库路径残留，因此本窗口没有对品牌名、旧工作区隔离路径、备份路径或 Windows 安装目录做机械大小写替换。
- 改名后已 fresh clone 到独立新目录 `D:\project\Go\ipgw-meta-govern-verify-20260828-r2`。该 clone 的 `main` 为 `5ad8c1fa05102fbcb249e7195f4801563b3d44a5`，验证分支为 `codex/v1-freeze` / `38fadd1bef3692f52a8c9a1b67db45819b57112c`，冻结 tree 为 `9b3765dba53fe8e98d4c3f9b7c5d78c6edb26b91`；refs、tag 类型、`fsck`、test/race/vet/doccheck、合成 gitleaks canary、全历史/reflog 与安全 tree scan 均通过，最终状态 clean。
- 合入后的精确 `main` `f927d7316885a26c8289ba77bc04ed27e379d3c8` / tree `4c5f253efebd5cea7bc85b0b5de5c2af84ed54f3` 已完成 `WP-BASELINE-VERIFY`：`go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...`、`go run ./cmd/doccheck --check` 全部通过；Windows/Linux/macOS 的 amd64/arm64 共 18 个三入口二进制交叉构建及 `internal/config` 六目标编译通过。
- 固定 gitleaks `8.30.1` 的上游 release assets 已同时按固定 SHA-256 与官方 checksums 核验；合成正负 canary、14 commits 的全历史/全部 refs/reflog 扫描及最终工作树扫描均为 0 命中。`git fsck --full` 通过，仅报告两个已知 dangling tree；全部本轮临时工具、cache 与构建目录已精确清理，工作树最终 clean。
- 首次 CI run `33166812911` 的 macOS 失败已按 docs-first 顺序修复：[`ADR-0010`](../architecture/decisions/ADR-0010-macos-trusted-system-path-alias.md) 由 signed commit `f1ca77c1096a84d5048af72395fd4449a34a9ffc` 建立；signed commit `907b3e754b67cf759421eb4326c44367b14fe78a` 使用逐组件 `openat(O_NOFOLLOW)`、只允许 Darwin 固定 `/var` → `/private/var` 受验证锚点，并增加原生 `/var/folders/...`、symlink 与 FIFO 回归测试。维护者登记 Signing key 后，post-clean signed commits `15fb31db059b660e03cf5460483cbf2f0aa0cbda`、`c562483cc21239246367d65a08687e20ea9c5356`、`5bee31b3ea07bb023c3b38b9465f7d12ed0caabb`、`f1ca77c1096a84d5048af72395fd4449a34a9ffc` 与 `907b3e754b67cf759421eb4326c44367b14fe78a` 当前均由 GitHub 验证为 `verified=true, reason=valid`。
- 最终 PR head `219da2f87112d54c8ad0f30ad13e45c39a0f8076` 的 CI run `33176975227` 六项精确 context 全部成功，且逐项复核均来自 GitHub Actions App `15368`；该提交由 GitHub 验证为 `verified=true, reason=valid`。PR #1 已以保留拓扑的普通 merge 合入，merge commit `f927d7316885a26c8289ba77bc04ed27e379d3c8` 的双亲精确为旧 `main` 与该 PR head，并由 GitHub 验证为 `valid`。
- active main ruleset `main-v1-protection`（ID `21733128`）无 bypass，现要求 PR、签名提交、严格六检查并禁止删除和 force-push；active tag ruleset `v-tag-protection`（ID `21733211`）无 bypass，禁止 `refs/tags/v*` 更新和删除。规则读回后 `main` 与四个既有 tag SHA 均未漂移。
- `WP-M0-RENAME-GOVERN` 与 `WP-BASELINE-VERIFY` 已完成。未对冻结提交或既有分支拓扑做追溯改写，也未使用 squash/rebase、force-push、release 或 tag 写操作。

## 2026-08-29 配置迁移事务收敛核验

- `WP-M2-CONFIG-CLOSE` 的 signed implementation commit `598850195a65167d121c2fc86477cf56676bb8df` 已补齐逐事务、无包级全局状态的 journal phase 失败注入，覆盖 `prepared`、`backups`、`keyring`、`config` 与 `marker_verified` 的自动回滚或已提交清理语义。
- 两类合成旧来源现同时验证 backup 固定写入 `BaseDir/migration-backups/`、名称精确为 `<source-kind>-<transaction-id>-<ordinal>.backup` 且完整保留原字节；Windows 原生 runner 验证受保护当前用户 DACL，Ubuntu 与 macOS 原生 runner 验证目录 `0700`、文件 `0600`。
- 合成 keyring backend 现覆盖“写入后返回错误”的补偿删除，以及补偿删除失败时 journal/backup 保留、错误脱敏和后续恢复重试；未调用真实系统 keyring 或接触真实凭据。
- PR #3 首轮 CI run `33224027909` 的六项 required checks 全部成功；本地 `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...`、`go run ./cmd/doccheck --check`、`internal/config` 六目标测试编译、固定 gitleaks `8.30.1` 合成 canary/15 commits 历史/精确 source tree 扫描及 `git fsck --full` 均通过，临时工具、cache、构建与扫描目录已清理。
- `WP-M2-CONFIG-CLOSE` 完成；M2 仍因安装器与原生安装矩阵未完成而保持 `in_progress`，后续工作包从 `WP-M2-INSTALL-UNIX` 继续。

## 2026-08-29 Unix 离线安装器收敛核验

- `WP-M2-INSTALL-UNIX` 已实现规范固定的 `--bundle`/`--bundle-sha256` 成对接口；离线来源必须为绝对、本地、非 symlink、`1..100 MiB` 且不可 group/world writable 的普通文件。安装器只从已打开的来源句柄复制到本轮私有 acquisition 目录，再对私有副本核对调用方 SHA-256；离线路径不初始化或调用下载器。
- 在线与离线 acquisition 后共用外层哈希、精确七成员名称/类型/大小、总量和压缩比限制、私有 bounded extraction、内部 `SHA256SUMS`、canonical manifest 与事务激活链。install root、bin dir 与 launcher dir 均执行绝对非根、不重叠和逐祖先无 symlink 校验；新建正式目录、version/binary/metadata 与私有 stage/backup/journal 使用规范模式。
- 激活事务现按已验证 version 发布、旧 active 分离、新 active 切换、三入口、launcher、PATH phase 和 commit 顺序写入受限 journal；失败时逆序恢复，无法安全恢复或命中 rollback failpoint 时保留 recovery materials 并 fail closed。测试控制仍只接受规范列出的四个变量，且仅在离线、当前用户私有测试根、精确 token 和全部输入/目标严格位于测试根时生效。
- Unix 测试覆盖 fresh install、upgrade、三入口 `--version`、launcher 默认/保持、公开与私有权限、离线无网络、输入/路径/归档拒绝、9 个固定前向 failpoint 及 3 个固定 rollback failpoint。WSL Ubuntu 本地完整矩阵通过；PR #4 implementation head `5dfe60ea152e4fd23677fe8cc18f4e2b59e151f5` 的 CI run `33227444605` 在 Ubuntu 与 macOS 原生执行上述测试，并与 Windows 全仓测试、race、vet/doccheck/secrets 和六目标 cross-build 一并六项成功。
- 本地 `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...`、`go run ./cmd/doccheck --check`、Git Bash 语法、Linux/macOS 两架构测试编译、固定 gitleaks `8.30.1` canary/全历史/reflog/工作树扫描及 `git fsck --full` 均通过；`fsck` 仅报告三个既有 dangling tree。
- `WP-M2-INSTALL-UNIX` 完成；合成 bundle 的原生 runner 测试不等于公开 release asset 的六平台 smoke，因此 M2 继续保持 `in_progress`，下一工作包为 `WP-M2-INSTALL-WINDOWS`，之后仍需 `WP-M2-INSTALL-NATIVE`。

## 2026-08-29 Windows 离线安装器收敛核验

- `WP-M2-INSTALL-WINDOWS` 已实现规范固定的 `-BundlePath`/`-BundleSha256` 成对接口；离线来源必须为固定本地磁盘上的绝对、非 reparse、`1..100 MiB` 普通文件，并拒绝 Users、Authenticated Users 或 Everyone 可写 ACL。安装器以不共享写入/删除的已打开句柄复制到本轮私有 acquisition 目录，再仅对私有副本核对调用方 SHA-256；离线路径不初始化下载 URL、TLS 或 HTTP client。
- Windows 在线与离线 acquisition 后共用外层哈希、精确七成员名称/类型/大小、总量和压缩比、私有 bounded extraction、内部 `SHA256SUMS`、canonical manifest 与事务激活链。install root、bin dir 与 config dir 均执行固定磁盘、绝对非根、不重叠、拒绝仓库/用户根和逐祖先无 junction/reparse 校验；install root 与 bin dir 还要求同卷，避免把跨卷复制伪装为原子入口发布。
- 当前用户安装布局使用受保护的当前用户、SYSTEM 与 Administrators ACL；事务依次发布已验证 version、分离/切换 `active` junction、原子发布三个 hard-link 入口、launcher、PATH phase 与 commit，并把受限状态持久化到原子替换的 journal。失败时逆序恢复；命中 rollback failpoint 或无法证明恢复安全时保留 transaction/backup/version recovery materials 并 fail closed。测试控制仍只接受规范列出的四个变量，且 PATH phase 在私有测试根内使用固定状态文件，不修改真实用户注册表 PATH。
- Windows 原生测试覆盖 fresh install、upgrade、三入口 `--version`、launcher 默认/保持、目标和文件 ACL、离线无网络、UNC/重叠/junction 祖先/宽松来源 ACL/异常归档拒绝、9 个固定前向 failpoint 与 3 个固定 rollback failpoint。首轮 PR #5 CI run `33237097662` 暴露从 pwsh 7 父进程继承的不兼容 `PSModulePath`；signed fix commit `b5a4d5c4c6e4f4e6fb48d3361fdb94a7b26905c0` 改为 .NET ACL API 并清理测试子进程继承路径后，run `33237394630` 的六项 required checks 全部成功。
- 本地 Windows PowerShell `5.1.26100.9168` 下完整安装器矩阵、`go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...`、`go run ./cmd/doccheck --check`、固定 gitleaks `8.30.1` 合成 canary/全历史/reflog/工作树扫描及 `git fsck --full` 均通过；`fsck` 仅报告三个既有 dangling tree。
- `WP-M2-INSTALL-WINDOWS` 完成；合成 bundle 的 Windows 原生测试不等于公开 release asset 的六平台 smoke，因此 M2 继续保持 `in_progress`，下一工作包为 `WP-M2-INSTALL-NATIVE`。

## 2026-08-29 六平台原生 release-shaped 安装矩阵核验

- `WP-M2-INSTALL-NATIVE` 经原生矩阵验收的 implementation head 为 signed commit `d54a30d085d9663c03970cd66b76f5df13216b0b`。package job 从精确 PR test-merge source commit `05035df77c4e75586e9cd5b03d569cc17a0a5e78`、tree `5eb94413504bda1d8231ec99a1081aaf7435666f` 生成版本 `native-05035df77c4e`；不可变 artifact ID 为 `9713909266`，digest 为 `sha256:51341d93b6eb09e758e2a62450312abca18400294d385d8a6e986a39dead9d5d`。
- 六个原生 runner 下载并核验同一精确 artifact，没有在 runner 上重建或重新打包。`linux-arm64`、`windows-arm64`、`darwin-amd64` 执行 release-shaped asset smoke；`linux-amd64`、`windows-amd64`、`darwin-arm64` 执行包含全部固定 failpoint、rollback failure、路径攻击和权限矩阵的 full 门禁。
- 首轮 Windows job 超时的根因是测试子进程环境 allowlist 遗漏 `PSModuleAnalysisCachePath`，导致 Windows PowerShell 每次启动重建 module analysis cache。signed fix `d54a30d085d9663c03970cd66b76f5df13216b0b` 仅恢复该缓存路径，未提高任何 timeout。
- implementation-head CI run `33249498526` 的六项 required checks 全部成功；native workflow run `33249498529` 的 package 与六个原生 runner 共七个 job 全部成功。
- 本地 `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...`、`go run ./cmd/doccheck --check` 全部通过；固定 gitleaks `8.30.1` 的合成 canary、implementation-head 全历史/全部 refs/reflog 与精确工作树扫描均为 0 命中。`git fsck --full` 通过，仅报告四个 dangling tree，且无损坏或 dangling commit。
- PR #6 已以普通 merge 合入 merge commit `989cfad32aaed7352c50fb9e80233ac137362616`；双亲依次为原 main `6d9cde533d4d3bc511cc8122a96bfcfa114988a8` 与 PR head `130a698399d83b2f7cda2ab7581ac6ef4c60b8a1`，tree 为 `8c280f9c92dc739e74425a4be52e6abdae364775`，GitHub 签名为 `verified=true, reason=valid`。merge 后 CI run `33254131004` 六项检查全部成功，native run `33254130929` 的 package 与六个平台共七个 jobs 全部成功。
- `WP-M2-INSTALL-NATIVE` 与 M2 已完成，下一工作包为 `WP-M3-LIVEGATE-SCHEMA`。本节 artifact 是仅用于本 PR 原生门禁的临时 release-shaped 验证产物，不是 M3 candidate-set、公开 release、tag 或发布资产。

## 2026-08-29 Live-gate schema 收敛核验

- `WP-M3-LIVEGATE-SCHEMA` 已按 docs-first 顺序固定 schema version 1：严格 18 个顶层字段与 5 个 step 字段、封闭枚举和四个精确环境 tuple、candidate/evidence ID 与小写 hash、canonical UTC time、capability transition、primary prefix/cleanup/result 以及产品/runner 退出码映射。
- `internal/livegate` 已实现严格 UTF-8/64 KiB/unknown/duplicate/trailing JSON guards、canonical 单 LF 编码、固定且不回显输入的验证错误，并以泄漏 canary 覆盖 JSON、枚举、ID、时间、状态机与 cleanup 拒绝路径；focused coverage 为 94.3%。
- 本地 `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 与 `go run ./cmd/doccheck --check` 全部通过。
- 固定 gitleaks `8.30.1` 的合成 canary、28 commits 全历史、全部 refs/reflog 与工作树扫描均为 0 命中；`git fsck --full` 通过，仅报告四个已知 dangling tree，且无损坏或 dangling commit。
- signed implementation commit `03ffa893dce68bf83886ca047b2c9c5760a351b9` 由 GitHub 验证为 `verified=true, reason=valid`。PR #8 已以普通 merge 合入 `01e4dc59bd7787cb382e9d2392f7e6c3052a569b`；双亲依次为 `0aaff5da9ac691bcb56538074a0b3c178b140808` 与 `03ffa893dce68bf83886ca047b2c9c5760a351b9`，tree 为 `6fa99e2f23efa0ef2ac6d1b269b52bcda0514148`，GitHub 签名为 `verified=true, reason=valid`。
- merge 后 CI run `33259519538` 六项 required checks 和 native run `33259519515` 的 package 与六个平台共七个 jobs 全部成功；独立 release push run `33259518906` 仍以零 job 失败，须在 `WP-M3-CANDIDATE` / promotion 前诊断。
- `WP-M3-LIVEGATE-SCHEMA` 完成；M3 保持 `in_progress`，下一工作包为 `WP-M3-LIVEGATE-RUNNER`。本工作包未实现 runner、candidate-set、attestation、release、tag 或 promotion，未启动校园网、QR、认证或其他网络会话。

## 2026-08-30 Live-gate runner 收敛核验

- `WP-M3-LIVEGATE-RUNNER` 已实现 maintainer-only runner：冻结 candidate/manifest/hash 绑定、固定 suite 状态机与清理权、私有三文件 evidence bundle、提交点与 durability 语义，以及不保留产品原始输出的结构化结果投影；公共 SDK、CLI、稳定 JSON、退出码、workflow、candidate/promotion 和安装器均未改变。
- Linux 证据目录发布以 dirfd、`openat(O_NOFOLLOW)` 和 no-replace rename 约束并发创建，既有非私有目录只验证且 fail closed；candidate identity 指纹纳入 ctime。Windows 新建 bundle 文件显式设置当前用户 owner 与受保护 DACL，外来或 defaulted owner 继续拒绝。非 Linux/Windows 的真实 evidence publication 继续精确 fail closed，未扩展平台能力。
- PR #10 首轮 CI run `33269797459` 暴露 Linux inode 复用/并发目录创建、macOS fail-closed 测试边界和 Windows 默认 owner 差异；signed fix commit `80462712f872519da73526927476fdad69edee32` 在不放宽验证器的前提下关闭这些问题。implementation commits `e5528f46792f7d9d3d087b2b59196106d6856976` 与 `80462712f872519da73526927476fdad69edee32` 均由 GitHub 验证为 `verified=true, reason=valid`。
- 修复后本地 `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 与 `go run ./cmd/doccheck --check` 全部通过；Linux/macOS 全仓交叉构建和 `internal/livegate` 测试编译通过。固定 gitleaks `8.30.1` 合成 canary、签名前 31 commits 的全历史/全部 refs/reflog 和精确 174 个工作树文件扫描均为 0 命中；`git fsck --full` 无损坏或 dangling commit，临时扫描与构建目标已清理。
- PR #10 最终 head 的 CI run `33271001499` 六项 required checks 全部成功，native run `33271001474` 的 package 与六个平台共七个 jobs 全部成功。PR 已以普通 merge 合入 `6196234374f72089affd5442d0b5c2c0193cf62d`；双亲依次为 `7585eb6c08d4e4471ae4783bd8c27fa10a9ebf23` 与 `80462712f872519da73526927476fdad69edee32`，tree 为 `b9383072cd8c32bf1f0aafd6596b4a6c0ae86077`，GitHub 签名为 `valid`。
- merge 后 CI run `33271253178` 六项和 native run `33271253166` 七项全部成功。同一 merge SHA 的 release push run `33271252579` 仍以精确零 job 失败，证明既有发布 workflow 缺口仍存在；它不是 main required check，进入 `WP-M3-CANDIDATE` 修改前必须先只读诊断，且不得据此创建 tag、release 或发布资产。
- `WP-M3-LIVEGATE-RUNNER` 完成；M3 继续保持 `in_progress`，下一工作包为 `WP-M3-CANDIDATE`。本工作包未创建 candidate-set、attestation、release、tag 或 promotion，也未启动校园网、QR、认证或其他真实网络会话；`REL-LIVE-MATRIX-001` 仍未执行。

## 2026-08-30 不可变 Candidate 流水线收敛核验

- 既有 `.github/workflows/release.yml` 的零-job failure 已完成只读根因复现：其 job 级 `env` 在 workflow 处理阶段引用不可用的 `runner` context；固定 actionlint `v1.7.12` 精确拒绝原第 74 行。该会在 tag 阶段重新构建并直接公开 release 的旧 workflow 已删除；CI 现固定安装 actionlint，并由 `internal/workflowguard` 同时锁定 workflow 触发、权限、action pin 与不可重建边界。
- `WP-M3-CANDIDATE` 已实现 maintainer-only `workflow_dispatch`：只接受精确 `v1.0.0` 与受保护 `main` 的完整 source SHA，验证 source 签名、tag 不存在及同 SHA 的六项 CI 和七项 Native checks；本轮按 [`ADR-0011`](../architecture/decisions/ADR-0011-nonblocking-m0-governance.md) 把状态 gate 收敛为 M1–M2，M0 不参与判定。单一 build job 按冻结 toolchain/build-input 只构建一次，生成 canonical full/release manifest、确定性六平台 bundle、两级 checksums、公开十资产与私有两 helper；六平台原生安装只下载同一不可覆盖 artifact，attestation job 重新下载并 full-verify 后仅为 candidate-set 与十个公开资产生成 11 个 subjects。
- 本地 focused candidate/CLI/installtest/workflowguard、actionlint、Linux 执行测试、Windows/Linux/macOS amd64/arm64 交叉构建、`go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 与 `go run ./cmd/doccheck --check` 均通过。固定 gitleaks `8.30.1` 合成 canary、全历史/全部 refs/reflog 与精确 205 个工作树文件扫描均为 0 命中；`git fsck --full` 无损坏或 dangling commit，扫描和交叉构建临时目标均已清理。
- PR #12 首轮 CI run `33279922238` 暴露 Unix rename ctime、测试暂存导致 executable mode 漂移以及 shellcheck 重定向/字面 Markdown 告警；signed fix `73f5aa30a29aebb60970319ea378bb20b62bdf06` 保留同一已打开对象的 identity/mode/size/mtime/hash 与目录精确成员验证，仅对 Unix 根目录 no-clobber rename 的 ctime 变化使用重命名后验证。implementation commit `74cb8f6e8dab02b2d1d935640acdb885ea19ff77` 与 fix 均由 GitHub 验证为 `verified=true, reason=valid`。
- PR #12 最终 head 的 CI run `33282461292` 六项 required checks 和 Native run `33282461260` 的 package 与六个平台共七个 jobs 全部成功。PR 已以普通 merge 合入 `382a8f994761a4a5d73f578d08f263718eac958c`；双亲依次为 `f907e270273bb376b37439e149567fa0398ed976` 与 `73f5aa30a29aebb60970319ea378bb20b62bdf06`，tree 为 `0206c0a06d3c8c46fc1225c5440d8a61b7ad2554`，GitHub 签名为 `valid`。merge 后 CI run `33282708893` 六项和 Native run `33282708924` 七项再次全部成功。
- `WP-M3-CANDIDATE` 完成，但 M0/`SEC-HISTORY-001` 仍为 `in_progress`，因此 `make candidate-gate` 按预期拒绝，Candidate workflow 未 dispatch，未创建正式 candidate-set、artifact、attestation、tag、release 或发布资产。M3 继续保持 `in_progress`，下一工作包为 `WP-M3-PROMOTION`；`REL-LIVE-MATRIX-001` 仍未执行，也未启动校园网、QR、认证或其他真实网络会话。

## 2026-08-30 Promotion 流水线收敛核验

- `WP-M3-PROMOTION` 已按 docs-first 顺序冻结 closed-world canonical `promotion-lock.json`、四份 public evidence summary、精确 release notes、source→promotion 白名单与 build-input 重算契约；Promotion workflow 只接受签名 annotated `v1.0.0` 和受保护 main 当前 tip，按精确 run/artifact API、原始 artifact digest、14 文件 Candidate、两级 manifest/checksum、11-subject attestation 与四份 evidence 绑定做封闭验证。
- Promotion 全程 no-build/no-repack：禁止 setup-go、build、package、strip、sign、clobber、单资产替换和自动 mutation 重试；只允许一次创建不可见 draft，逐项重新下载核对十个公开资产，再在同一步 mutation 前复核 gate/lock/tag/main/draft，最后一次公开并再次 fresh download 验证。stdlib verifier 及 7 组合成 happy-path/tamper 测试、workflowguard 与 CI actionlint 门禁均已落盘。
- 本地 `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...`、`go run ./cmd/doccheck --check`、actionlint 与六目标 cross-build 全部通过；固定 gitleaks v8.30.1 合成 canary、签名后 50 commits 的全历史、全部 refs/reflog 与精确 209 文件工作树扫描均为 0 finding。`git fsck --full` 无损坏，仅报告 1 个 dangling blob 与 7 个 dangling tree，全部临时扫描/cache/build 路径均已清理。
- signed implementation commit `466276fd28a855370a10ca8421117811b4e4ef13` 由 GitHub 两个 commit API 验证为 `verified=true, reason=valid`。PR #14 head CI run `33290209456` 六项 required checks 和 Native run `33290209465` 的 package 与六个平台共七个 jobs 全部成功；PR 已以普通 merge 合入 `e542f108a32e37f8313c9673cbd68254af25968c`，双亲依次为 `7660348949745ed4193a545c452097cbc91f9c92` 与 implementation head，tree 为 `0d2d0dcb9439bccc67945d1b94d367374a90f776`，GitHub 签名为 `valid`。
- merge 后 CI run `33290459305` 六项和 Native run `33290459311` 七项再次全部成功。`make promotion-gate` 因 M0 `in_progress` 按预期拒绝；Candidate/Promotion workflow 均未 dispatch，未创建或使用正式 candidate-set、artifact、attestation、promotion lock/evidence、tag、draft、release 或发布资产，也未启动校园网、认证、QR 或其他真实网络会话。
- `WP-M3-PROMOTION` implementation 完成；三个旧对象的 GitHub Support 外部待办完成前，`SEC-HISTORY-001` 与 M0 继续保持 `in_progress`，M0 仍禁止新 release。M3 也继续保持 `in_progress`；下一工作包为独立只读、无凭据且不创建 VM 的 `LAB-DISCOVER`。

## 2026-08-30 LAB-DISCOVER BHK 匿名路径核验

- ZOS 隔离能力尚未按 [`REL-LAB-002`](../runbooks/campus-lab.md#rel-lab-002发现与供应) 证明，未创建或启动 VM。BHK fallback 通过经人工核对 host key 的 WSL SSH 链路调用原生 Windows 网络栈；exact-main Windows/amd64 binary、受保护空配置、物理有线/Wi-Fi、TUN 与覆盖路由消失、非 fake-IP DNS、constrained-source 物理出口及 RealVNC 连通性均在请求前通过核对。
- 配置目录首次预检因 Windows 临时目录继承 DACL 而在创建 HTTP client 前 fail closed；两次命令均未发出请求。切换到仅当前用户、SYSTEM 与 Administrators 可访问的受保护空目录后，纯离线 `profile list` 成功且未创建配置文件。
- 维护者批准有线与 Wi-Fi 各一次替代 `status` 窗口；实际仅有线执行一次并返回有效 `protocol_changed` 契约，随后按停止规则终止，Wi-Fi 未执行，总真实请求数为 1。未使用凭据，未运行 `network scan`、login 或 logout，未采集、保存或输出原始响应；该结果按 [`REL-LIVEGATE-002`](../operations/live-validation.md#rel-livegate-002suite-状态机与清理权) 为 fail，不构成正式 live evidence，也不能外推为协议、认证或 M3 完成。
- 代码核对发现状态格式无法识别时 producer 使用未列入 CLI 脱敏 allowlist 的 `ProtocolPart="status"`，导致安全诊断字段被清空。docs-first 修复把该路径统一为规范允许的 `gateway_status`，未放宽解析器、改变 endpoint/transport 或增加响应暴露；合成 SDK/CLI 测试同时证明允许值保留、未知值清空且 response canary 不进入错误 JSON。
- signed implementation commit `8c1e24aa66824062580ec380d4bb2acce41ae991` 经 PR #16 的 CI run `33310700735` 六项 required checks 与 Native run `33310700716` 七项全部成功后，以普通 merge 合入 `ce57f4e2474055defba00195645db363729d3533`；merge 双亲、tree `cd9ee5cb7c41e0900917b37d2e2491eac5f92e8d` 与 GitHub 签名均已精确复验，merge 后 CI run `33310968357` 六项与 Native run `33310968406` 七项再次成功。
- implementation tree 的 focused test、`go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...`、`go run ./cmd/doccheck --check` 全部通过；固定 gitleaks `8.30.1` 的合成 canary、39 commits 全历史/refs/reflog 与 209 文件工作树扫描为 0 finding。`git fsck --full` 无损坏或 dangling commit，仅报告 1 个 dangling blob 与 14 个 dangling tree。M0、`SEC-HISTORY-001` 与 M3 均继续保持 `in_progress`。

- 维护者随后批准 exact-main 的单次有线、无凭据协议形状诊断。请求前再次核对远端 main、已知 rulesets 与 refs、source/tree/signature、helper hash、受保护空配置、system proxy、物理网卡、TUN/覆盖路由为零、非 fake-IP DNS、constrained-source 有线出口和 RealVNC；全部通过后仅执行 1 个新网关 round trip。BHK 匿名阶段累计真实请求数为 2，Wi-Fi 仍未执行。
- 封闭结果为 `http_class=http_2xx`、非重定向、`body_size_bucket=1_1024`、`leading_shape=other`、全部状态/身份/流量字段与 marker presence 为 false、`legacy_csv_shape=true`，产品分类为 `protocol_gateway_status` / `gateway_status`，包装器确认 `gateway_requests=1`。该结果只证明 HTTPS 返回了现有解析器未接受的单行多列 legacy CSV-like 形状，不证明字段含义、会话、认证或正式 evidence；未采集、保存或输出正文、响应头、URL、账号、IP 或原始 stdout/stderr。请求后 postflight 通过，本地 helper、WSL staging 与 Windows 受保护 helper 目录均已精确清理；旧 exact-main 产品 binary 与受保护空配置未改动。
- 本次 docs-only closeout tree 的 `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 与 `go run ./cmd/doccheck --check` 全部通过；固定 gitleaks `v8.30.1` 的合成 canary、40 commits 全历史/全部 refs/reflog 与工作树扫描均为 0 finding。`git fsck --full` 无损坏或 dangling commit，仅报告 1 个 dangling blob 与 14 个 dangling tree。

## 2026-08-31 legacy CSV 状态兼容核验

- docs-first 已固定 positional legacy CSV 的最小 online 身份不变量：去除外层空白后必须是单个无内嵌 CR/LF、至少 9 列的 CSV record，位置 0 为合法 username，位置 8 为全局单播 IPv4；其他列保持 opaque，不参与身份、online/offline 或冲突推断。
- 位置 6/7/11 只形成 all-or-nothing 的可选 summary 候选。流量与时长必须同时为 `int64` 范围内的非负十进制整数，非空余额必须可精确转换为 minor units；否则只保留已经成立的 online 身份并令 summary 整体为 `nil`，不返回部分摘要。该降级仅适用于 legacy CSV；JSON/JSONP 显式命名 summary 的无效、部分或 alias 冲突继续返回 `protocol_changed`。
- 实现只调整 `internal/srun` 的 legacy CSV 解析顺序并增加内部与公共 SDK 合成边界测试；公共 SDK/CLI/JSON/退出码、endpoint、transport、请求次数和脱敏边界均未改变。聚焦普通/race 测试与全量 `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...`、`go run ./cmd/doccheck --check` 全部通过。
- 固定 gitleaks `v8.30.1` 的合成 canary、41 commits 全历史/全部 refs/reflog 与工作树扫描均为 0 finding。`git fsck --full` 无损坏或 dangling commit，仅报告既有 1 个 dangling blob 与 14 个 dangling tree。
- signed implementation commit `dd7b563936a6c2472896fc4e2fd288e5d0cae536` 由 GitHub 验证为 `valid`；PR #19 head CI run `33323660497` 六项 required checks 与 Native run `33323660534` 七项全部成功，并以普通 merge 合入 main `2348f9a0bf208e56303222e03c0137618ff700fb`。merge 双亲依次为 `ea152221813020578c69bdfb604cc07ec00edcbe` 与 implementation commit，tree 为 `51abf026059e6a4e005952306ad046d785326764`，GitHub 签名为 `valid`；merge 后 CI run `33323891972` 六项与 Native run `33323892073` 七项再次全部成功。未执行新的校园网请求；任何真实有线 `status` 仍须取得新的单请求批准，M0、`SEC-HISTORY-001` 与 M3 继续保持 `in_progress`。

## 2026-08-31 exact-main 有线匿名复核窗口

- 维护者批准通过现有 `BHKDesktop-WSL` 使用最多 4 次 strict SSH/SCP 连接，重新取得并验证 exact-main Windows/amd64 artifact，并只在物理有线上以受保护空配置尝试一次无凭据 `status`；完整 preflight 不通过时必须保持网关请求为 0。请求前复核确认 main `147bfa9bcaf357b175bc183f6e67543223a62d8b` / tree `335677e39ca390f55eea576c72250aa6a3c8d372`、签名、完整 refs、rulesets 与 Native run `33324692028` 均无漂移；artifact `9735897428` 的外层 digest、manifest、release hashes、Windows archive、目标 SHA-256 与 build info 全部逐层匹配。该 artifact 不是 Candidate、tag 或 release。
- 第 1 次连接只创建固定 WSL 私有 staging，第 2 次只传输 exact binary 和封闭 runner；第 3 次通过远端成员/hash 门禁后，Windows topology preflight 以 `topology_preflight_internal` 在产品启动前 fail closed。封闭结果精确为 `request_attempted=false`、`status_invocations=0`、`gateway_request_upper_bound=0`，且 Windows 与 WSL 固定目标均确认清理。源码复核确认 `status` 只有一个 `client.Do`、无重试且拒绝 redirect；因此本窗口实际产品调用和网关请求均为 0。
- 剩余第 4 次连接仅重复只读 topology 诊断，不运行产品、不创建文件且网关请求为 0；其远端 stderr 非空，因此本地封闭输出契约拒绝结果，未读取、保存或回显错误正文，也未安全确定更细的失败步骤。4 次连接额度到此全部消耗，没有重试 `status`。该窗口不构成 live evidence，legacy CSV 兼容仍未获得 live 复核；继续远端诊断或新的有线 `status` 需要新的明确授权。M0、`SEC-HISTORY-001` 与 M3 继续保持 `in_progress`。
- 本轮远端 Windows/WSL 固定目标均已确认清理；本地已忽略的 artifact/runner 目录也在精确路径、零 reparse、零 tracked 成员门禁后删除并复核 absent。收尾 `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 与 doccheck 全部通过；固定 gitleaks `v8.30.1` 的合成 canary、63 commits/50 refs/178 reflog entries 全历史与 209 文件工作树扫描均为 0 finding。`git fsck --full` 无损坏或 dangling commit，仅报告 1 个 dangling blob 与 23 个 dangling tree；扫描临时目标残留为 0。

## 2026-08-31 BHKDesktop Win11 有线匿名兼容复核

- 维护者随后改用现有 Xftp/Xshell 会话在 BHKDesktop 的 WSL2 中人工执行自包含封闭 harness；Codex 未再直接 SSH/SCP。WSL2 只负责上传、staging、调用 Windows PowerShell、字节计数与清理，八阶段 topology 查询和 Windows/amd64 产品均使用 Windows 11 原生网络栈。zero-DNS 合成 preflight 与一次 real-DNS preflight 最终完整通过，且没有输出或保存 IP、接口、MAC、SSID、DNS 答案、URL、命令、路径或原始错误。
- status bundle 内嵌干净 clone 上以 Go `1.26.1` 构建的 exact-source Windows/amd64 binary：大小 `8242176`、SHA-256 `158357fde02a590ee0bee921627a21fded05e0c12ced49dc6c096d16c8070bd5`、`vcs.revision=147bfa9bcaf357b175bc183f6e67543223a62d8b`、`vcs.modified=false`。它不是 Native run `33324692028` 的 Go 1.25 artifact，也不是 Candidate、tag、release 或正式 evidence。
- 最终 bundle SHA-256 为 `e72659dfdbf1165142591eb6c2542b0aeed7572bd44ca345980ad50366d8ffb4`，远端 shell 自动校验返回 `upload_present=true`、`sha256_match=true`。封闭结果为 `result=pass`、`child_phase=complete`、`dns_invocations=1`、`status_invocations=1`、`gateway_request_upper_bound=1`、`status_outcome=success`、`network_state=reachable`、`session_state=online`、`cleanup_confirmed=true`；因此 legacy CSV 兼容实现已在该 exact-source build 的 BHKDesktop Win11 物理有线真实响应上通过匿名 parser compatibility 复核。
- 本次没有运行 login/logout、使用凭据、切换 Wi-Fi、执行 `network scan`、修改网络或读取原始产品输出/身份/网络标识。`session_state=online` 只表示匿名 status 观察到既有在线会话；按 [`REL-LIVEGATE-001`](../operations/live-validation.md#rel-livegate-001runner-接口与信任边界)，正式 suite 的 initial status 必须明确 offline，Agent 不得为测试自动 logout。本结果不提升认证能力、不进入 promotion evidence，也不改变 M0、`SEC-HISTORY-001` 或 M3 的 `in_progress` 状态。
- `cleanup_confirmed=true` 确认固定 WSL staging、Windows `%LOCALAPPDATA%` 目标和上传脚本均已清理。已忽略的本地 harness 通过 PowerShell 5.1 synthetic/AST/命令闭集、Bash mapper/E2E、确定性重建、gitleaks、doccheck、`git diff --check` 与 Git ignore/tracked/index 门禁，保持 0 个 tracked 路径且未暂存、未提交。

## 2026-09-01 LAB-DISCOVER ZOS 只读能力结论

- 维护者提供的非唯一性设备与版本信息确认测试对象为双口 Z4Pro，系统版本 `V1.0.0430216`、服务版本 `V1.0.0430398.2632`；只读 UI 显示当前为解绑 Bond，两个物理口各自拥有宿主 IP 并支持多网关。期间未记录账号、完整 IP/MAC 或其他网络标识。
- ZOS 官方 UI 同时提供“双网口加入同一网桥”和“双网口分别创建网桥（适用于软路由）”；后者可为两个物理口分别建立网桥并供 VM 指定网口。官方[网络设置相关注意事项](https://www.zspace.cn/help/?cid=0&articleId=100197)说明软路由网桥仍可由 ZOS 配置 IP，并警告使用主网口的 VM 会阻止该端口故障时的自动切换、可能导致 ZOS 管理面失联；官方[虚拟机功能说明](https://www.zspace.cn/help/?cid=1068&articleId=100167)也只证明桥接 vNIC 与物理口选择能力。
- 现有正式支持能力未证明 NAS 宿主可让测试物理口同时保持无 IP、DHCP 和默认路由，也未展示把该端口独占直通给 VM 的选项。依 [`REL-LAB-002`](../runbooks/campus-lab.md#rel-lab-002发现与供应) 的“能力不明即 blocked”规则，`LAB-DISCOVER` 只读窗口以 `blocked` 结果完成；没有切换网络模式、保存设置、创建网桥/VM、修改网络或启动认证。
- ZOS 后续仅用于离线 Linux 安装和无凭据合成测试，NAS 真实网络路径不进入 `LAB-PROVISION`。BHKDesktop Win11 物理有线匿名 parser compatibility 的既有 `pass` 结论保持独立，不构成 Candidate、正式 live evidence 或认证结果。

## 2026-09-01 M0 非阻塞发布门禁决策

- 维护者接受 GitHub 仍可按对象 ID 访问三个已失去 ref 可达性的旧 commit 这一残余治理风险，并决定 M0/`SEC-HISTORY-001` 继续保持 `in_progress`、继续跟踪 GitHub Support 与既有副本处置，但不再阻塞 Candidate、Promotion、release 或真实验收；不得把该决定改写成 M0 已完成。
- [`ADR-0011`](../architecture/decisions/ADR-0011-nonblocking-m0-governance.md)把状态判定固定为：Candidate 只要求 M1/M2 `complete`；Promotion 只要求 M1/M2 `complete` 且 M3 `in_progress`；release 只要求 M1/M2/M3 `complete`。M0 的缺失或任意状态均不影响这三个 gate 的结果。
- required CI、完整可达历史/refs/reflog 与工作树 secret scan、泄漏测试、source/main/tag 签名、ruleset、Candidate identity、build-input、artifact digest、attestation、真实网络 evidence、promotion lock 和逐资产复核均保持独立 hard gate；本决定不授权 Candidate dispatch、tag、draft、release、校园网或认证操作。
- 共用 `scripts/milestone-gate.sh` 已成为 Makefile 三个状态 gate 的唯一判定入口；53 个合成用例证明 M0 的 `not_started|in_progress|blocked|complete` 与缺失均不改变结果，同时覆盖 M1/M2/M3 缺失、重复和错误状态的封闭拒绝。当前正式 status 下 Candidate 与 Promotion gate 通过，release 因 M3 仍为 `in_progress` 按预期拒绝。
- 本地聚焦与全量普通/race 测试、vet、doccheck、actionlint、7 项 promotion verifier、六目标交叉构建、Bash AST、固定 gitleaks 合成 canary/43 commits 全历史/16 个改动文件扫描和 `git fsck --full` 均通过；fsck 无损坏或 dangling commit，仅报告既有 1 个 dangling blob 与 25 个 dangling tree。
- 本工作包只实现规范、状态 gate 与合成回归测试，并提交签名 PR 等待 required CI；不自动 merge。合入后下一工作包为 `RC-BUILD` 的独立授权窗口；ZOS `blocked`、BHK 既有会话 `online` 和正式 live matrix 的其他约束不因 M0 解耦而改变。
- PR #21 head 的 CI 六项与 Native 七项全部成功，并以普通 merge 合入 main `3786382bfe0b91c428d45166d4dbb046b57c720e`；merge 双亲、tree `f0a7cf1fff2a66d91e2939e3611502d1ae4923fe` 与 GitHub 签名均已精确复验，合入后 CI run `33463522316` 六项和 Native run `33463522437` 七项再次全部成功。维护者随后授权进入 `RC-BUILD`。
- 第一次 Candidate dispatch run `33464985630` 在任何 build/upload/attestation 前由 preflight 封闭失败：check-run hard gate 的 jq 使用了不可编译的 `all(.[] as $name; ...)` 形式；source identity、immutable inputs 与 checkout 已通过，后续所有 jobs 均 skipped，未创建 artifact、attestation、tag、draft 或 release。修复把 filter 拆为可直接执行合成测试的 `scripts/candidate-checks.jq`，保留精确 13 项 checks、最新同名 run、GitHub Actions app/source SHA 和 100 项分页上限约束；修复合入并通过新 main checks 前不得再次 dispatch。
- jq 修复的 signed commit `377ad0c587ae60555d190c7f08af3604d5303915` 经 PR #22 的 CI 六项与 Native 七项全部成功后，以普通 merge 合入 main `bd9df2d16edd1eaaf9c79d8fe2c7956606f321c5`；merge 双亲、tree `fa785418736d6b70b30f9446e667ae065facaa82` 与签名已复验，合入后 CI run `33466147317` 六项和 Native run `33466147353` 七项再次成功。
- 第二次 Candidate dispatch run `33466493360` 的 preflight 与唯一 build/upload job 成功，artifact `9785072853` 名称为 `candidate-set-v1.0.0-bd9df2d16edd-33466493360.1`、服务端 digest 为 `sha256:0c5bd5fd1ef68619f0c7444221ba03b347a563d6bc0630257ea367b05af496a5`。同一 artifact 在 Linux/macOS 四项原生验证通过，但 Windows amd64/arm64 均在安装前的 full verifier 阶段失败；根因是 workflow 传入 `D:\.../ipgw-candidate-set` 样式的绝对混合分隔符路径，而 CLI 未按 `ABS_PATH` 接口在调用核心 verifier 前执行 `filepath.Clean`。本地按精确 artifact 下载后以 clean absolute path 完整验证通过，Candidate-set SHA-256 为 `3fb183c76e9d00a43ac7b204d6b39df85a09f57cce7fe20680a31e3a54dd1763`；attestation job 按依赖 skipped，因此该 artifact 不是可接受 Candidate，不得进入 `LAB-TRANSFER`。
- CLI 路径适配的 signed commit `b2ef9bac5a6984e28ee2cdf4e5505a0c42db8d27` 经 PR #23 的 CI run `33467789575` 六项与 Native run `33467789580` 七项成功后，以普通 merge 合入 main `8f6831511aaaad04af8f3f5749310295427a2055`；merge 双亲、tree `25950fc821b9aebfcd89ba161074d6dc9d2d6b51` 与签名已复验，合入后 CI run `33468055045` 六项和 Native run `33468055065` 七项再次成功。
- 第三次 Candidate dispatch run `33468379539` 的 preflight、build/upload 和六平台 full verifier 均成功；artifact `9785669867` 名称为 `candidate-set-v1.0.0-8f6831511aaa-33468379539.1`、服务端 digest 为 `sha256:f42ccd0468a0e432f59df3b4ee3d0e871c90cd2d1f079fd9346644481f4ec8aa`。Linux/macOS 四项安装通过，但 Candidate-mode installtest 从同一未规范化 `IPGW_NATIVE_INSTALL_ARTIFACT_ROOT` 再次直接调用严格核心 verifier，导致 Windows amd64/arm64 在 installtest 阶段失败；attestation skipped。精确 artifact 的本地核心验证与修复后的 Windows/amd64 Candidate-mode 安装 smoke 均通过，Candidate-set SHA-256 为 `a3d91988d230716b25207bb91d9331a58cbedbc9a7911b59ba185ed3ce995ac5`。该 artifact 同样不是可接受 Candidate，不得进入 `LAB-TRANSFER`；修复只在 installtest 绝对路径环境适配层执行 `filepath.Clean`，核心 verifier 保持不变。

## 2026-08-27 本地验收结果

- `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 与 `go run ./cmd/doccheck --check` 均通过。
- `ipgw`、`ipgw-meta`、`ipgw-legacy` 在 Windows/Linux/macOS 的 amd64/arm64 共 18 个二进制构建通过；`internal/config` 测试在同六目标编译通过，Windows/amd64 race 实际执行通过。
- `install.ps1` PowerShell AST、`install.sh`/`scripts/release.sh`/`scripts/test-gitleaks.sh` Git Bash 语法以及 `make -n ci` 均通过。它们不是六平台实际安装、升级、回滚证据。
- gitleaks 正/负合成 canary 与排除 `.git`、`build`、module cache 后的当前源码快照扫描通过。截至该次 2026-08-27 验收尚未执行历史重写；后续处理与复核结果见上节。

## 尚未完成与外部条件

- `SEC-HISTORY-001` 仍为 `in_progress`：会话失效确认、冻结提交、受限历史备份、全 refs 重写、tag 复核、全历史复扫、atomic per-ref lease 远端更新和本机 fresh clone 已完成；GitHub Support 缓存/悬空对象清理以及其他既有副本的重新克隆仍待完成。
- 仓库治理、只读 baseline、所有 M2 工作包、`WP-M3-LIVEGATE-SCHEMA`、`WP-M3-LIVEGATE-RUNNER`、`WP-M3-CANDIDATE`、`WP-M3-PROMOTION` 与只读 `LAB-DISCOVER` 均已完成；`LAB-DISCOVER` 的 ZOS 结果为 `blocked`，BHK fallback 仅关闭匿名 parser compatibility 不确定性。三个旧对象完成 GitHub Support 清理前，`SEC-HISTORY-001` 与 M0 继续保持 `in_progress`，但按 ADR-0011 不再阻塞后续工作；本门禁变更合入后可在独立授权窗口进入 `RC-BUILD`。
- 六平台原生 release-shaped asset 安装矩阵已使用同一精确临时 PR artifact 完成；该 artifact 只构成 M2 原生门禁证据，不是 M3 candidate-set、公开 release、tag 或发布资产。
- 既有 `.github/workflows/release.yml` push runs `33259518906` 与 `33271252579` 的零-job 根因已复现并随 `WP-M3-CANDIDATE` 关闭；旧 workflow 已删除，candidate workflow 与 CI actionlint/workflowguard 门禁已合入。目前仍不存在可供真实验收或 promotion 使用的 M3 candidate artifact；M0 解耦合入后仍须在独立窗口明确 dispatch `RC-BUILD`，不会由本工作包自动触发。
- `WP-M3-CANDIDATE` 与 `WP-M3-PROMOTION` 已完成并冻结符合 [ADR-0007](../architecture/decisions/ADR-0007-immutable-candidate-promotion.md) 的一次构建、验证、attestation 与 no-build 原样晋升路径。目前尚未生成正式 candidate-set、attestation、promotion lock/evidence、tag、draft 或 release；临时 PR artifact 不得视为 candidate-set，M3 继续为 `in_progress`。当前 `LAB-DISCOVER` 状态与后续边界见上节；仍不创建 VM、不切线、不启动认证。
- `LAB-DISCOVER` 已完成：ZOS 路径因未能证明 [`REL-LAB-002`](../runbooks/campus-lab.md#rel-lab-002发现与供应) 要求的宿主测试口无 IP/DHCP/default route 而以 `blocked` 收敛，且没有通过改网或创建 VM 继续探测。BHK fallback 的 Win11 物理有线匿名 topology 与 exact-source parser compatibility 已按上节封闭通过，但 Wi-Fi、正式 Candidate/evidence 与认证矩阵均未执行；匿名结果观察到既有会话 online，不能作为 live-gate initial offline 或认证成功证据。M3 继续保持 `in_progress`。
- `REL-LIVE-MATRIX-001` 未执行：校园有线/无线的 password 场景和至少一种网络的 Terminal QR 扫码闭环都需要在 [ADR-0009](../architecture/decisions/ADR-0009-separated-live-test-plane.md) 的隔离边界内完成，并按证据规范脱敏落盘。

## 当前已知能力限制

- v1 在具备可靠 network fingerprint 前禁用持久协议缓存 fallback；绑定 IP 不作为网络身份，每次操作执行动态发现。`WithProtocolStateStore` 仅为未来扩展缝，详见 [`SDK-CACHE-001`](../reference/go-sdk.md#sdk-cache-001协议状态存储)。
- `AUTH-SMS-001` 手机验证码仅为 `observed_anonymous + detected_only`，目前没有可触发的真实测试条件，不阻塞 v1。
- `AUTH-QR-001` Terminal QR 在真实校园网验证前保持 `live_unverified`。
- `AUTH-PASSWORD-001` 在当前候选完成有线、无线真实验收前保持 `live_unverified`。
- 当前可用性仅有 2026-08-27 的人工使用报告，不构成发布门禁证据。
- M0 残余治理继续 `in_progress`，但不再参与 Candidate、Promotion 或 release 状态 gate；其他独立 hard gate 仍必须全部通过。

## 更新规则

完成状态必须同时附带可复现的自动化结果或符合 `EVID-REDACT-001` 的真实网络证据。不得因“暂时没有复现”关闭安全或协议问题。
