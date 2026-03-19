package cmd

import (
	"fmt"
	"ipgw-meta/pkg/model"
	"ipgw-meta/pkg/utils"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "配置管理与本地凭证存储",
	Long:  "用于管理和存储 IPGW 的个性化配置，或者将账号密码本地加密保存，实现免密登录。",
}

var configAccountCmd = &cobra.Command{
	Use:   "account",
	Short: "多账号管理",
}

var configAccountAddCmd = &cobra.Command{
	Use:   "add",
	Short: "添加新的账号凭证",
	Run: func(cmd *cobra.Command, args []string) {
		u, _ := cmd.Flags().GetString("username")
		p, _ := cmd.Flags().GetString("password")
		isDefault, _ := cmd.Flags().GetBool("default")

		if u == "" || p == "" {
			utils.Log.Error("参数不足: add 操作必须同时携带 -u [学号] 和 -p [密码]")
			return
		}

		accounts := model.GetAccounts()
		exists := false
		for _, acc := range accounts {
			if acc.Username == u {
				exists = true
				break
			}
		}

		if exists {
			utils.Log.Warn(fmt.Sprintf("账户 %s 已存在，是否覆盖其密码？[y/N]: ", u))
			var confirm string
			fmt.Scanln(&confirm)
			if confirm != "y" && confirm != "Y" {
				utils.Log.Info("已取消添加/覆盖操作")
				return
			}
		}

		if err := model.AddAccount(u, p, isDefault); err != nil {
			utils.Log.Error("持久化写入凭据失败", "error", err)
			return
		}

		utils.Log.Info("账号信息已安全保存", "学号", u)
		
		if isDefault {
			utils.Log.Info("之后只需要执行 'ipgw login' 即可触发免密自动登录。")
		}
	},
}

var configAccountSetCmd = &cobra.Command{
	Use:   "set",
	Short: "修改账号密码或设置为默认账号",
	Run: func(cmd *cobra.Command, args []string) {
		u, _ := cmd.Flags().GetString("username")
		if u == "" {
			utils.Log.Error("参数缺失: 必须提供 -u [学号]")
			return
		}

		var newPwd *string
		if cmd.Flags().Changed("password") {
			p, _ := cmd.Flags().GetString("password")
			newPwd = &p
		}

		var setDef *bool
		if cmd.Flags().Changed("default") {
			isDefault, _ := cmd.Flags().GetBool("default")
			setDef = &isDefault
		}

		if newPwd == nil && setDef == nil {
			utils.Log.Warn("未指定任何修改项，请提供 -p [新密码] 或 --default [是否设为默认]")
			return
		}

		if err := model.UpdateAccount(u, newPwd, setDef); err != nil {
			utils.Log.Error("修改账号失败", "error", err)
			return
		}
		utils.Log.Info("账号修改成功", "学号", u)
	},
}

var configAccountShowCmd = &cobra.Command{
	Use:   "show",
	Short: "展示所有已保存的账号",
	Run: func(cmd *cobra.Command, args []string) {
		accounts := model.GetAccounts()
		if len(accounts) == 0 {
			utils.Log.Info("当前未保存任何账号")
			return
		}

		defAcc, _ := model.GetDefaultAccount()
		defUser := ""
		if defAcc != nil {
			defUser = defAcc.Username
		}

		utils.Log.Info("已保存的账号列表:")
		for i, acc := range accounts {
			mark := " "
			if acc.Username == defUser {
				mark = "*"
			}
			utils.Log.Info(fmt.Sprintf("[%s] %d. %s", mark, i+1, acc.Username))
		}
	},
}

var configAccountDelCmd = &cobra.Command{
	Use:   "del",
	Short: "删除指定的账号",
	Run: func(cmd *cobra.Command, args []string) {
		u, _ := cmd.Flags().GetString("username")
		if u == "" {
			utils.Log.Error("参数缺失: 必须提供 -u [学号]")
			return
		}

		utils.Log.Warn(fmt.Sprintf("确定要删除账号 %s 吗？[y/N]: ", u))
		var confirm string
		fmt.Scanln(&confirm)
		if confirm != "y" && confirm != "Y" {
			utils.Log.Info("已取消删除操作")
			return
		}

		if err := model.DeleteAccount(u); err != nil {
			utils.Log.Error("删除账号失败", "error", err)
			return
		}
		utils.Log.Info("账号已成功删除", "学号", u)
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configAccountCmd)

	configAccountCmd.AddCommand(configAccountAddCmd)
	configAccountAddCmd.Flags().StringP("username", "u", "", "学生的真实学号")
	configAccountAddCmd.Flags().StringP("password", "p", "", "统一身份认证密码")
	configAccountAddCmd.Flags().Bool("default", false, "是否设为默认账号")

	configAccountCmd.AddCommand(configAccountSetCmd)
	configAccountSetCmd.Flags().StringP("username", "u", "", "指定要修改的真实学号")
	configAccountSetCmd.Flags().StringP("password", "p", "", "统一身份认证新密码")
	configAccountSetCmd.Flags().Bool("default", false, "是否设为默认账号")

	configAccountCmd.AddCommand(configAccountShowCmd)

	configAccountCmd.AddCommand(configAccountDelCmd)
	configAccountDelCmd.Flags().StringP("username", "u", "", "要删除的学号")
}
