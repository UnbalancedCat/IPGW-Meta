---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
---

# 脱敏证据规范

`docs/evidence/` 只保存最小、脱敏、可审计的人工验收或安全事件记录。自动化测试结果由 CI 保存，不在此复制完整日志；安全事件记录只能保存工具版本、数量、规则 ID、无秘密路径、ref/hash 结果和明确的未完成项。

## EVID-REDACT-001：绝对禁止内容

禁止提交：

- 用户名、学号、密码、手机号、验证码；
- Cookie、CAS ticket、TGT、LT、QR payload、session ID、CSRF token；
- 原始认证请求/响应、HTML、header、form body；
- 带 query/fragment/userinfo 的认证 URL；
- 完整公网/内网 IP、网卡 MAC、设备指纹；
- 可能恢复上述信息的哈希、Base64、截图或压缩包。

脱敏不是把秘密编码成 Base64，也不是只遮盖部分 token。无法确认安全的材料不得进入仓库。

## EVID-BUNDLE-001：私有证据 bundle

每次 live-gate 运行只允许在 `build/live-evidence/<candidate-id>/<evidence-id>/` 创建：

```text
evidence.json
summary.md
SHA256SUMS
```

目录和文件权限分别为 Unix `0700/0600` 或 Windows 当前用户私有 ACL。bundle 是本地私有审核材料，不得整包提交到仓库；仓库只接收 [`EVID-REVIEW-001`](#evid-review-001人工复核与入库) 允许的固定摘要和 bundle hash。

- 编码后的 `evidence.json` 必须是有效 UTF-8，大小不得超过 64 KiB（65536 字节）。
- 顶层对象必须恰好包含下方样例的 18 个字段，每个 step 对象必须恰好包含样例的 5 个字段；所有字段都必需且只能出现一次。
- 解析必须拒绝任意层级的未知字段、缺失字段或重复 object key，拒绝无效 UTF-8，并拒绝完整顶层对象之后的另一个 JSON 值或任意非空白尾随字节；JSON grammar 允许尾随空白，canonical runner 编码必须恰好以一个 LF 结尾。
- schema validator 只返回固定且不回显输入的错误；不得把原始值、JSON 片段、profile、路径或产品输出拼入错误。

`summary.md` 必须只从已验证的 `evidence.json` 按以下精确模板生成：

```text
# IPGW-Meta live-gate summary

- schema_version: <schema_version>
- plan_id: <plan_id>
- revision: <revision>
- evidence_id: <evidence_id>
- candidate_id: <candidate_id>
- candidate_set_sha256: <candidate_set_sha256>
- source_commit: <source_commit>
- platform: <platform>
- testbed: <testbed>
- network_type: <network_type>
- auth_method: <auth_method>
- suite: <suite>
- capability_before: <capability_before-comma-joined>
- result: <result>
- capability_after: <capability_after-comma-joined>
- started_at: <started_at>
- finished_at: <finished_at>

## Steps

- <name> | <result> | <exit_code> | <error_code-or-null> | <duration_seconds>
```

`<...>` 表示对应 schema 值而不是自由文本：整数使用十进制，字符串使用已验证的原值且不加 JSON 引号，capability 数组按原顺序以逗号连接且逗号后无空格；每个 step 按 `steps` 顺序生成一行，空 error code 写字面量 `null`。

模板所有换行都必须是 LF，不得有 CRLF 或行尾空格；文件末尾恰好一个 LF，不得增加其他标题、注释或空行。

`SHA256SUMS` 必须恰好为以下两行固定顺序：

```text
<evidence-json-sha256-lowerhex>  evidence.json
<summary-md-sha256-lowerhex>  summary.md
```

每行在 hash 与文件名之间恰好两个 ASCII 空格并以 LF 结束，文件末尾恰好一个 LF；两个 hash 分别覆盖 `evidence.json` 与 `summary.md` 的精确字节，`SHA256SUMS` 不得自哈希。

## EVID-AUTH-001：认证证据字段

`evidence.json` 使用 schema version 1，只允许以下结构：

```json
{
  "schema_version": 1,
  "plan_id": "IPGW-META-V1",
  "revision": "2026-08-28-r2",
  "evidence_id": "EVID-YYYYMMDD-NNN",
  "candidate_id": "v1.0.0-0123456789ab-12345.1",
  "candidate_set_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "source_commit": "0123456789abcdef0123456789abcdef01234567",
  "platform": "linux-amd64",
  "testbed": "nas_vm",
  "network_type": "campus_wired",
  "auth_method": "password",
  "suite": "password_core",
  "capability_before": ["synthetic_covered", "live_unverified"],
  "result": "pass",
  "capability_after": ["synthetic_covered", "live_verified"],
  "started_at": "2026-08-29T01:02:03Z",
  "finished_at": "2026-08-29T01:02:10Z",
  "steps": [
    {
      "name": "initial_status_offline",
      "result": "pass",
      "exit_code": 0,
      "error_code": null,
      "duration_seconds": 1
    },
    {
      "name": "login_logged_in",
      "result": "pass",
      "exit_code": 0,
      "error_code": null,
      "duration_seconds": 1
    },
    {
      "name": "status_online",
      "result": "pass",
      "exit_code": 0,
      "error_code": null,
      "duration_seconds": 1
    },
    {
      "name": "second_login_already_online",
      "result": "pass",
      "exit_code": 0,
      "error_code": null,
      "duration_seconds": 1
    },
    {
      "name": "logout_logged_out",
      "result": "pass",
      "exit_code": 0,
      "error_code": null,
      "duration_seconds": 1
    },
    {
      "name": "final_status_offline",
      "result": "pass",
      "exit_code": 0,
      "error_code": null,
      "duration_seconds": 1
    },
    {
      "name": "second_logout_already_offline",
      "result": "pass",
      "exit_code": 0,
      "error_code": null,
      "duration_seconds": 1
    }
  ]
}
```

`schema_version`、`plan_id`、`revision` 必须分别精确为 `1`、`IPGW-META-V1`、`2026-08-28-r2`。

`evidence_id` 必须为 `EVID-YYYYMMDD-NNN`：日期必须是有效公历日期且等于 `started_at` 的 UTC 日期，序号为 `001` 至 `999`。`candidate_id` 必须为 `v1.0.0-<source-sha12>-<run-id>.<attempt>`：SHA 前缀是 `source_commit` 的前 12 位，run ID 与 attempt 均为无前导零的正十进制整数。`candidate_set_sha256` 必须是 64 位小写十六进制，`source_commit` 必须是 40 位小写十六进制。

`started_at` 与 `finished_at` 必须是使用 `Z` 的 canonical UTC RFC3339Nano，且 `finished_at` 不早于 `started_at`。

`platform`、`testbed`、`network_type`、`auth_method`、`suite`、总体 `result` 与 step `result` 必须使用 [`REL-LIVEGATE-001`](../operations/live-validation.md#rel-livegate-001runner-接口与信任边界) 的封闭枚举和精确环境组合。

capability 的封闭枚举为 `supported`、`detected_only`、`observed_anonymous`、`synthetic_covered`、`live_unverified`、`live_verified`、`unknown`。schema version 1 的数组值和顺序固定为：

- `password_core`：`capability_before` 为 `["synthetic_covered", "live_unverified"]`；通过后为 `["synthetic_covered", "live_verified"]`。
- `terminal_qr`：`capability_before` 为 `["observed_anonymous", "synthetic_covered", "live_unverified"]`；通过后为 `["observed_anonymous", "synthetic_covered", "live_verified"]`。

总体 `fail` 或 `blocked` 时，`capability_after` 必须与 `capability_before` 完全相同；总体 `pass` 时必须精确使用对应 suite 的通过后数组。

`steps` 的 ID、顺序、prefix、cleanup、result 归并以及 `exit_code/error_code` 对应关系必须满足 [`REL-LIVEGATE-002`](../operations/live-validation.md#rel-livegate-002suite-状态机与清理权)。

`duration_seconds` 必须是非负整数；不要求各 step 时长之和等于 `finished_at - started_at`。

禁止自由 `notes`、reviewer handle、命令文本、profile、username、IP、interface、URL、`details` 或原始输出。

## EVID-GATE-001：证据门禁

- 真实证据必须满足 [`REL-LIVE-MATRIX-001`](../operations/live-validation.md#rel-live-matrix-001v1-真实网络矩阵)，password 在 NAS wired、BHK wired 和 BHK Wi-Fi 分别完成要求的闭环。
- Terminal QR 在 NAS wired 完成一次扫码、轮询、HTTPS activation 和最终身份检查。
- 异账号 conflict/switch 只有合成覆盖；OTP detected-only 不阻塞 v1，但被 OTP 阻断的必需 suite 不算通过。
- fail/blocked 记录可以保留供审阅，但不得提升能力状态或进入 promotion lock 的通过集合。
- 证据只对绑定的 candidate-set 有效；候选失效、协议/认证页面或核心传输实现变化后，相关能力回到 `live_unverified`。

## EVID-CAPTURE-001：源端捕获与持久性

- runner 在源端直接构造字段 allowlist，禁止先写原始 stdout/stderr、网络数据或页面再脱敏。
- QR payload 必须直达维护者私有 TTY；password 必须由产品 TTY prompt 读取。runner、shell history、SSH 转录和 evidence 都不得接触它们。
- evidence ID allocator 使用 `started_at` 的 UTC 日期，在 `build/live-evidence/<candidate-id>/` 下选择最低可用序号 `001` 至 `999`；最终目录名遵守 schema 的 `evidence_id` 规则且必须 no-clobber，竞争时重新选择最低可用序号，序号耗尽则退出 13 且无 bundle。
- runner 只在最终目录同一父目录内创建隐藏 staging directory；staging 名称不得匹配 evidence ID，审阅器与 allocator 不得把它识别为 bundle。成功 publish 后最终目录恰好包含 `evidence.json`、`summary.md`、`SHA256SUMS`；任意失败都不发布最终目录，并 best-effort 清理 staging。
- Unix 必须对三个文件分别 flush/`fsync`，对完整 staging directory `fsync`，no-clobber rename publish 后再对父目录 `fsync`。
- Windows 必须对三个文件分别 flush/`FlushFileBuffers`，并使用 write-through 的原子 replace 与 no-clobber directory publish；文件 flush 和 write-through publish 共同构成 durability barrier。
- 只有 `evidence.json` 与 `summary.md` 已持久化后才生成固定两行 `SHA256SUMS`；checksum 文件持久化后必须重新读取并验证三文件的名称、精确字节、权限与 hash，才允许 publish。
- candidate-manifest、candidate 路径/类型/权限、manifest projection 或任一 preflight hash 拒绝时，runner 退出 12 且不得创建有效 bundle。
- candidate/manifest 运行后 drift、bundle 权限不满足、任一 flush/fsync/FlushFileBuffers/write-through/publish/复核失败时，runner 退出 13 且不得保留有效 bundle。
- 在原子目录发布提交点前，runner cancel 的优先退出码为 130；即使同时观察到 gate 或持久化失败也不得改写为其他退出码，并且不得发布或保留有效 bundle。
- 最终目录原子发布成功是 evidence 提交点；此后到达的取消不再被 runner 观察，已提交的有效 bundle 及其 0、10 或 11 结果不得被追溯丢弃或改写为 130。

## EVID-REVIEW-001：人工复核与入库

1. 维护者在源 testbed 检查三个文件的私有权限和 `SHA256SUMS`，再传回本地 Windows 私有目录。
2. 本地重新验证 bundle hash、schema、固定枚举、candidate/source 绑定、时间顺序和所有禁止字段；不得仅依赖 runner 的自检结果。
3. 维护者人工检查 [`EVID-REDACT-001`](#evid-redact-001绝对禁止内容)。审阅身份由 Git 提交/PR 记录体现，不写入 evidence 字段。
4. 只有选定的 candidate/evidence ID、平台、testbed、network、auth method、suite、result、capability before/after、UTC 时间和整个私有 bundle 的 SHA-256 可进入 `docs/evidence/releases/<version>/` 的脱敏摘要。
5. 私有 bundle 不得进入 Git、issue、PR 附件或公共 artifact。是否保留和何时安全删除由维护者在独立批准窗口决定。

## EVID-SECURITY-001：安全事件证据

安全事件演练可以记录脱敏的扫描命中数量、规则 ID、仓库内路径、工具版本、提交/tag 哈希及复扫结果，但不得复制 finding、匹配片段、原始报告、认证材料或可用于恢复认证材料的哈希。演练结果必须明确标注是否修改了真实 refs，不能用隔离镜像通过代替会话失效、远端强制更新或 GitHub 缓存清理。

当前记录：[`EVID-HISTORY-DRYRUN-001`](2026-08-27-history-rewrite-dry-run.md#evid-history-dryrun-001历史重写隔离演练)。

## 文件命名

发布证据摘要放在 `releases/<version>/`，文件名只使用固定 evidence ID 或 `promotion-lock.json`/`release-notes.md`；安全事件使用 `YYYY-MM-DD-<event>.md`。目录中的 README 可被工具索引，但禁止生成包含秘密的调试附件。
