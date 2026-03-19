package cmd

import (
	"time"

	"ipgw-meta/pkg/handler"
	"ipgw-meta/pkg/model"
	"ipgw-meta/pkg/utils"

	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "登录校园网",
	Long:  `使用配置好的学号和密码，完成校园网统一登录并截取 Ticket 激活放行。`,
	Run: func(cmd *cobra.Command, args []string) {
		username, _ := cmd.Flags().GetString("username")
		password, _ := cmd.Flags().GetString("password")

		var targetUser, targetPass string

		if username != "" {
			accounts := model.GetAccounts()
			found := false
			for _, acc := range accounts {
				if acc.Username == username {
					targetUser = acc.Username
					targetPass = acc.GetDecryptedPassword()
					found = true
					break
				}
			}

			if found {
				if password != "" {
					targetPass = password
				}
			} else {
				if password == "" {
					utils.Log.Error("未找到该账号保存的密码", "建议", "执行 'ipgw login -u 学号 -p 密码' 或先执行 'ipgw config account add'")
					return
				}
				targetUser = username
				targetPass = password
			}
		} else {
			acc, err := model.GetDefaultAccount()
			if err == nil && acc != nil {
				targetUser = acc.Username
				targetPass = acc.GetDecryptedPassword()
				if password != "" {
					targetPass = password
				}
			} else {
				utils.Log.Error("未配置默认账号且未在命令行提供凭据", "建议", "执行 'ipgw config account add -u 学号 -p 密码 --default' 添加并设为默认账号")
				return
			}
		}

		if targetUser == "" || targetPass == "" {
			utils.Log.Error("未能解析出有效的学号与密码")
			return
		}

		h := handler.NewIPGWHandler()

		utils.Log.Debug("执行 Smart Login，检查当前设备登录状态...")
		info, err := h.FetchGatewayInfo()
		if err == nil && info != nil {
			if info.Username == targetUser {
				utils.Log.Info("当前设备已使用该账号登录，无需重复操作", "学号", targetUser)
				return
			} else {
				utils.Log.Warn("检测到当前设备已登录其他账号，准备下线原账号...", "原账号", info.Username)
				if err := h.DoLogout(info.Username); err != nil {
					utils.Log.Error("下线原账号失败", "error", err)
				} else {
					utils.Log.Info("原账号下线成功，即将登入新账号")
					time.Sleep(1 * time.Second)
				}
			}
		}

		utils.Log.Info("准备执行校园网登录流程...", "学号", targetUser)
		if err := h.DoLogin(targetUser, targetPass); err != nil {
			utils.Log.Error("登录进程阻断", "错误信息", err)
		} else {
			utils.Log.Info("校园网登录/激活成功，网关已放行设备流量")
		}
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
	loginCmd.Flags().StringP("username", "u", "", "学生的真实学号")
	loginCmd.Flags().StringP("password", "p", "", "统一身份认证密码")
}
