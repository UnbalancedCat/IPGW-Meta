---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
adr: ADR-0014
status: accepted
---

# ADR-0014：CAS RSA-512 仅作为 HTTPS 内兼容封装

2026-09-03 对 Candidate `v1.0.0-fbce60521ba7-33607679812.1` 的 BHKDesktop Win11 物理有线零凭据诊断，在明确 offline、Wi-Fi/TUN/IPv4 cover route 均不存在的条件下，从官方 CAS 同源 HTTPS 登录脚本动态发现 PKIX RSA 公钥。安全分类只输出公钥结构而不输出公钥内容：模数 512 位、指数 65537、PKCS#1 v1.5 最大明文 53 bytes。诊断执行 6 个网络请求、3 次 DNS，不读取凭据、不发送 CAS POST、不 activation、不 logout。Candidate 原有 2048 位实现下限因此在本地加密阶段返回 `protocol_changed`；旧 Meta 的固定 RSA-2048 公钥不是当前 wire contract，也不能作为 fallback。

决定如下：

- 密码、Cookie 与 ticket 的保密和服务端认证边界仍是系统 PKI 校验的 HTTPS；CAS 页面要求的 RSA PKCS#1 v1.5 `rsa` 字段只视为该 TLS 通道内的应用协议兼容封装，不宣称提供独立于 TLS 的现代密码学强度。
- 每次认证必须从官方 CAS 页面或其同源 HTTPS 公开登录脚本动态发现公钥。只接受有界 base64 DER 中的 PKIX 或 PKCS#1 RSA，模数范围 512–8192 位；不得固定、持久化、缓存或 fallback 到历史公钥。
- 低于 Go 通用 RSA 安全下限的 512–1023 位路径只允许用于上述 CAS 加密字段，并必须使用密码学安全随机源生成 PKCS#1 v1.5 非零 padding。该例外不得用于解密、签名、TLS、release、配置或其他协议。
- 加密明文精确为 UTF-8 `username + password`，长度不得超过 `key_size_bytes - 11`。容量不足、无效指数、畸形/非 RSA DER、越界模数或随机源失败均在 CAS POST 前返回 `protocol_changed`；不得截断、哈希替代、发送明文、关闭 TLS 或切换固定密钥。
- 1024 位及以上继续使用 Go 标准库 RSA 实现；兼容路径必须用合成 512 位私钥做 round-trip 测试，并覆盖 PKIX、PKCS#1、下界拒绝、容量边界和秘密不进入错误信息。

这一决策是对当前官方服务协议的窄兼容，不把 RSA-512 认可为一般安全选择。服务端升级密钥时客户端继续使用本次动态值；若页面来源、格式、算法或边界发生变化，仍 fail closed 为 `protocol_changed`。
