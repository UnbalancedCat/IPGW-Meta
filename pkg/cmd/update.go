package cmd

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"os"

	"ipgw-meta/pkg/utils"

	"github.com/Masterminds/semver/v3"
	"github.com/minio/selfupdate"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "检测并自动安装最新版本的 IPGW-Meta",
	Long:  "从 GitHub Releases 取回最新适配本机架构的预编译包，并进行无缝原地热更新（免去覆盖执行文件的麻烦）。",
	Run: func(cmd *cobra.Command, args []string) {
		utils.Log.Info("正在连接 GitHub 检测最新版本...")

		rel, err := utils.FetchLatestRelease()
		if err != nil {
			utils.Log.Error("版本检测失败", "Error", err)
			return
		}

		// 比较版本号
		currVer, err := semver.NewVersion(Version)
		if err != nil {
			utils.Log.Warn("解析本地版本失败，强制触发全量更新检测", "Error", err)
		} else {
			latestVer, err := semver.NewVersion(rel.TagName)
			if err != nil {
				utils.Log.Error("解析远端版本号失败", "Error", err)
				return
			}
			if !latestVer.GreaterThan(currVer) {
				utils.Log.Info("当前已是最新版本，无需更新！", "Version", Version)
				return
			}
		}

		asset := utils.GetAssetForCurrentPlatform(rel)
		if asset == nil {
			utils.Log.Error("当前系统架构未找到对应的发行包资产", "TagName", rel.TagName)
			return
		}

		utils.Log.Info("发现可用新版本！", "TargetVersion", rel.TagName, "Assets", asset.Name)
		utils.Log.Info("正在从 GitHub 拉取发行全量包，请耐心等待...")

		resp, err := http.Get(asset.BrowserDownloadURL)
		if err != nil || resp.StatusCode != 200 {
			utils.Log.Error("下载更新包失败", "Error", err)
			return
		}
		defer resp.Body.Close()

		zipData, err := io.ReadAll(resp.Body)
		if err != nil {
			utils.Log.Error("读取下载流失败", "Error", err)
			return
		}

		zipReader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
		if err != nil {
			utils.Log.Error("解压 ZIP 文件失败", "Error", err)
			return
		}

		// 筛选二进制可执行文件
		var binaryFile *zip.File
		for _, f := range zipReader.File {
			if f.Name == "ipgw" || f.Name == "ipgw.exe" {
				binaryFile = f
				break
			}
		}

		if binaryFile == nil {
			utils.Log.Error("更新失败: 打包 ZIP 中未发现可预期的 ipgw 核心二进制文件！")
			return
		}

		binStream, err := binaryFile.Open()
		if err != nil {
			utils.Log.Error("解压二进制核心内容失败", "Error", err)
			return
		}
		defer binStream.Close()

		utils.Log.Info("正在将新版核心装载至当前执行线程以实施覆盖更新...")
		err = selfupdate.Apply(binStream, selfupdate.Options{})
		if err != nil {
			utils.Log.Error("原地自我更新覆盖失败 (可能是权限不足导致的写入受阻)", "Error", err)
			return
		}

		utils.Log.Info("自我更新完成！工具已成功升级至最新版！")
		os.Exit(0)
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
