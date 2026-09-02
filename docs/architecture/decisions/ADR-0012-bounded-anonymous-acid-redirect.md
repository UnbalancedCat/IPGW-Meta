---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
adr: ADR-0012
status: accepted
---

# ADR-0012：有界匿名 ac_id redirect 发现

冻结候选曾假设初始 captive 响应的 Location 或正文直接包含 `ac_id`。真实匿名诊断证明网关可以先返回同 host、无 query 的中间 Location，再在下一响应的 Location 和正文中提供合法 `ac_id`。在初始响应处停止会使登录在读取凭据前错误返回 `protocol_changed`；按有线/Wi‑Fi 类型恢复历史常量则无法证明当前网络控制器身份，也违反发现优先和 fail-closed 原则。

决定如下：

- `ac_id` 发现使用独立的无 Cookie Jar HTTP client，只发送匿名 GET；不携带账号、Cookie、ticket 或凭据。
- 初始 captive 响应可以触发至多一次手动跟随。中间 Location 必须仍为同一网关 host，只使用 `http`/`https` 默认端口，且无 userinfo、fragment、query、控制字符、反斜杠或点段。
- 初始和第二响应都受固定大小限制；实现只接受同网关 Location 或正文中唯一的 1–10 位十进制候选。第二响应之后不继续第三跳。
- 非法 redirect、候选冲突、候选过多、需要更多跳或无法发现均返回 `protocol_changed`。不得按网卡名称、绑定 IP、历史成功值或公网可达性猜测 fallback。
- 发现值仍是不可信提示；CAS 动态表单、公钥、ticket redirect 与最终身份不变量继续独立验证，发现成功本身不构成登录成功证据。

该决策恢复旧实现“从实际 redirect 动态发现”的有效意图，同时删除其接口类型 fallback、无界自动重定向和认证 Cookie 复用。代价是对允许的匿名跳转形状更严格；网关再次改变时必须以新的封闭匿名诊断和规范变更处理，不能静默放宽。
