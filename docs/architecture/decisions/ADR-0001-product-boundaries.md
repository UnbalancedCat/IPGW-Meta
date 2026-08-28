---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
adr: ADR-0001
status: accepted
---

# ADR-0001：产品边界

决定采用单仓库、单 module、一个公开 Go SDK 与 `ipgw-legacy`、`ipgw-meta`、`ipgw` 三套薄入口。CAS、Srun、Dashboard 是 internal 边界；profile、配置、credential provider 选择和 renderer 属于应用层。v1 不建设 GUI、daemon、多语言 RPC 或通用协议插件。

理由是保证协议逻辑只有一份，同时允许旧工作流稳定退场并为未来同进程 Go GUI 提供接口。

详细契约见 [`ARCH-BOUNDARY-001`](../overview.md) 和 [`SDK-API-001`](../../reference/go-sdk.md)。
