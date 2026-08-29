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

截至 2026-08-28，以下内容已进入重写后的 `codex/v1-freeze` 和已验证的 fresh clone，但“实现存在”不代表对应发布门禁已完成：

- 文档/agent 分层、稳定 ID 索引与 `doccheck` 已建立；工作树中的真实测试 fixture 已移除并由合成 fixture 替代，secret scan 配置及其合成 canary 已接入，自更新代码与可达入口已移除。
- 根 `ipgw` SDK façade、typed errors、接口枚举和 internal CAS/Srun/Dashboard 边界已实现；HTTPS-only、ticket HTTP 下一跳进程内截获、动态 CAS 表单/公钥、JSON/JSONP、最终身份核对、账号冲突、幂等 logout、挑战分类和脱敏 Observer 均已有合成或 `httptest` 覆盖。
- `ipgw`、`ipgw-meta`、`ipgw-legacy` 三入口、模式优先级、稳定 JSON envelope/退出码、named profiles、keyring/env/file/prompt provider、事务化双来源配置迁移及原子 bundle/安装脚本已实现。Config 读取、跨进程 mutation lock、pending-journal 写阻断、preview 目标代次核对和 keyring 提交失败清理均已加固并有确定性 race 覆盖。
- CI/release workflow、六目标交叉构建定义和 release gate 已写入仓库；实现与治理已通过 PR #1 以普通 merge 合入 `main` 的 `f927d7316885a26c8289ba77bc04ed27e379d3c8`（tree `4c5f253efebd5cea7bc85b0b5de5c2af84ed54f3`），但尚未形成 candidate-set、安装或真实校园网证据，因此不能外推为 M3 完成。

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

## 2026-08-27 本地验收结果

- `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 与 `go run ./cmd/doccheck --check` 均通过。
- `ipgw`、`ipgw-meta`、`ipgw-legacy` 在 Windows/Linux/macOS 的 amd64/arm64 共 18 个二进制构建通过；`internal/config` 测试在同六目标编译通过，Windows/amd64 race 实际执行通过。
- `install.ps1` PowerShell AST、`install.sh`/`scripts/release.sh`/`scripts/test-gitleaks.sh` Git Bash 语法以及 `make -n ci` 均通过。它们不是六平台实际安装、升级、回滚证据。
- gitleaks 正/负合成 canary 与排除 `.git`、`build`、module cache 后的当前源码快照扫描通过。截至该次 2026-08-27 验收尚未执行历史重写；后续处理与复核结果见上节。

## 尚未完成与外部条件

- `SEC-HISTORY-001` 仍为 `in_progress`：会话失效确认、冻结提交、受限历史备份、全 refs 重写、tag 复核、全历史复扫、atomic per-ref lease 远端更新和本机 fresh clone 已完成；GitHub Support 缓存/悬空对象清理以及其他既有副本的重新克隆仍待完成。
- 仓库治理、只读 baseline、`WP-M2-CONFIG-CLOSE`、`WP-M2-INSTALL-UNIX` 与 `WP-M2-INSTALL-WINDOWS` 已完成；下一工作包为 `WP-M2-INSTALL-NATIVE`。三个旧对象完成 GitHub Support 清理前，`SEC-HISTORY-001` 与 M0 继续保持 `in_progress`，且 M0 完成前仍禁止新 release。
- 尚未完成六目标公开 release asset 的实际安装、升级、回滚 smoke test；当前 Unix 合成 bundle 已在 Ubuntu 与 macOS 原生 runner 覆盖 acquisition、路径、权限和完整固定 failpoint，但不能替代 `WP-M2-INSTALL-NATIVE` 的 release-asset 矩阵。
- 既有 `.github/workflows/release.yml` 在 push 上仍出现无 job 的即时失败；它不是 main ruleset 的六项 required checks，也非本工作包引入，但进入 M3 candidate/promotion 前必须按发布规范诊断并关闭该门禁缺口。
- 尚未实现并冻结符合 [ADR-0007](../architecture/decisions/ADR-0007-immutable-candidate-promotion.md) 的 candidate-set，也未完成 attestation/promotion workflow。因此 M3 保持 `not_started`，M0 未完成前继续禁止新 release。
- `REL-LIVE-MATRIX-001` 未执行：校园有线/无线的 password 场景和至少一种网络的 Terminal QR 扫码闭环都需要在 [ADR-0009](../architecture/decisions/ADR-0009-separated-live-test-plane.md) 的隔离边界内完成，并按证据规范脱敏落盘。
- Unix 与 Windows 离线安装器均已满足 [ADR-0008](../architecture/decisions/ADR-0008-offline-transactional-installer.md) 的合成 acquisition、权限/reparse 和完整 failpoint 门禁，但六平台原生公开 release-asset 矩阵仍未执行。

## 当前已知能力限制

- v1 在具备可靠 network fingerprint 前禁用持久协议缓存 fallback；绑定 IP 不作为网络身份，每次操作执行动态发现。`WithProtocolStateStore` 仅为未来扩展缝，详见 [`SDK-CACHE-001`](../reference/go-sdk.md#sdk-cache-001协议状态存储)。
- `AUTH-SMS-001` 手机验证码仅为 `observed_anonymous + detected_only`，目前没有可触发的真实测试条件，不阻塞 v1。
- `AUTH-QR-001` Terminal QR 在真实校园网验证前保持 `live_unverified`。
- `AUTH-PASSWORD-001` 在当前候选完成有线、无线真实验收前保持 `live_unverified`。
- 当前可用性仅有 2026-08-27 的人工使用报告，不构成发布门禁证据。
- M0 完成前禁止发布新版本。

## 更新规则

完成状态必须同时附带可复现的自动化结果或符合 `EVID-REDACT-001` 的真实网络证据。不得因“暂时没有复现”关闭安全或协议问题。
