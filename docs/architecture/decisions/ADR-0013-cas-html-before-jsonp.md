---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
adr: ADR-0013
status: accepted
---

# ADR-0013：CAS HTML 优先于 JSONP challenge 分类

真实 BHKDesktop Win11 物理有线的无凭据诊断证明，当前 CAS 响应是成功的 HTML 登录页：`loginForm`、同源可解析 action、`lt`、`execution`、密码输入和公开登录脚本均存在。旧判别器却只要在任意正文位置同时发现 `(` 与后续 `{`，就先把整个响应送入 JSONP 解码器；普通页面内 JavaScript 的函数和对象字面量因此被误判为结构化 envelope，导致凭据读取前错误返回 `protocol_changed`。

决定如下：

- challenge 分类先移除可选 UTF-8 BOM 并裁剪首尾空白；首个有效字符为 `<` 时，必须直接按 HTML DOM 处理，不得扫描脚本标点来推断整页格式。
- HTML challenge 仍先记录普通密码表单形状，再移除 script、style、template、noscript、hidden、`aria-hidden`、disabled 和内联隐藏节点，仅以剩余活动控件与可见文本分类挑战。
- JSON/JSONP 继续严格解析。JSONP 必须是覆盖整个响应的单一安全 callback 调用，callback 只能使用允许的简单标识符，唯一参数必须是 JSON object，并拒绝重复字段、尾随脚本、额外调用或第二个 envelope。
- 未识别、矛盾或格式错误的结构化响应继续 fail closed；本变更只消除普通 HTML 被脚本内容误归类为 JSONP 的假阳性，不扩大挑战成功条件，也不改变凭据、Cookie、ticket、redirect 或最终身份边界。
- 回归测试必须包含带函数调用与对象字面量的普通 CAS HTML、严格 JSONP、损坏 JSONP、HTML 活动挑战、dormant 控件和响应内容泄漏 canary。

旧 Meta 从 DOM 直接读取 `lt`/`execution`，因此未触发这一特定误判；它同时使用固定 POST 目标、硬编码公钥 fallback、宽松重定向和敏感日志，仍只可作为协议研究线索，不能复制其安全降级。该决策保留 v1 的动态 form action、公钥和挑战模型，只校正 wire-format 分派顺序。
