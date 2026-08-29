---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
---

# 真实校园网验收规范

真实认证只用于验证冻结候选，不用于协议探索或采集原始响应。维护者在私有 TTY 中完成所有凭据和二维码交互；runner 只能生成固定白名单证据。

## REL-LIVEGATE-001：Runner 接口与信任边界

源码固定在 `internal/livegate/` 与 `internal/cmd/ipgw-live-gate/`。helper 不是产品 CLI，不安装到 PATH，也不进入公开 release。接口为：

```text
ipgw-live-gate run
  --candidate ABS_PATH
  --candidate-sha256 HEX64
  --candidate-manifest ABS_PATH
  --suite password-core|terminal-qr
  --testbed nas_vm|bhk_windows
  --network campus_wired|campus_wifi
  --profile NAME
  --output-dir ABS_PATH
```

`--suite` 的 CLI 拼写只接受 `password-core` 与 `terminal-qr`，写入 `evidence.json` 时分别规范化为 `password_core` 与 `terminal_qr`；不得接受其他别名或大小写变体。

### Candidate-manifest v1 runner projection

`--candidate-manifest` 指向的 candidate-manifest v1 顶层必须包含以下 runner projection；七个字段都必需且各出现一次。未来 `WP-M3-CANDIDATE` 可以增加其他顶层字段，但不得改变这些字段的类型或语义：

| 字段 | runner 约束 |
|---|---|
| `schema_version` | JSON 整数 `1` |
| `plan_id` | `IPGW-META-V1` |
| `revision` | `2026-08-28-r2` |
| `version` | `v1.0.0` |
| `candidate_id` | `v1.0.0-<source-sha12>-<positive-run-id>.<positive-attempt>`；两个十进制数无前导零 |
| `source_commit` | 40 位小写十六进制，且前 12 位等于 `candidate_id` 的 source-sha12 |
| `live_gate_targets` | 下表规定的数组 |

`live_gate_targets` 必须恰好包含两个对象并保持固定顺序；每个对象恰好包含一次 `platform`、`name`、`size`、`sha256`，不得包含其他字段。`size` 是 `1..67108864` 的 JSON 整数，`sha256` 是 64 位小写十六进制：

| 索引 | platform | name |
|---:|---|---|
| 0 | `linux-amd64` | `ipgw-meta` |
| 1 | `windows-amd64` | `ipgw-meta.exe` |

candidate-manifest 必须是有效 UTF-8、大小不超过 64 KiB 的单一 JSON object。runner 使用单遍 token stream：任意层级 duplicate key、第二个 JSON value 或尾随非空白字节都拒绝；JSON grammar 的尾随空白允许。未知顶层字段必须流式跳过而不保留，但 candidate-manifest 的全部原始字节，包括未知字段与尾随空白，仍参与 digest。

`candidate_set_sha256` 定义为 candidate-manifest 精确原始字节的 SHA-256 小写十六进制；`--candidate-sha256` 是当前产品 binary 精确字节的 SHA-256 小写十六进制。runner 按当前平台选择固定 target，要求 candidate basename 与 target `name` 精确匹配、实际大小等于 target `size`，并要求 target `sha256`、显式 `--candidate-sha256`、运行前实际 SHA 与运行后实际 SHA 全部相同。

candidate-manifest 与 candidate 都必须是私有本地目录内的绝对、非 symlink/reparse 普通文件；runner 不得执行 manifest 之外的文件，也不得从名称猜测其他 target。

传输到 testbed 前仍须使用 GitHub artifact digest 与 provenance attestation 提供的可信 digest 复核 candidate-manifest 和 candidate；runner 的本地校验只记录 candidate/source/hash 绑定，不能自行建立 GitHub provenance。

schema version 1 的运行环境字段是封闭枚举：

- `platform`：`linux-amd64`、`windows-amd64`；
- `testbed`：`nas_vm`、`bhk_windows`；
- `network_type`：`campus_wired`、`campus_wifi`；
- `auth_method`：`password`、`terminal_qr`；
- `suite`：`password_core`、`terminal_qr`；
- 总体 `result` 与 step `result`：`pass`、`fail`、`blocked`。

`platform/testbed/network_type/auth_method/suite` 只允许以下精确组合：

| platform | testbed | network_type | auth_method | suite |
|---|---|---|---|---|
| `linux-amd64` | `nas_vm` | `campus_wired` | `password` | `password_core` |
| `linux-amd64` | `nas_vm` | `campus_wired` | `terminal_qr` | `terminal_qr` |
| `windows-amd64` | `bhk_windows` | `campus_wired` | `password` | `password_core` |
| `windows-amd64` | `bhk_windows` | `campus_wifi` | `password` | `password_core` |

上述 tuple 校验只约束 live-gate wire schema；它本身不满足 [`REL-LIVE-MATRIX-001`](#rel-live-matrix-001v1-真实网络矩阵) 另行要求的 Windows 原生离线安装，以及 BHK Wi-Fi 的 bind IP、network list/scan 验收。

- runner 必须执行冻结的 `ipgw-meta`，不得重新实现或绕过 SDK/CLI。
- candidate 必须是私有目录内的本地普通文件；manifest、显式 SHA 与运行前后实际 SHA 必须一致。
- profile/config 只用于启动参数，不进入证据。password suite 只接受 TTY prompt，不接受命令行、env、file 或 keyring 密码。
- QR payload 直接写向维护者私有 TTY，不进入 runner 捕获管道、JSON、日志、错误或 evidence。
- 初始 status 必须明确 offline；online 或 unknown 返回 blocked，runner 不得为了测试而自动注销既有会话。

## REL-LIVEGATE-002：Suite 状态机与清理权

`password-core` 固定状态机：

```text
initial status = offline
login = logged_in
status = online
second login = already_online 且未读取凭据
logout = logged_out
status = offline
second logout = already_offline
```

`terminal-qr` 固定状态机：

```text
initial status = offline
TTY QR login = logged_in
status = online
logout = logged_out
status = offline
```

### Candidate CLI 执行与输出捕获

任一 suite 在首次 status 前，runner 必须执行冻结 candidate 的 `--json --profile NAME profile show`，要求退出 0、envelope `schema_version=1`、`command=profile.show`、`ok=true`。`data.profile.username` 必须非空，只在内存中瞬时作为 expected username 使用；不得写入 evidence、日志或错误。

只有 `password-core` 还必须验证 `data.profile.credential.provider=prompt`，且 `data.profile.credential.reference` 不存在或为空字符串；`terminal-qr` 不要求 password credential provider，也不得以 provider 非 `prompt` 拒绝运行。

所有 JSON 模式 candidate stdout 都以 64 KiB 为上限执行单遍 token streaming，不落盘、不保留 raw JSON；无效 UTF-8、任意层级 duplicate key、第二个 JSON value 或尾随非空白字节都拒绝。公共 JSON 契约允许增加未知字段，runner 必须流式跳过；只保留状态机所需的封闭枚举、退出码、error code 与 username 是否匹配的布尔结果，不得保留 `message`、`details`、IP 或 username 原值。candidate stderr 始终直达维护者私有 TTY，不进入捕获管道或 bundle。

`password-core` 的凭据读取证明固定为：
- 第一次 `--json --profile NAME login --method password` 使用维护者 TTY 作为 stdin，并要求 `logged_in`、online 与 expected username 匹配。
- 第二次相同 login 使用 EOF、非 TTY stdin，必须退出 0 并返回 `already_online`、online 与 expected username 匹配；任何 prompt 尝试或其他结果都证明“未读取凭据”不成立。

`terminal-qr` 只允许在维护者确认 testbed 独占窗口后开始。initial status 必须 offline；human-mode `--profile NAME login --method qr` 的 stdout/stderr 直达私有 TTY，runner 只观察退出 0，随后不插入其他命令立即执行 JSON status；只有目标 username online 才合成为 `qr_login_logged_in`。任何并发会话、身份漂移或窗口不再独占的迹象都记为 `blocked`。

`cleanup_logout` 与 `cleanup_status_offline` 各自使用独立的 30 秒上限；前一步耗时、失败或超时不得缩短或跳过后一步的独立上限。

`evidence.json` 的 primary step ID 和顺序固定为：

- `password_core`：`initial_status_offline`、`login_logged_in`、`status_online`、`second_login_already_online`、`logout_logged_out`、`final_status_offline`、`second_logout_already_offline`；
- `terminal_qr`：`initial_status_offline`、`qr_login_logged_in`、`status_online`、`logout_logged_out`、`final_status_offline`。

`status_online` 只有在 status 为 online 且最终目标身份匹配时才算 `pass`；它同时建立本轮会话的清理权。

结果与步骤序列遵守以下不变量：

- 总体 `pass` 必须记录完整 primary sequence，全部 step 为 `pass`，且不得记录 cleanup step。
- 总体非 `pass` 只记录截至首个非 `pass` step（含该 step）的有序 primary prefix；首个非 `pass` 后不得继续 primary sequence。
- 仅当 `status_online` 已 `pass` 且 `final_status_offline` 尚未 `pass` 时，必须在 prefix 后依次追加且仅追加 `cleanup_logout`、`cleanup_status_offline`；其他情况禁止 cleanup step。两个 cleanup step 都必须记录，即使前一个非 `pass`。
- 非通过记录中，只要任一步为 `fail`，总体结果就是 `fail`；否则总体结果是 `blocked`。

step `exit_code` 只允许产品 CLI 的 0 至 7 以及 130，且与 `error_code` 固定对应：

| exit_code | error_code |
|---:|---|
| `0` | `null` |
| `1` | `internal` |
| `2` | `invalid_argument`、`config` 或 `unsupported` |
| `3` | `network` |
| `4` | `authentication` |
| `5` | `session_conflict` |
| `6` | `protocol_changed` |
| `7` | `interaction_required` |
| `130` | `network` |

step result 分类固定如下，不改变 evidence schema：

- `network`、`config`、`interaction_required`，以及 runner 本身未取消时 candidate `exit_code: 130`，均为 `blocked`；
- `invalid_argument`、`unsupported`、`internal`、`authentication`、`session_conflict`、`protocol_changed` 均为 `fail`；
- candidate 以 0/`null` 返回但 session 为 `unknown` 时为 `blocked`；
- candidate 以 0/`null` 返回明确但与该 step 相反的 outcome/session，或 expected identity 不匹配时为 `fail`；
- initial status 明确为 online 时继续遵守 `REL-LIVEGATE-001` 的既有特例并记为 `blocked`。

非零 exit code 的 step 不得为 `pass`；产品命令以 0/`null` 返回但观察值不符合该 step 的语义时，该 step 仍可为 `fail` 或 `blocked`。

step 中的 `exit_code: 130` 表示未取消的 runner 路径记录到 candidate CLI 取消，和 runner 自身退出 130 不同；前者可按上述规则进入非通过 evidence，后者在原子目录发布提交点前取消时不得保留有效 bundle。

失败或取消时，runner 仍受上述清理权和有界 cleanup 约束；清理失败要求维护者通过官方门户确认。窗口结束不得遗留 QR 轮询或未知会话。

runner 退出码固定为：0 suite pass；10 gate fail；11 blocked；12 security reject；13 evidence durability failure；14 internal；130 cancel。OTP/unknown challenge 是 blocked/`interaction_required`，不能替代必需成功门禁。

只有 runner 退出 0、10、11 才分别对应可保留的 `pass`、`fail`、`blocked` bundle；在原子目录发布提交点前退出 12、13、14 或 130 时不得生成或保留有效 bundle。

最终目录原子发布成功是 evidence 提交点；此后到达的取消不再被 runner 观察，已提交的有效 bundle 及其 0、10 或 11 结果不得被追溯丢弃或改写为 130。

## REL-LIVEGATE-003：候选绑定与结果输出

每次运行绑定 candidate ID、candidate-set SHA、source commit、测试平台、testbed、network、suite 和时间。runner 只创建 [`EVID-BUNDLE-001`](../evidence/README.md#evid-bundle-001私有证据-bundle) 规定的三个文件；不能捕获或暂存产品原始 stdout/stderr 后再脱敏。运行后 candidate hash 变化、证据无法持久化或出现未列字段时，结果不得为 pass。

## REL-LIVE-MATRIX-001：v1 真实网络矩阵

| 环境 | 必须执行 |
|---|---|
| NAS Ubuntu / campus wired | password-core、same-account、logout 幂等、Terminal QR |
| BHK Windows / campus wired | password-core、Windows 原生离线安装 |
| BHK Windows / campus Wi-Fi | password-core、bind IP、network list/scan |
| GitHub macOS runners | 安装和 CLI smoke；不声明校园网认证 |

只有一个授权校园账号，因此异账号 conflict/switch 只做合成测试并保持 `synthetic_covered + live_unverified`；不借用第二账号且不阻塞 v1。Terminal QR 只要求 NAS wired 完成一次真实闭环。OTP detected-only 不阻塞 v1，但 OTP 阻断的 password/QR 运行不能算通过。

## REL-TRANSFER-001：候选下载与远端传输

1. 本地 Windows 按精确 artifact ID 下载 candidate-set，验证 GitHub attestation、artifact digest、candidate-set SHA、manifest 和目标文件 SHA。
2. 将验证后的 candidate-set 保存为只读本地副本；记录 candidate ID/hash，不保存账号或网络标识。
3. 只通过 SCP 把所需平台 binary、对应 helper、manifest 和 checksums 发送到远端私有目录；不传源码、`.git`、GitHub token、秘密历史或备份。
4. 远端在运行前重新验证相同 SHA；不得 build、package、strip、sign 或改写候选。
5. 结果只回传经 [`EVID-REVIEW-001`](../evidence/README.md#evid-review-001人工复核与入库) 审阅的固定字段摘要和 evidence bundle hash。
