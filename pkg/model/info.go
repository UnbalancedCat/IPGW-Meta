package model

import "fmt"

// GatewayInfo 包含通过基础 API 获取的网络仪表盘信息
type GatewayInfo struct {
	Username string
	Traffic  int64   // 已用流量 (Bytes)
	Duration int64   // 已用时长 (Seconds)
	Balance  float64 // 账户余额
	IP       string  // 登录 IP 地址
}

func (i GatewayInfo) FormatTraffic() string {
	gb := float64(i.Traffic) / (1024 * 1024 * 1024)
	return fmt.Sprintf("%.2f GB", gb)
}

func (i GatewayInfo) FormatDuration() string {
	hours := i.Duration / 3600
	minutes := (i.Duration % 3600) / 60
	seconds := i.Duration % 60
	return fmt.Sprintf("%d小时%d分%d秒", hours, minutes, seconds)
}

func (i GatewayInfo) FormatBalance() string {
	return fmt.Sprintf("%.2f", i.Balance)
}
