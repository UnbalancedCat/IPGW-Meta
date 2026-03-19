<p align="center">
    <img src="./assets/logo.png" width="200" alt="ipgw-meta"/>
</p>


<h1 align="center">IPGW-Meta<br><br>东北大学非官方跨平台校园网关客户端</h1>

<p align="center"><a href="#下载安装">下载安装</a> | <a href="#快速开始">快速开始</a> | <a href="https://github.com/UnbalancedCat/IPGW-Meta/issues/new">问题反馈</a></p>

## 简介

**IPGW-Meta** 是东北大学非官方跨平台校园网关客户端（[IPGW](https://github.com/neucn/ipgw)）针对最新统一身份认证系统及其后台网络策略而重构的现代化命令行工具。
由于上游（网关系统）引入了动态 WAF 检测、混淆令牌（lt/execution）以及长密钥前端 RSA 环境包装，原有的自动化脚本失效。本项目采用了原生的 Go 语言与协议劫持重新打造了一个抗干扰、高稳定且极具扩展性的跨平台 CLI 管理工具。

### 核心特性

- **协议级认证实现**：通过解析 CAS 动态票据（lt/execution）并应用基于 `crypto/rsa` 的等效前端加密规范，实现直接通过 HTTP 请求完成深澜（Srun）系统认证。
- **多账号与会话管理**：支持本地多账号凭证存储。登录请求前将调用网关 API 查询设备在线状态，若检测到其他账号的存活会话，会自动发起下线请求并切换登入目标账号。
- **IPv4 强制路由**：通过 Hook `net.DialContext` 绑定 IPv4 连接，规避双栈网络下因部分 IPv6 寻址导致的网络认证异常。
- **配置分级加载**：优先在操作系统标准配置目录（如 Windows AppData、Linux `~/.config` 等）存取配置，在无权限环境下可降级至可执行文件同级目录读写。
- **结构化日志输出**：集成 `charmbracelet/log` 提供日志分级输出功能，支持使用 `charm-color` 彩色高亮或 `native` 纯文本格式以便调试。
- **配置持久化存储**：配置文件中的用户凭证通过 Base64 编码进行基础混淆存储。
- **网络连通性检测**：结合深澜网关的内部 API 查询与公网 HTTP 请求（如 Generate 204），进行双重验证以排除 TUN/TAP 类代理软件造成的假阳性误判。

---

## 下载安装

**推荐使用一键脚本安装预编译的二进制文件：**

### Windows
如果身处 Windows 平台，以管理员或其他拥有适当权限的身份打开 PowerShell 并执行：
```powershell
iwr https://raw.githubusercontent.com/UnbalancedCat/IPGW-Meta/master/install.ps1 -useb | iex
```

### Linux / macOS / FreeBSD
打开受支持的 Shell 终端，直接通过 cURL 一键拉取安装：
```shell
curl -fsSL https://raw.githubusercontent.com/UnbalancedCat/IPGW-Meta/master/install.sh | sh
```

### 手动编译 (开发者/其他环境)
当前源代码使用较高 TLS 安全标准限制库，请使用 **Go 1.25.0 或以上** 构建环境：
```Bash
# 1. 把仓库克隆到本地
git clone https://github.com/UnbalancedCat/IPGW-Meta.git

# 2. 进入目录并拉取相关依赖
cd IPGW-Meta
go mod tidy

# 3. 跨平台自由编译 (也可以直接 `make release` 触发批量构建)
go build -trimpath -ldflags "-s -w" -o ipgw ./cmd/ipgw/main.go
```
执行完毕后，当前目录下会生成一个单独的二进制可执行文件。你可以直接将它放进环境变量 `$PATH` 里。

---

## 快速开始

保存账号信息 (密码经过基础编码防窥视)

```shell
ipgw config account add -u "学号" -p "密码" --default
```

一键极速登入校园网

```shell
ipgw login
```

一键获取当前套餐使用情况及余额

```shell
ipgw info
```

快速断开校园网连接

```shell
ipgw logout
```

> **提示：** 更多高级技巧（如多账户智能切换、远程强踢所有已登录设备下线、详尽的 Debug 排雷模式等）请参阅 [**API 手册 (进阶使用)**](./API.md)。

---

## 配置文件 (配置持久化机制)

通过全新的配置管家（`spf13/viper`），系统会在第一次启动时自动寻址**创建默认配置**。它被阶梯式存放在以下目录（按优先级查找）：

1. **默认存储地址**:
    * Windows: `C:\Users\<Username>\AppData\Roaming\ipgw\config.yaml`
    * Linux: `~/.config/ipgw/config.yaml`
    * macOS: `~/Library/Application Support/ipgw/config.yaml`
2. **便携备用地址**: 若无以上系统目录的写入权限，退化至 **本工具同级目录** 自动生成。

你可以参考本仓库根目录下的 `example_config.yaml` 来了解完整的配置项结构。

### 3. 指定自定义配置文件路径
如果需要多环境隔离或由脚本自动触发，可使用全局参数 `--config`（简写 `-c`）强制指定配置文件的存取路径（无视上述默认优先级）。若目标文件不存在，工具将自动创建：
```Bash
ipgw --config /opt/ipgw/my_config.yaml login
ipgw -c ./testing.yaml config account add -u <学号> -p <密码> --default
```



---

## 特别鸣谢

**IPGW-Meta** 是基于优秀的开源前身项目 [neucn/ipgw](https://github.com/neucn/ipgw) 带来的启发与基础实现重构改造而来的。
特别感谢原作者及社区的所有贡献者们做出的探索与前置开源工作。

---
**声明：** 本软件仅用于学术网络研究和个人终端快捷接入使用，账号数据经非对称加密后交由学校内网处理与防篡改校对，不包含任何云端分发与持久化服务链接，开发者不对非正当应用后果及其造成的任何安全策略处分负责。
