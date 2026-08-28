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

runner 只有在本轮同时观察到 initial offline、login success 和最终目标身份匹配后，才拥有本轮会话的清理权。失败或取消最多执行一次有界 logout，再验证 offline；清理失败报告 fail/blocked，并要求维护者通过官方门户确认。窗口结束不得遗留 QR 轮询或未知会话。

退出码固定为：0 suite pass；10 gate fail；11 blocked；12 security reject；13 evidence durability failure；14 internal；130 cancel。OTP/unknown challenge 是 blocked/interaction_required，不能替代必需成功门禁。

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
