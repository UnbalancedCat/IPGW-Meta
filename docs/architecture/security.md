---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
---

# 安全边界

## SEC-SECRET-001：秘密定义与禁止落盘

密码、手机号、验证码、Cookie、CAS ticket、TGT、LT、QR payload、原始认证响应和带查询参数的认证 URL 均视为秘密或敏感认证材料。它们不得进入源码、测试 fixture、配置 YAML、launcher 状态、协议缓存、JSON 输出、Observer、普通日志、诊断包或公开 evidence。

Base64 不是加密。配置只保存 credential provider 引用：桌面默认 OS keyring，另支持环境变量、权限受限文件和 TTY prompt。Unix 文件 provider 强制 `0600`，Windows 限制为当前用户 ACL。

Unix 私密文件路径拒绝用户可控的父目录 symlink 和最终文件 symlink。macOS 固定系统别名例外必须同时满足 [`ADR-0010`](decisions/ADR-0010-macos-trusted-system-path-alias.md) 与 [`MIG-FILE-001`](../operations/config-migration.md#mig-file-001写入与权限)；该锚点之后的全部组件不得放宽，也不能用整路径 symlink 解析或 CI 临时目录覆盖绕过。

## SEC-HISTORY-001：历史泄露响应

已确认进入历史的测试 session 必须按以下顺序处理：

1. 尽可能通过正常注销/会话管理使其失效；
2. 删除真实 fixture，替换为合成数据；
3. 在受控备份后使用 `git-filter-repo --sensitive-data-removal --replace-text` 重写所有相关 refs；
4. 扫描全部历史，核对受影响 tag，强制更新远端并要求协作者重新克隆；
5. 按 [GitHub 官方流程](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository)处理缓存和 PR 引用。

仓库重写属于单独的高风险运维动作，不得由普通代码补丁隐式执行。

M0 与本节的未完成外部事项必须继续如实跟踪，但不再作为 Candidate、Promotion、release 或真实验收的状态门禁；精确风险接受和仍保留的独立 hard gate 见 [`ADR-0011`](decisions/ADR-0011-nonblocking-m0-governance.md)。新的可达秘密、有效凭据、secret scan finding 或泄漏测试失败仍必须立即 fail closed，本例外只适用于已经失去 ref 可达性的既知旧对象及其外部缓存处置。

2026-08-27 的隔离镜像演练、tag 映射及 `refs/codex` tree-ref 陷阱记录在 [`EVID-HISTORY-DRYRUN-001`](../evidence/2026-08-27-history-rewrite-dry-run.md#evid-history-dryrun-001历史重写隔离演练)；该记录不代表真实 refs 或远端已清理。

## SEC-TRANSPORT-001：秘密不经 HTTP

任何账号、密码、Cookie 或 ticket 都不得通过 HTTP 发送。系统 TLS 校验不可关闭，也不提供明文兼容开关。具体 redirect 和 endpoint 行为见 [`PROTO-REDIRECT-001`](protocol-correctness.md)。

匿名 captive/`ac_id` 发现不复用认证 Cookie Jar，最多在同一网关 host 内手动跟随一次经过 [`PROTO-DISCOVERY-001`](protocol-correctness.md#proto-discovery-001发现优先) 校验的无 query redirect。该例外只允许产生不可信协议提示，不扩大到 CAS、activation、status、logout、跨 host 跳转或网卡类型 fallback。

## SEC-LOG-001：最小可观测性

结构化事件仅包含阶段、耗时、脱敏结果、错误码和无秘密的网络类别。用户名默认不记录；IP 仅在用户明确请求的业务输出中出现。错误消息不能包含完整 URL、表单、header 或响应体。自动化测试必须对 stdout、stderr、JSON 和 Observer 做 canary 泄漏断言。

## SEC-UPDATE-001：自更新冻结

v1 暂时禁用自更新及发布入口。恢复前必须满足 `REL-UPDATE-001`：签名 manifest、校验和、严格大小限制、临时文件验证、原子替换、失败回滚和跨平台验收。

## SEC-REPORT-001：安全失败语义

协议不确定、身份不匹配、人工挑战或 TLS 失败必须 fail closed，返回稳定 typed error；不得退化为密码错误、离线或成功。取消必须保留 context 错误并映射退出码 130。
