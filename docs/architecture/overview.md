---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
---

# 总体架构

## ARCH-BOUNDARY-001：产品边界

IPGW-Meta 使用单仓库、单 Go module、单 SDK 和三套薄入口。根 module 为 `github.com/UnbalancedCat/ipgw-meta`，根包名为 `ipgw`。

```text
ipgw-legacy ─┐
ipgw-meta ───┼─> application layer ─> public ipgw SDK ─> internal/{cas,srun,dashboard}
ipgw ────────┘              │
                            └─> config / credentials / renderers（SDK 之外）
```

- `ipgw-legacy`：1.x 兼容旧工作流，无参数保持旧登录语义。
- `ipgw-meta`：新命令；无参数只执行只读 status/help。
- `ipgw`：只做模式选择和进程分发，不包含网络或凭据逻辑。
- 三个入口共用同一 SDK、应用服务与 renderer，作为一个原子发布包安装和回滚。

## ARCH-MODE-001：模式选择

优先级固定为：`--mode` > `IPGW_MODE` > 独立 launcher 配置 > 安装批次默认值。旧安装升级后固定 legacy；v1 后的新安装默认 meta；任何升级不得静默改变已保存选择。legacy 最早在 2.0 移除。

## ARCH-SDK-001：职责划分

SDK 是无配置文件、无 profile、无 keyring 决策的状态无关协议库。应用层负责：

- profile 和配置迁移；
- credential provider 选择；
- TTY/JSON 呈现；
- 三入口兼容与退出码；
- launcher 和协议缓存的持久化位置。

协议内部边界为 CAS、Srun 和 Dashboard。v1 不引入通用插件框架、daemon、多语言 RPC 或 GUI。未来同进程 Go GUI 直接嵌入 SDK，非 Go 客户端优先使用稳定 JSON CLI。

## ARCH-CONCURRENCY-001：生命周期

`NewClient` 不访问网络。Client 只读操作并发安全，`Login`/`Logout` 在同一个 Client 内串行；每次认证使用独立 Cookie Jar 和 redirect policy，不能修改调用者传入的 Transport 或共享 Client。

相关决策见 [ADR 索引](decisions/README.md)。
