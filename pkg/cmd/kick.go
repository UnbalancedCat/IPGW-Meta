package cmd

import (
	"fmt"
	"os"

	"ipgw-meta/pkg/handler"
	"ipgw-meta/pkg/model"
	"ipgw-meta/pkg/utils"

	"github.com/spf13/cobra"
)

var (
	kickAll bool
)

var kickCmd = &cobra.Command{
	Use:   "kick",
	Short: "断开网关的设备连接 (下线设备)",
	Long:  "下线绑定在当前账号下的设备。由于底层网关限制，目前稳定支持强制下线所有已连接设备 (-a/--all)。",
	Run: func(cmd *cobra.Command, args []string) {
		if !kickAll {
			utils.Log.Warn("由于网关改版，目前仅支持断开「所有」设备。请使用 ipgw kick -a 或 --all")
			return
		}

		acc, err := model.GetDefaultAccount()
		if err != nil || acc == nil {
			utils.Log.Error("未配置账号密码，无法执行安全下线操作")
			utils.Log.Info("请使用 'ipgw config account add -u <学号> -p <密码>' 进行配置")
			os.Exit(1)
		}

		user := acc.Username
		pass := acc.GetDecryptedPassword()

		h := handler.NewIPGWHandler()
		utils.Log.Info("正在通过统一认证验证身份以获取下线权限...")

		// 先进行一轮静默登录，刷新 CookieJar 中的 mysession 会话
		err = h.DoLogin(user, pass)
		if err != nil {
			utils.Log.Error("身份验证失败，无法获取网络下级令牌: " + err.Error())
			os.Exit(1)
		}

		utils.Log.Info("认证成功，正在发送全端下线广播...")
		// 调用 v1/batch-online-drop 接口，无需提供显式 cookie，因为之前登录步骤已经把 cookie 保存在 h.client 的 CookieJar 中
		err = h.DropAllConnections("")
		if err != nil {
			utils.Log.Error("全部下线指令执行失败: " + err.Error())
			return
		}

		fmt.Println()
		utils.Log.Info("操作成功！当前账号下的所有设备（包括本机）均已下线。")
		utils.Log.Info("如果需要继续上网，请重新执行 'ipgw login'。")
	},
}

func init() {
	kickCmd.Flags().BoolVarP(&kickAll, "all", "a", false, "断开该账号下所有的已联网设备")
	rootCmd.AddCommand(kickCmd)
}
