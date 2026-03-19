package cmd

import (
	"fmt"
	"net"

	"ipgw-meta/pkg/handler"
	"ipgw-meta/pkg/utils"

	"github.com/spf13/cobra"
)

// getLocalIPv4 获取本地设备系统分配的真实 IP
func getLocalIPv4() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "未知 IP"
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "检测网络情况并测试外网连通性",
	Long:  "使用该命令以探查是否已连接校园网，并在活跃的身份下连接了互联网。提供双重校验：深澜内网接口验证及公网探测",
	Run: func(cmd *cobra.Command, args []string) {
		h := handler.NewIPGWHandler()

		utils.Log.Info("开始检测网络情况...")
		inCampus, isLoggedIn, user, ip, err := h.GetNetworkStatus()
		if err != nil {
			utils.Log.Error("校园网状态请求异常", "error", err)
			return
		}

		// 判断是否在校园网内
		if inCampus {
			utils.Log.Info("校园网内网环境：已连接")
		} else {
			utils.Log.Warn("校园网内网环境：未连接")
			utils.Log.Warn("未侦测到校园网环境，请检查您是否连接了东大相关的 Wi-Fi 或使用了 VPN 接入。当前环境无法使用账号登录网关。")
			return // 不在内网就不用往下看
		}

		if ip == "" {
			ip = getLocalIPv4() + "（由本地设备系统接口获取）"
		} else {
			ip = ip + "（由 IPGW 网关侧获取）"
		}
		utils.Log.Info(fmt.Sprintf("当前设备 IP 地址：%s", ip))

		// 登录状态
		if isLoggedIn {
			utils.Log.Info("IPGW 网关账密登录验证：已登录验证")
			utils.Log.Info(fmt.Sprintf("已登录认证的绑定学号：%s", user))
		} else {
			utils.Log.Warn("IPGW 网关账密登录验证：未登录认证")
		}

		// 探测外网
		if err := h.TestInternet(); err == nil {
			utils.Log.Info("外网畅通性（真实 IPv4 连通性）：畅通无阻")
			utils.Log.Info("================ 外网访问测试成功，当前正处于连通状态 ================")
		} else {
			utils.Log.Error(fmt.Sprintf("外网畅通性（真实 IPv4 连通性）：存在异常或未完全放行网络，%v", err))
		}
	},
}

func init() {
	rootCmd.AddCommand(testCmd)
}
