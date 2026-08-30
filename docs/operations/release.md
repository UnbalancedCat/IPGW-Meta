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

### Full candidate-manifest v1

`candidate-manifest.json` 的 full validator 是 closed-world。顶层按以下顺序精确包含 14 个字段：`schema_version`、`plan_id`、`revision`、`version`、`candidate_id`、`source_commit`、`source_tree`、`workflow_run_id`、`workflow_run_attempt`、`toolchain`、`build_input_sha256`、`release_assets`、`test_tools`、`live_gate_targets`。未知、缺失或重复字段均拒绝；任意嵌套层的 duplicate key、无效 UTF-8、第二个 JSON value、非空白尾随字节和超过 64 KiB 均拒绝。生成器只输出按上述字段顺序编码的紧凑 JSON，末尾恰好一个 LF；full validator 必须拒绝不等于该 canonical 编码的字节。`candidate_set_sha256` 是这些精确字节的外部派生值，不得写回 manifest 形成自引用。

`schema_version` 固定为 `1`；plan/revision/version 固定为 `IPGW-META-V1`、`2026-08-28-r2`、`v1.0.0`。`source_commit`、`source_tree` 是 40 位小写十六进制；tree 必须等于 source commit 的 Git tree。run ID 与 attempt 是无前导零的正 JSON 整数，并与 candidate ID 中的值完全相同。

`toolchain` 精确包含 `go_version`、`go_toolchain`、`host_platform`、`cgo_enabled`、`goamd64`、`goarm64`、`source_date_epoch`、`build_recipe`，依次固定为当前 `go.mod` 的 `go` 版本带 `go` 前缀、`local`、`linux-amd64`、`false`、`v1`、`v8.0`、source commit 的正 Unix committer timestamp 和 `candidate-v1`。`candidate-v1` 使用 `CGO_ENABLED=0`，六个固定 GOOS/GOARCH，产品入口使用 `-trimpath -buildvcs=false -ldflags "-s -w -X main.version=v1.0.0"`，helper 使用相同 build flags 和 `-ldflags "-s -w"`。十八个产品输出和两个 helper 各构建一次；归档、原生测试、attestation 和 promotion 只能消费这些冻结输出，不得再次 build。

`build_input_sha256` 覆盖 source commit 中除 promotion 白名单外的全部 tracked regular file。白名单精确为 `docs/evidence/releases/v1.0.0/**`、`docs/upgrade/status.md`、`docs/compatibility/auth-capabilities.md`；不得扩大到整个 `docs/evidence/**`。只接受 Git mode `100644`、`100755`，拒绝 symlink、submodule、特殊 mode 和无效 UTF-8 path；path 的 UTF-8 字节长度必须在 1..4096。path 按原始 UTF-8 字节升序，每项编码为 `<path>NUL<mode>NUL<decimal-size>NUL<content-sha256>LF`，连接后计算 SHA-256。tree listing 与 blob 必须限界、流式处理，不得在校验大小前无界载入内存。版本与 toolchain 由同一 manifest 的独立字段绑定；任何非白名单 tracked path 的增加、删除、改名、mode 或内容变化都必须改变 build-input digest。

`release_assets` 和 `test_tools` 的成员精确为 `name`、`platform`、`size`、`sha256`。name 是 ASCII POSIX 相对路径；拒绝绝对路径、反斜线、`.`/`..` segment、控制字符、重复或大小写折叠冲突。size 是正整数，SHA 是 64 位小写十六进制，数组按 name 的原始字节升序。platform 只允许六个构建 target 以及 `unix`、`windows`、`all`。

公开数组精确包含十项：六个 `release/ipgw-meta-<target>` bundle、`release/install.sh`、`release/install.ps1`、`release/release-manifest.json`、`release/SHA256SUMS`。私有数组精确包含 `test-tools/ipgw-live-gate-linux-amd64` 和 `test-tools/ipgw-live-gate-windows-amd64.exe`；这里的“私有”表示 maintainer-only、不得发布，不表示 GitHub Actions artifact 是秘密存储。artifact 物理树只能再包含根 `candidate-manifest.json` 与根 `SHA256SUMS`，共 14 个普通文件，不得出现 symlink、hardlink、special file 或额外成员。

`live_gate_targets` 保持 [`REL-LIVEGATE-001`](live-validation.md#rel-livegate-001runner-接口与信任边界) 的固定顺序。Linux target 的 size/hash 必须同时等于 `release/ipgw-meta-linux-amd64.tar.gz` 内 `ipgw-meta` 的实际字节与 inner `bundle-manifest.json` entry；Windows target 对应 Windows amd64 ZIP 内 `ipgw-meta.exe`。传输时从这两个固定 archive member 提取产品 candidate，不增加第二份产品构建；同平台 helper 必须与 `test_tools` 记录一致。

### Release manifest、归档与 checksums

`release/release-manifest.json` 也是 closed-world canonical JSON，字段顺序精确为 `schema_version`、`plan_id`、`revision`、`version`、`candidate_id`、`source_commit`、`source_tree`、`build_input_sha256`、`release_sha256sums_sha256`、`assets`。`assets` 只列六个 bundle 和两个 installer，使用 basename、上述 platform/size/hash 结构并按 name 排序；不列 manifest 自身或 checksums。`release_sha256sums_sha256` 绑定下述 8 行 checksum 文件。

`release/SHA256SUMS` 必须继续精确覆盖六个 bundle 和两个 installer；这是在线安装器的 closed allowlist，不得加入 release manifest。根 `SHA256SUMS` 精确覆盖 candidate manifest、十个公开资产和两个 helper，不自哈希。两者均按 POSIX 相对名称原始字节升序，以 `<64-lowerhex><two-spaces><name><LF>` 编码。

归档由仓库内 Go packager 生成，不依赖宿主 `tar`、`gzip` 或 `zip`。tar 固定 USTAR、member 顺序、uid/gid 0、用户名/组名空、mode、无扩展 header，gzip 固定 deflate、空 name/comment、UTC 与 timestamp；tar/gzip member mtime 精确使用 `source_date_epoch`。ZIP 固定 member 顺序、creator/mode、deflate method、空 extra/comment，并将 `source_date_epoch` 向下截断到 ZIP UTC DOS timestamp 的固定 2 秒精度。binary mode 为 `0755`，公开 metadata 为 `0644`，文本为 LF。相同 source/version/toolchain/build-input 必须生成逐字节相同的八个公开 payload；不同 run/attempt 只允许 candidate/release manifest、根 checksum 与 artifact identity 相应变化。

### Artifact 与 attestation

artifact name 固定为 `candidate-set-<candidate_id>`，upload 必须 `overwrite: false`、不做额外压缩，并只上传上述精确树。上传后记录的 artifact ID 必须为正整数，digest 规范化为 `sha256:<64-lowerhex>`；还必须通过 GitHub API 回读并匹配 repository、workflow、run ID、attempt、name、digest 与 `expired=false`。这些上传后值只进入 workflow output/summary 和后续 promotion lock，不得修改或二次上传 candidate manifest。

provenance 精确覆盖十一个 subject：candidate-set subject 使用 artifact 的固定 name 与服务端 digest；十个公开 release asset 使用最终公开 basename 与其文件 digest。不得把 `test_tools` 或根 checksum 当作公开 subject。验证必须同时约束 repository、workflow、source commit、run ID/attempt；只匹配 digest 不足以通过。attestation 前必须按精确 artifact ID 重新下载并完成 full manifest、两级 checksum、所有成员与 runner projection 验证；任一失败不得产生可接受候选。

## REL-PROMOTION-001：Promotion lock 与原样发布

promotion lock 位于 `docs/evidence/releases/v1.0.0/promotion-lock.json`，绑定 version、candidate/source、workflow/artifact、candidate-set/release-manifest/build-input digest、attestation subjects 和已审阅 evidence IDs。candidate source 与最终 tag 之间只允许修改 `docs/evidence/releases/v1.0.0/**`、`docs/upgrade/status.md`、`docs/compatibility/auth-capabilities.md`；其他变化使候选失效。

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
