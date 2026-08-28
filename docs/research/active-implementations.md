---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
snapshot_date: 2026-08-27
---

# 活跃实现与官方页面研究快照

本页只记录可复核的协议线索与风险，不把第三方实现当作规范。运行时不依赖这些仓库；更新结论时必须记录新的固定提交和日期。

| 来源 | 固定版本 | 可参考事实 | 不采纳的做法 |
|---|---|---|---|
| [neucn/ipgw](https://github.com/neucn/ipgw) | `8a52d79` | 旧命令、配置与 Dashboard 工作流兼容基线 | 旧成功语义和不安全副作用 |
| [Neboer/ipgw-py-manager](https://github.com/Neboer/ipgw-py-manager/commit/3583017) | `3583017`（2026-05-14） | 动态 form action、`lt`、`execution`、公开 JS 公钥、动态 `ac_id`、JSONP、logout 参数 | HTTP gateway、明文密码、字符串成功判断、详细敏感日志 |
| [DoraTiger/NEU_IPGW](https://github.com/DoraTiger/NEU_IPGW/commit/f0b608b) | `f0b608b`（2026-06-23） | HTTPS gateway 的近期可用性线索 | 硬编码公钥、跳过 TLS 验证、弱业务成功判断 |
| chiyuki0325/NEU-IPGW | `70b3f03`（2026-05-10） | 移动 SSO / TGT / SMS 端点只作为研究线索 | 保存或打印 TGT；未验证的移动协议不得进入 v1 |
| clodite 浏览器实现 | `f6d6552`（2026-03-18） | 浏览器路径可作为人工 canary | 不作为 SDK 正确性或 headless 支持证据 |

## 官方匿名观察

- [统一身份认证页面](https://pass.neu.edu.cn/tpass/login)在快照日可匿名观察到动态表单字段、公开登录脚本、QR 登录入口、验证码/可信设备 UI。
- [QR 登录脚本](https://pass.neu.edu.cn/tpass/comm/js/login-qrcode.js)显示二维码会话、约 3 秒轮询和约 180 秒期限；这些数值属于匿名观察，不是永久协议常量。
- [学校服务页](https://xwb.neu.edu.cn/10053/list.htm)提供 HTTPS 校园网入口线索。
- 当前公开登录脚本表现为 RSA 加密 `username + password`；实现必须动态发现并在异常时返回 `protocol_changed`，不得把快照公钥或页面标题写死。

## 证据等级

第三方代码和匿名页面只能证明 `observed_anonymous`。合成 fixture 通过后可标记 `synthetic_covered`；真实校园网完成并按 [`EVID-REDACT-001`](../evidence/README.md)记录后才可标记 `live_verified` 或 `supported`。
