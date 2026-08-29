---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
---

# 发布操作手册

v1 按门禁而不是日期发布。M0 未完成时禁止创建或发布新版本。

## REL-WINDOW-001：短执行窗口

- 每个执行窗口只处理一个工作包，最多 30–45 分钟；开始时声明修改范围与停止条件，最后约 10 分钟用于验证和交接。
- 不在窗口末启动新的高风险动作。超时测试停止或转为外部 CI，只记录 workflow/run ID，下一窗口再读取结果。
- 结束时不得存在后台登录/QR 轮询、未知活动校园网会话、半完成 ref 更新、未处理安装事务或未说明的敏感临时文件。
- 窗口结束只更新正式里程碑状态和最小 Agent handoff；不得把未完成工作写成 complete。

## REL-APPROVAL-001：高风险动作逐项批准

以下动作必须各自在对应窗口重新取得维护者明确批准，不能由计划批准或前一窗口批准自动继承：

- 读取或使用真实凭据、扫描二维码、操作校园网会话；
- 创建敏感历史备份、重写本地历史、强制更新远端 refs；
- 仓库小写改名、启用或改变 main/tag 保护规则；
- 创建最终签名 tag、公开 release；
- 删除旧工作区、敏感备份或其他恢复材料。

执行前必须解析精确目标并完成只读核验；执行后必须验证最终状态。审批只授权列明的动作和目标，不扩大为任意 GitHub、网络或文件系统操作。

## REL-M0-001：紧急安全门禁

发布负责人必须确认：

- 已通过正常会话管理使泄露 session 失效；
- 真实测试文件已移除并由完全合成 fixture 替代；
- 历史清理按 [`SEC-HISTORY-001`](../architecture/security.md)完成，所有 refs 和相关 tags 已复扫；
- 远端强制更新及协作者重新克隆已协调完成；
- secret scan 无有效发现，日志/JSON/Observer 泄漏测试通过；
- 未签名自更新及其发布入口已禁用。

Git 历史重写必须独立执行、创建受控备份、解析精确 refs 并经维护者批准；普通 release job 不自动重写历史。

## REL-CI-001：自动化门禁

每个 PR 和 release candidate 必须通过：

```text
go test ./...
go test -race ./...
go vet ./...
secret scan
doccheck --check
```

并交叉构建 Windows、Linux、macOS 的 amd64/arm64。协议测试必须使用本地 `httptest` 与合成 fixture，覆盖动态 CAS、JSON/JSONP、响应上限、恶意 redirect、ticket HTTP 下一跳从未发送、TLS 无降级、最终身份检查、账号冲突、注销失败、人工挑战、context 取消和全部 CLI/JSON 退出语义。

## REL-BUNDLE-001：原子发布包

每个支持平台的 artifact 必须同时包含兼容版本一致的：

- `ipgw-legacy`；
- `ipgw-meta`；
- `ipgw` dispatcher；
- 安装/卸载元数据、license、checksums 和模式默认元数据。

安装器先将完整 bundle 写入临时版本目录，校验后原子切换 active 指针；失败回滚到上一完整 bundle。禁止逐个覆盖正在使用的三个二进制。旧安装的 launcher 选择保持 legacy；只有没有既有状态的新安装使用 meta 默认。

## REL-LIVE-001：真实校园网门禁

自动化通过后，由维护者在候选二进制上执行：

- NAS Ubuntu campus wired：password-core、same-account、幂等 logout 和 Terminal QR；
- BHK Windows campus wired：password-core 和 Windows 原生离线安装；
- BHK Windows campus Wi-Fi：password-core、bind IP、network list/scan；
- macOS 只要求原生安装和 CLI smoke，release notes 必须明确没有校园网认证证据。

完整状态机、候选传输和矩阵由 [`REL-LIVEGATE-001`](live-validation.md#rel-livegate-001runner-接口与信任边界) 与 [`REL-LIVE-MATRIX-001`](live-validation.md#rel-live-matrix-001v1-真实网络矩阵) 定义。只有一个授权账号，异账号 conflict/switch 只保留合成覆盖且不阻塞 v1。结果只按 [`EVID-BUNDLE-001`](../evidence/README.md#evid-bundle-001私有证据-bundle) 落盘；手机验证码保持 `observed_anonymous + detected_only`，不得写入“支持”列表。

## REL-CANDIDATE-001：候选与发布顺序

1. workflow 输入 `release_version: v1.0.0` 和完整 40 位 `source_commit`；SHA 必须等于受保护 `main` 当前 tip，M0–M2 门禁通过且最终 tag 不存在。
2. 六个平台、三入口与两个私有 live-gate helper 各构建一次，生成确定性 candidate-set；不为测试、tag 或发布重新编译、重压缩。
3. 完成 artifact/attestation、六平台原生安装和同一 candidate-set 的真实网络矩阵。
4. 人工复核 evidence，更新能力矩阵和正式状态，提交 promotion lock 与 release notes。
5. 维护者创建指向 promotion commit 的 SSH 签名 annotated tag；promotion workflow 原样晋升候选。
6. 逐资产重新下载验证通过后公开 release；失败时保留不可见 draft，不单独替换或覆盖任何入口。

release notes 必须列出 legacy/meta 默认策略、配置迁移方式、Terminal QR 证据状态、OTP detected-only 限制、自更新禁用和任何未完成平台验证。

## REL-ATTEST-001：Candidate manifest 与 provenance

Candidate ID 为 `v1.0.0-<SOURCE_SHA12>-<RUN_ID>.<RUN_ATTEMPT>`。单一 GitHub artifact 的根 manifest 必须分列公开 `release_assets` 和私有 `test_tools`，并绑定 plan/revision、version、candidate ID、source commit/tree、Go/toolchain、build-input digest 及每个文件的名称、平台、大小和 SHA-256。

完整 candidate manifest 必须包含 [`REL-LIVEGATE-001`](live-validation.md#rel-livegate-001runner-接口与信任边界) 冻结的 candidate-manifest v1 runner projection。`candidate_set_sha256` 即 candidate manifest 精确原始字节的 SHA-256 小写十六进制；full schema 的其余字段与生成规则由 `WP-M3-CANDIDATE` 冻结和生成，任何新增字段都不得改变 runner projection 的字段类型、语义、target 顺序或 binary 绑定。

workflow 记录 artifact ID、artifact digest、run ID/attempt，并为 candidate-set 和每个公开 release asset 生成 GitHub provenance attestation。本地只读副本不能替代 GitHub artifact；artifact 不可用、attestation 无法验证、hash 不符或构建输入变化时，候选失效，所有真实测试必须对新候选重做。

## REL-PROMOTION-001：Promotion lock 与原样发布

promotion lock 位于 `docs/evidence/releases/v1.0.0/promotion-lock.json`，绑定 version、candidate/source、workflow/artifact、candidate-set/release-manifest/build-input digest、attestation subjects 和已审阅 evidence IDs。candidate source 与最终 tag 之间只允许修改 `docs/evidence/**`、`docs/upgrade/status.md`、`docs/compatibility/auth-capabilities.md`；其他变化使候选失效。

最终 tag 必须是维护者用专用 Ed25519 key 创建、指向 promotion commit 的 SSH 签名 annotated `v1.0.0`。私钥只存在本地 Windows 用户环境；公钥登记为 GitHub signing key。promotion job 必须通过 GitHub API 验证 tag signature valid/verified，并且：

1. 按 lock 中精确 artifact ID 下载 candidate-set，验证 artifact digest、集合 hash、manifest、全部资产与 attestation。
2. 禁止 setup-go、build、package、strip、sign 或重新压缩。
3. 创建不可见 draft，不使用 clobber，上传六个 bundle、两个 installer、release manifest 和 SHA256SUMS。
4. 重新下载每个资产，核对名称、大小和 SHA-256；全部一致后才公开。
5. 任一步失败保持 draft 不可见；不得公开半包、替换单资产或从其他 run 拼装发布。

## REL-UPDATE-001：未来恢复自更新的前置条件

自更新只有在另一个 ADR 接受并满足以下条件后才能恢复：

- 离线可验证的签名 manifest 与明确的信任根/轮换策略；
- 每个平台 artifact 的加密校验和、严格下载大小上限和 Content-Type 校验；
- 下载到受限临时文件，签名与 hash 全部验证后才进入版本目录；
- 三入口 bundle 原子切换和失败自动回滚；
- 防止版本回退、路径穿越、符号链接替换和跨卷伪原子操作；
- Windows/Linux/macOS 安装、并发更新、断电/中断恢复测试。

在这些条件全部满足前，CLI 只提示前往官方 release 页面，不下载或替换自身。
