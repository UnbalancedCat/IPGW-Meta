package cmd

import (
	"fmt"

	"ipgw-meta/pkg/handler"
	"ipgw-meta/pkg/utils"

	"github.com/spf13/cobra"
)

var infoAll bool

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "显示当前网络账户的使用情况（流量、时长、余额等）",
	Long:  "从深澜网关快速抓取当前认证设备的概览数据。使用 --all 遍历所有网口并发探测校园网连接状态。",
	Run: func(cmd *cobra.Command, args []string) {
		if infoAll {
			h := handler.NewIPGWHandler()
			utils.Log.Info("正在并发扫描系统所有网口...")
			results, err := h.ScanAllInterfaces()
			if err != nil {
				utils.Log.Error("网口扫描失败", "error", err)
				return
			}

			fmt.Println()
			utils.Log.Info("======== 全网口校园网探测结果 ========")
			for _, r := range results {
				if r.Error != "" {
					utils.Log.Warn(fmt.Sprintf("[%s] %s | 探测失败: %s", r.Name, r.IP, r.Error))
					continue
				}

				line := fmt.Sprintf("[%s] %s", r.Name, r.IP)

				if r.InCampus {
					line += " | 校园网: 已连接"
					if r.IsLogged {
						line += " | 认证: 已登录"
						if r.Username != "" {
							line += fmt.Sprintf(" | 学号: %s", r.Username)
						}
					} else {
						line += " | 认证: 未登录"
					}
					utils.Log.Info(line)
				} else {
					line += " | 校园网: 未连接"
					utils.Log.Warn(line)
				}
			}
			utils.Log.Info(fmt.Sprintf("======== 探测完毕, 共 %d 个网口 ========", len(results)))
			fmt.Println()
			return
		}

		h := handler.NewIPGWHandler(bindIP)
		utils.Log.Debug("正在向网关请求设备流量信息...")

		info, err := h.FetchGatewayInfo()
		if err != nil {
			utils.Log.Error(err.Error())
			return
		}

		fmt.Println()
		utils.Log.Info("【网关连接状态】 正常 (已认证)")
		utils.Log.Info(" 用户账号:  " + info.Username)
		utils.Log.Info(" IP 地址:   " + info.IP)
		utils.Log.Info(" 已用流量:  " + info.FormatTraffic())
		utils.Log.Info(" 已用时长:  " + info.FormatDuration())
		utils.Log.Info(" 账户余额:  " + info.FormatBalance() + " 元")
		fmt.Println()
	},
}

func init() {
	rootCmd.AddCommand(infoCmd)
	infoCmd.Flags().BoolVarP(&infoAll, "all", "a", false, "遍历所有网口, 分别探测校园网连接状态")
}
