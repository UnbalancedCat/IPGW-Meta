---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
adr: ADR-0009
status: accepted
---

# ADR-0009：真实认证采用分离的管理面与测试面

真实校园网验收需要运行固定候选、私下输入凭据并在失败时可靠清理会话。双网口 NAS 可以提供可复用实验环境，但若管理流量和测试流量共享默认路由、宿主占用测试口，或在 Docker host network 中认证，就无法确定协议流量经过哪条网络，也可能让自动化意外控制已有会话。

决定如下：

- 本地 Windows 负责协调、下载、attestation/hash 验证与传输；NAS 上的 Codex 仅位于管理 Ubuntu VM，一次性测试 VM 不安装 Codex。
- 测试 VM 的管理接口只有私网静态路由，没有默认路由或 DNS；固定 MAC 的测试接口是唯一默认路由和 DNS。NAS 宿主测试物理口不得配置 IP、DHCP 或默认路由。
- 只采用 ZOS 正式支持的桥接或直通能力；不 root hack，不使用 Docker host network。无法证明隔离时 fail closed，NAS 只做离线 Linux 测试，真实认证转到 BHK 原生 Windows。
- 候选按固定 hash 传入远端，远端不放 GitHub token、不传源码、`.git`、秘密历史或备份，也不重新构建。
- 密码与二维码仅在维护者私有 TTY 中处理。runner 初始状态必须明确 offline，不能自动注销已有或未知会话；只对本轮确认创建的会话拥有一次有界清理权。
- evidence 在源端直接按 allowlist 构造，禁止先保存原始日志再脱敏，禁止 pcap、截图、页面、headers、URL、标识符或任何认证材料。
- 只有一个授权账号，因此异账号 conflict/switch 保持合成覆盖和 live unverified，不借用第二账号且不阻塞 v1。

代价是需要物理换线、虚拟机快照和部分人工操作；收益是管理通道、测试路由、凭据交互与可公开证据具有明确边界，故障时也不会猜测或扩大网络/会话权限。
