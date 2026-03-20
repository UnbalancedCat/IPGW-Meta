package utils

import (
	"fmt"
	"net"
)

// InterfaceStatus 单个网口的探测结果
type InterfaceStatus struct {
	Name      string // 网卡名，如 "以太网", "WLAN"
	IP        string // 该网口的 IPv4 地址
	InCampus  bool   // 是否在校园网内
	IsLogged  bool   // 是否已登录认证
	Username  string // 登录学号（若有）
	GatewayIP string // 网关侧返回的 IP
	Error     string // 探测错误信息
}

// ListIPv4Interfaces 获取系统中所有 UP 状态的网口及其 IPv4 地址。
// 过滤 loopback 和 down 的接口，每个网口仅取第一个 IPv4 地址。
func ListIPv4Interfaces() ([]InterfaceStatus, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("枚举系统网络接口失败: %w", err)
	}

	var results []InterfaceStatus

	for _, iface := range ifaces {
		// 跳过 loopback 和未启用的接口
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP

			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// 仅取 IPv4 地址，跳过 IPv6
			if ip == nil || ip.To4() == nil {
				continue
			}

			results = append(results, InterfaceStatus{
				Name: iface.Name,
				IP:   ip.String(),
			})
			break // 每个网口只取第一个 IPv4 地址
		}
	}

	return results, nil
}
