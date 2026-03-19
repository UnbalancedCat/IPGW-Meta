package cmd

import (
	"fmt"
	"os"

	"ipgw-meta/pkg/model"
	"ipgw-meta/pkg/utils"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
)

var (
	verbose     bool
	configPath  string
	showVersion bool

	Version = "0.1.0"
	Build   = "unknown"
	Repo    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "ipgw",
	Short: "东北大学 IPGW 自动登录与管理工具 Meta",
	Long:  `一个更现代、健壮的命令行工具，用于登录和管理东北大学校园网 (IPGW)。`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if showVersion {
			fmt.Printf("IPGW-Meta %s\n", Version)

			rel, err := utils.FetchLatestRelease()
			if err == nil && rel != nil {
				currVer, _ := semver.NewVersion(Version)
				latestVer, _ := semver.NewVersion(rel.TagName)
				if latestVer != nil && currVer != nil && latestVer.GreaterThan(currVer) {
					fmt.Printf("\n✨ 发现新版本 %s！请执行 'ipgw update' 进行无缝热更新。\n", rel.TagName)
				}
			}
			os.Exit(0)
		}

		// 在执行任何子命令之前，初始化配置与日志
		if err := model.InitConfig(configPath); err != nil {
			fmt.Printf("无法加载配置模块: %v\n", err)
			os.Exit(1)
		}
		utils.InitLogger(model.GlobalConfig, verbose)
	},
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

const chineseUsageTemplate = `用法:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}
{{if .HasAvailableSubCommands}}
可用命令:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}
{{if .HasAvailableLocalFlags}}
可选参数:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}
{{if .HasAvailableInheritedFlags}}
全局参数:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}
{{if .HasHelpSubCommands}}
其他帮助主题:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}
{{if .HasAvailableSubCommands}}
使用 "{{.CommandPath}} [command] --help" 获取有关某个命令的更多信息。{{end}}

项目主页: https://github.com/UnbalancedCat/IPGW-Meta
问题反馈: https://github.com/UnbalancedCat/IPGW-Meta/issues
`

// Execute 执行根命令
func Execute() error {
	rootCmd.SetUsageTemplate(chineseUsageTemplate)
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.SetHelpCommand(&cobra.Command{
		Use:   "help [command]",
		Short: "显示命令的帮助信息",
		Run: func(c *cobra.Command, args []string) {
			cmd, _, e := c.Root().Find(args)
			if cmd == nil || e != nil {
				c.Printf("未知的 help 主题 %#q\n", args)
				c.Root().Usage()
			} else {
				cmd.InitDefaultHelpFlag() // make possible 'help' flag to be shown
				cmd.Help()
			}
		},
	})
	return rootCmd.Execute()
}

func init() {
	// 注册全局级别 Flag
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "显示详细的 debug 请求日志 (无视配置文件)")
	rootCmd.PersistentFlags().BoolVarP(&showVersion, "version", "V", false, "显示当前程序版本")
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "指定自定义配置文件的绝对或相对路径")

	// 覆写默认的 help flag 翻译
	rootCmd.InitDefaultHelpFlag()
	if flag := rootCmd.Flags().Lookup("help"); flag != nil {
		flag.Usage = "显示帮助信息"
	}
	if flag := rootCmd.PersistentFlags().Lookup("help"); flag != nil {
		flag.Usage = "显示帮助信息"
	}
}
