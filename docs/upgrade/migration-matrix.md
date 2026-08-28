---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
---

# 功能与兼容迁移矩阵

兼容目标是用户工作流、配置迁移和命令意图，不包含旧输出字节、错误退出码或危险副作用。

## MIG-CMD-001：无参数旧版登录

- 旧能力：无参数调用会直接登录。
- v1 目标：`ipgw-legacy` 保留旧工作流；`ipgw-meta` 无参数仅显示只读状态或帮助。
- 入口与阶段：legacy/meta，M2。

## MIG-CMD-002：核心登录命令

- 旧能力：login、logout、status 和 info 分散实现。
- v1 目标：统一进入 Go SDK，由 meta 提供稳定命令，并保留 legacy 工作流映射。
- 入口与阶段：SDK + CLI，M1–M2。

## MIG-NET-001：接口绑定与网络扫描

- 旧能力：指定接口并显示接口信息。
- v1 目标：`ListInterfaces` 只执行本地枚举；`network scan` 由 CLI 组合 `Status`。
- 入口与阶段：SDK + meta，M1–M2。

## `neucn/ipgw` JSON 配置

此项由 [`MIG-CONFIG-001`](../operations/config-migration.md#mig-config-001目标布局)权威声明。v1 通过 `profile migrate` 执行预览、备份、冲突处理和幂等迁移，阶段为 M2。

## MIG-CONFIG-002：当前 Meta YAML 配置

- 旧风险：当前 Meta YAML 可能包含旧字段或不安全的凭据表示。
- v1 目标：通过 `profile migrate` 迁移；损坏配置必须停止，不得静默丢弃或覆盖。
- 入口与阶段：profile migrate，M2。

## MIG-CRED-001：旧凭据存储与参数密码

- 旧风险：Base64 配置和命令行参数可能泄露密码。
- v1 目标：使用 keyring、env、权限受限文件或 TTY prompt；YAML 只保存 provider 引用。
- 入口与阶段：app/provider，M2。

## MIG-MODE-001：三入口模式切换

- 旧能力：单一二进制具有固定默认行为。
- v1 目标：legacy、meta 与 dispatcher 原子发布；现有安装不得静默切换模式。
- 入口与阶段：install bundle，M2–M3。

## MIG-SESSION-001：在线会话管理

- 旧能力：设备列表、精确下线和批量下线。
- 迁移目标：先建立 SDK domain model，再进入 meta CLI，最后补充 legacy 映射。
- 入口与阶段：session，1.x-1。

## MIG-USAGE-001：套餐与当前用量

- 旧能力：查询套餐和当前用量。
- 迁移目标：先进入 SDK domain model，再进入 meta CLI，最后补充 legacy 映射。
- 入口与阶段：account，1.x-2。

## MIG-BILL-001：历史用量、账单与充值

- 旧能力：历史用量、账单和充值。
- 迁移目标：按 SDK、meta、legacy 顺序迁移，且不得将 Dashboard HTML 或 API 细节暴露为公共 SDK 类型。
- 入口与阶段：account，1.x-3。

## MIG-UPDATE-001：不安全自更新

- 旧风险：自更新缺少完整签名、大小限制和原子回滚。
- v1 目标：保持禁用；满足 `REL-UPDATE-001` 后另行恢复。
- 入口与阶段：release，v1 后。

## 明确不迁移

- HTTP 凭据/ticket 传输、TLS 跳过、自动不安全降级。
- 把公网连通性当作登录成功、注销失败后继续切换账号。
- 打印错误仍返回退出码 `0`。
- 在日志、JSON、配置或 fixture 中存储凭据与认证材料。
- v1 的 GUI、daemon、多语言 RPC、通用协议插件和 command credential provider。
