package cmd

import (
	"ipgw-meta/pkg/handler"
	"ipgw-meta/pkg/model"
	"ipgw-meta/pkg/utils"

	"github.com/spf13/cobra"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "主动注销网关，断开当前网络的连接",
	Run: func(cmd *cobra.Command, args []string) {
		u, _ := cmd.Flags().GetString("username")

		// 如果未明确声明学号，回退使用配置文件中的默认学号
		if u == "" {
			acc, err := model.GetDefaultAccount()
			if err == nil && acc != nil {
				u = acc.Username
			}
		}

		if u == "" {
			utils.Log.Error("未指定要注销的学号", "解决办法", "使用 -u 指定学号，或先执行 ipgw config account add 登记账号")
			return
		}

		utils.Log.Info("准备向服务大厅发起设备脱网信令...", "学号", u)
		h := handler.NewIPGWHandler(bindIP)
		if err := h.DoLogout(u); err != nil {
			utils.Log.Error("断网失败: 注销请求非预期结束", "error", err)
		} else {
			utils.Log.Info("设备下线成功")
		}
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
	logoutCmd.Flags().StringP("username", "u", "", "指定的学号 (若已配置可忽略此参数)")
}
