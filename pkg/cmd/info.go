package cmd

import (
	"fmt"

	"ipgw-meta/pkg/handler"
	"ipgw-meta/pkg/utils"

	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "显示当前网络账户的使用情况（流量、时长、余额等）",
	Long:  "从深澜网关快速抓取当前认证设备的概览数据。无需配置密码即可查询本设备的使用情况。",
	Run: func(cmd *cobra.Command, args []string) {
		h := handler.NewIPGWHandler()
		utils.Log.Debug("正在向网关请求设备流量信息...")

		info, err := h.FetchGatewayInfo()
		if err != nil {
			utils.Log.Error(err.Error())
			return
		}

		fmt.Println()
		utils.Log.Info("【网关连接状态】 正常 (已认证)")
		utils.Log.Info(" 用户账号：  " + info.Username)
		utils.Log.Info(" IP 地址：   " + info.IP)
		utils.Log.Info(" 已用流量：  " + info.FormatTraffic())
		utils.Log.Info(" 已用时长：  " + info.FormatDuration())
		utils.Log.Info(" 账户余额：  " + info.FormatBalance() + " 元")
		fmt.Println()
	},
}

func init() {
	rootCmd.AddCommand(infoCmd)
}
