---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
generated: true
---

# 稳定 ID 索引

本文件由 `doccheck` 根据 `docs/` 中的权威标题确定性生成，请勿手工编辑。

| 稳定 ID | 标题 | 权威声明 |
|---|---|---|
| `ADR-0001` | 产品边界 | [docs/architecture/decisions/ADR-0001-product-boundaries.md](architecture/decisions/ADR-0001-product-boundaries.md#adr-0001产品边界) |
| `ADR-0002` | 兼容与切换 | [docs/architecture/decisions/ADR-0002-compatibility-and-cutover.md](architecture/decisions/ADR-0002-compatibility-and-cutover.md#adr-0002兼容与切换) |
| `ADR-0003` | 安全传输 | [docs/architecture/decisions/ADR-0003-secure-transport.md](architecture/decisions/ADR-0003-secure-transport.md#adr-0003安全传输) |
| `ADR-0004` | 人工挑战模型 | [docs/architecture/decisions/ADR-0004-challenge-model.md](architecture/decisions/ADR-0004-challenge-model.md#adr-0004人工挑战模型) |
| `ADR-0005` | 文档权威边界 | [docs/architecture/decisions/ADR-0005-docs-authority.md](architecture/decisions/ADR-0005-docs-authority.md#adr-0005文档权威边界) |
| `ADR-0006` | 配置迁移采用显式凭据决策与可恢复事务 | [docs/architecture/decisions/ADR-0006-transactional-config-migration.md](architecture/decisions/ADR-0006-transactional-config-migration.md#adr-0006配置迁移采用显式凭据决策与可恢复事务) |
| `ADR-0007` | 一次构建的不可变候选原样晋升 | [docs/architecture/decisions/ADR-0007-immutable-candidate-promotion.md](architecture/decisions/ADR-0007-immutable-candidate-promotion.md#adr-0007一次构建的不可变候选原样晋升) |
| `ADR-0008` | 离线安装与事务激活共用验证链 | [docs/architecture/decisions/ADR-0008-offline-transactional-installer.md](architecture/decisions/ADR-0008-offline-transactional-installer.md#adr-0008离线安装与事务激活共用验证链) |
| `ADR-0009` | 真实认证采用分离的管理面与测试面 | [docs/architecture/decisions/ADR-0009-separated-live-test-plane.md](architecture/decisions/ADR-0009-separated-live-test-plane.md#adr-0009真实认证采用分离的管理面与测试面) |
| `ADR-0010` | macOS 固定系统别名作为受信任路径锚点 | [docs/architecture/decisions/ADR-0010-macos-trusted-system-path-alias.md](architecture/decisions/ADR-0010-macos-trusted-system-path-alias.md#adr-0010macos-固定系统别名作为受信任路径锚点) |
| `ARCH-BOUNDARY-001` | 产品边界 | [docs/architecture/overview.md](architecture/overview.md#arch-boundary-001产品边界) |
| `ARCH-CONCURRENCY-001` | 生命周期 | [docs/architecture/overview.md](architecture/overview.md#arch-concurrency-001生命周期) |
| `ARCH-MODE-001` | 模式选择 | [docs/architecture/overview.md](architecture/overview.md#arch-mode-001模式选择) |
| `ARCH-SDK-001` | 职责划分 | [docs/architecture/overview.md](architecture/overview.md#arch-sdk-001职责划分) |
| `AUTH-CHALLENGE-001` | 统一挑战结果 | [docs/compatibility/auth-capabilities.md](compatibility/auth-capabilities.md#auth-challenge-001统一挑战结果) |
| `AUTH-CONFLICT-001` | 异账号冲突与显式切换 | [docs/compatibility/auth-capabilities.md](compatibility/auth-capabilities.md#auth-conflict-001异账号冲突与显式切换) |
| `AUTH-DEVICE-001` | 设备验证与信任设备 | [docs/compatibility/auth-capabilities.md](compatibility/auth-capabilities.md#auth-device-001设备验证与信任设备) |
| `AUTH-PASSWORD-001` | CAS 用户名密码 | [docs/compatibility/auth-capabilities.md](compatibility/auth-capabilities.md#auth-password-001cas-用户名密码) |
| `AUTH-QR-001` | Terminal QR | [docs/compatibility/auth-capabilities.md](compatibility/auth-capabilities.md#auth-qr-001terminal-qr) |
| `AUTH-QR-002` | Terminal QR 契约 | [docs/compatibility/auth-capabilities.md](compatibility/auth-capabilities.md#auth-qr-002terminal-qr-契约) |
| `AUTH-SMS-001` | 手机验证码 | [docs/compatibility/auth-capabilities.md](compatibility/auth-capabilities.md#auth-sms-001手机验证码) |
| `AUTH-SMS-002` | 不可实测分支 | [docs/compatibility/auth-capabilities.md](compatibility/auth-capabilities.md#auth-sms-002不可实测分支) |
| `AUTH-STATUS-001` | 能力状态词汇 | [docs/compatibility/auth-capabilities.md](compatibility/auth-capabilities.md#auth-status-001能力状态词汇) |
| `AUTH-UNKNOWN-001` | 未识别挑战 | [docs/compatibility/auth-capabilities.md](compatibility/auth-capabilities.md#auth-unknown-001未识别挑战) |
| `CLI-AUTH-001` | 登录与账号切换 | [docs/reference/cli.md](reference/cli.md#cli-auth-001登录与账号切换) |
| `CLI-BINARY-001` | 三个入口 | [docs/reference/cli.md](reference/cli.md#cli-binary-001三个入口) |
| `CLI-EXIT-001` | 稳定退出码 | [docs/reference/cli.md](reference/cli.md#cli-exit-001稳定退出码) |
| `CLI-PROFILE-001` | Profile | [docs/reference/cli.md](reference/cli.md#cli-profile-001profile) |
| `CLI-STREAM-001` | 输出通道 | [docs/reference/cli.md](reference/cli.md#cli-stream-001输出通道) |
| `CLI-TREE-001` | 现代命令树 | [docs/reference/cli.md](reference/cli.md#cli-tree-001现代命令树) |
| `EVID-AUTH-001` | 认证证据字段 | [docs/evidence/README.md](evidence/README.md#evid-auth-001认证证据字段) |
| `EVID-BUNDLE-001` | 私有证据 bundle | [docs/evidence/README.md](evidence/README.md#evid-bundle-001私有证据-bundle) |
| `EVID-CAPTURE-001` | 源端捕获与持久性 | [docs/evidence/README.md](evidence/README.md#evid-capture-001源端捕获与持久性) |
| `EVID-GATE-001` | 证据门禁 | [docs/evidence/README.md](evidence/README.md#evid-gate-001证据门禁) |
| `EVID-HISTORY-DRYRUN-001` | 历史重写隔离演练 | [docs/evidence/2026-08-27-history-rewrite-dry-run.md](evidence/2026-08-27-history-rewrite-dry-run.md#evid-history-dryrun-001历史重写隔离演练) |
| `EVID-REDACT-001` | 绝对禁止内容 | [docs/evidence/README.md](evidence/README.md#evid-redact-001绝对禁止内容) |
| `EVID-REVIEW-001` | 人工复核与入库 | [docs/evidence/README.md](evidence/README.md#evid-review-001人工复核与入库) |
| `EVID-SECURITY-001` | 安全事件证据 | [docs/evidence/README.md](evidence/README.md#evid-security-001安全事件证据) |
| `JSON-COMPAT-001` | 演进规则 | [docs/reference/json-cli.md](reference/json-cli.md#json-compat-001演进规则) |
| `JSON-ENVELOPE-001` | 唯一顶层结构 | [docs/reference/json-cli.md](reference/json-cli.md#json-envelope-001唯一顶层结构) |
| `JSON-ERROR-001` | 错误对象 | [docs/reference/json-cli.md](reference/json-cli.md#json-error-001错误对象) |
| `JSON-STATUS-001` | 状态示例 | [docs/reference/json-cli.md](reference/json-cli.md#json-status-001状态示例) |
| `JSON-VALUE-001` | 值编码 | [docs/reference/json-cli.md](reference/json-cli.md#json-value-001值编码) |
| `MIG-BILL-001` | 历史用量、账单与充值 | [docs/upgrade/migration-matrix.md](upgrade/migration-matrix.md#mig-bill-001历史用量账单与充值) |
| `MIG-CMD-001` | 无参数旧版登录 | [docs/upgrade/migration-matrix.md](upgrade/migration-matrix.md#mig-cmd-001无参数旧版登录) |
| `MIG-CMD-002` | 核心登录命令 | [docs/upgrade/migration-matrix.md](upgrade/migration-matrix.md#mig-cmd-002核心登录命令) |
| `MIG-CONFIG-001` | 目标布局 | [docs/operations/config-migration.md](operations/config-migration.md#mig-config-001目标布局) |
| `MIG-CONFIG-002` | 当前 Meta YAML 配置 | [docs/upgrade/migration-matrix.md](upgrade/migration-matrix.md#mig-config-002当前-meta-yaml-配置) |
| `MIG-CRED-001` | 旧凭据存储与参数密码 | [docs/upgrade/migration-matrix.md](upgrade/migration-matrix.md#mig-cred-001旧凭据存储与参数密码) |
| `MIG-CREDENTIAL-001` | 旧密码处理 | [docs/operations/config-migration.md](operations/config-migration.md#mig-credential-001旧密码处理) |
| `MIG-FILE-001` | 写入与权限 | [docs/operations/config-migration.md](operations/config-migration.md#mig-file-001写入与权限) |
| `MIG-IDEMPOTENT-001` | Marker 与重跑 | [docs/operations/config-migration.md](operations/config-migration.md#mig-idempotent-001marker-与重跑) |
| `MIG-MODE-001` | 三入口模式切换 | [docs/upgrade/migration-matrix.md](upgrade/migration-matrix.md#mig-mode-001三入口模式切换) |
| `MIG-NET-001` | 接口绑定与网络扫描 | [docs/upgrade/migration-matrix.md](upgrade/migration-matrix.md#mig-net-001接口绑定与网络扫描) |
| `MIG-PREVIEW-001` | 迁移流程 | [docs/operations/config-migration.md](operations/config-migration.md#mig-preview-001迁移流程) |
| `MIG-SESSION-001` | 在线会话管理 | [docs/upgrade/migration-matrix.md](upgrade/migration-matrix.md#mig-session-001在线会话管理) |
| `MIG-SOURCE-001` | 支持的来源 | [docs/operations/config-migration.md](operations/config-migration.md#mig-source-001支持的来源) |
| `MIG-TEST-001` | 验收 | [docs/operations/config-migration.md](operations/config-migration.md#mig-test-001验收) |
| `MIG-TRANSACTION-001` | Journal、提交与回滚 | [docs/operations/config-migration.md](operations/config-migration.md#mig-transaction-001journal提交与回滚) |
| `MIG-UPDATE-001` | 不安全自更新 | [docs/upgrade/migration-matrix.md](upgrade/migration-matrix.md#mig-update-001不安全自更新) |
| `MIG-USAGE-001` | 套餐与当前用量 | [docs/upgrade/migration-matrix.md](upgrade/migration-matrix.md#mig-usage-001套餐与当前用量) |
| `PROTO-DISCOVERY-001` | 发现优先 | [docs/architecture/protocol-correctness.md](architecture/protocol-correctness.md#proto-discovery-001发现优先) |
| `PROTO-IO-001` | 网络边界 | [docs/architecture/protocol-correctness.md](architecture/protocol-correctness.md#proto-io-001网络边界) |
| `PROTO-LOGIN-001` | 登录成功不变量 | [docs/architecture/protocol-correctness.md](architecture/protocol-correctness.md#proto-login-001登录成功不变量) |
| `PROTO-LOGOUT-001` | 幂等注销 | [docs/architecture/protocol-correctness.md](architecture/protocol-correctness.md#proto-logout-001幂等注销) |
| `PROTO-REDIRECT-001` | CAS ticket 截获 | [docs/architecture/protocol-correctness.md](architecture/protocol-correctness.md#proto-redirect-001cas-ticket-截获) |
| `PROTO-TRANSPORT-001` | HTTPS-only | [docs/architecture/protocol-correctness.md](architecture/protocol-correctness.md#proto-transport-001https-only) |
| `REL-APPROVAL-001` | 高风险动作逐项批准 | [docs/operations/release.md](operations/release.md#rel-approval-001高风险动作逐项批准) |
| `REL-ATTEST-001` | Candidate manifest 与 provenance | [docs/operations/release.md](operations/release.md#rel-attest-001candidate-manifest-与-provenance) |
| `REL-BUNDLE-001` | 原子发布包 | [docs/operations/release.md](operations/release.md#rel-bundle-001原子发布包) |
| `REL-CANDIDATE-001` | 候选与发布顺序 | [docs/operations/release.md](operations/release.md#rel-candidate-001候选与发布顺序) |
| `REL-CI-001` | 自动化门禁 | [docs/operations/release.md](operations/release.md#rel-ci-001自动化门禁) |
| `REL-INSTALL-001` | 离线 acquisition | [docs/operations/offline-install.md](operations/offline-install.md#rel-install-001离线-acquisition) |
| `REL-INSTALL-002` | 归档、路径与权限 | [docs/operations/offline-install.md](operations/offline-install.md#rel-install-002归档路径与权限) |
| `REL-INSTALL-003` | 事务、回滚与失败注入 | [docs/operations/offline-install.md](operations/offline-install.md#rel-install-003事务回滚与失败注入) |
| `REL-LAB-001` | 分离拓扑 | [docs/runbooks/campus-lab.md](runbooks/campus-lab.md#rel-lab-001分离拓扑) |
| `REL-LAB-002` | 发现与供应 | [docs/runbooks/campus-lab.md](runbooks/campus-lab.md#rel-lab-002发现与供应) |
| `REL-LAB-003` | 单次实验窗口 | [docs/runbooks/campus-lab.md](runbooks/campus-lab.md#rel-lab-003单次实验窗口) |
| `REL-LIVE-001` | 真实校园网门禁 | [docs/operations/release.md](operations/release.md#rel-live-001真实校园网门禁) |
| `REL-LIVE-MATRIX-001` | v1 真实网络矩阵 | [docs/operations/live-validation.md](operations/live-validation.md#rel-live-matrix-001v1-真实网络矩阵) |
| `REL-LIVEGATE-001` | Runner 接口与信任边界 | [docs/operations/live-validation.md](operations/live-validation.md#rel-livegate-001runner-接口与信任边界) |
| `REL-LIVEGATE-002` | Suite 状态机与清理权 | [docs/operations/live-validation.md](operations/live-validation.md#rel-livegate-002suite-状态机与清理权) |
| `REL-LIVEGATE-003` | 候选绑定与结果输出 | [docs/operations/live-validation.md](operations/live-validation.md#rel-livegate-003候选绑定与结果输出) |
| `REL-M0-001` | 紧急安全门禁 | [docs/operations/release.md](operations/release.md#rel-m0-001紧急安全门禁) |
| `REL-PROMOTION-001` | Promotion lock 与原样发布 | [docs/operations/release.md](operations/release.md#rel-promotion-001promotion-lock-与原样发布) |
| `REL-TRANSFER-001` | 候选下载与远端传输 | [docs/operations/live-validation.md](operations/live-validation.md#rel-transfer-001候选下载与远端传输) |
| `REL-UPDATE-001` | 未来恢复自更新的前置条件 | [docs/operations/release.md](operations/release.md#rel-update-001未来恢复自更新的前置条件) |
| `REL-WINDOW-001` | 短执行窗口 | [docs/operations/release.md](operations/release.md#rel-window-001短执行窗口) |
| `SDK-API-001` | 公开 façade | [docs/reference/go-sdk.md](reference/go-sdk.md#sdk-api-001公开-façade) |
| `SDK-CACHE-001` | 协议状态存储 | [docs/reference/go-sdk.md](reference/go-sdk.md#sdk-cache-001协议状态存储) |
| `SDK-CONCURRENCY-001` | 并发与边界 | [docs/reference/go-sdk.md](reference/go-sdk.md#sdk-concurrency-001并发与边界) |
| `SDK-ERROR-001` | 错误契约 | [docs/reference/go-sdk.md](reference/go-sdk.md#sdk-error-001错误契约) |
| `SDK-INTERACTION-001` | 人工挑战 | [docs/reference/go-sdk.md](reference/go-sdk.md#sdk-interaction-001人工挑战) |
| `SDK-LOGIN-001` | 认证输入 | [docs/reference/go-sdk.md](reference/go-sdk.md#sdk-login-001认证输入) |
| `SDK-OBSERVE-001` | 可观测性 | [docs/reference/go-sdk.md](reference/go-sdk.md#sdk-observe-001可观测性) |
| `SDK-STATUS-001` | 状态模型 | [docs/reference/go-sdk.md](reference/go-sdk.md#sdk-status-001状态模型) |
| `SEC-HISTORY-001` | 历史泄露响应 | [docs/architecture/security.md](architecture/security.md#sec-history-001历史泄露响应) |
| `SEC-LOG-001` | 最小可观测性 | [docs/architecture/security.md](architecture/security.md#sec-log-001最小可观测性) |
| `SEC-REPORT-001` | 安全失败语义 | [docs/architecture/security.md](architecture/security.md#sec-report-001安全失败语义) |
| `SEC-SECRET-001` | 秘密定义与禁止落盘 | [docs/architecture/security.md](architecture/security.md#sec-secret-001秘密定义与禁止落盘) |
| `SEC-TRANSPORT-001` | 秘密不经 HTTP | [docs/architecture/security.md](architecture/security.md#sec-transport-001秘密不经-http) |
| `SEC-UPDATE-001` | 自更新冻结 | [docs/architecture/security.md](architecture/security.md#sec-update-001自更新冻结) |
