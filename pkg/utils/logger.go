package utils

import (
	"log/slog"
	"os"

	"ipgw-meta/pkg/model"

	"github.com/charmbracelet/log"
	"github.com/muesli/termenv"
)

// Log 定义一个所有包共享的全局记录器实例，初始设为丢弃状态防空指针
var Log = slog.New(slog.NewTextHandler(os.Stderr, nil))

// InitLogger 根据外部传入的全局配置策略，组装正确的底层日志器
// flagVerbose: 如果为 true 则强行覆盖设为 debug 及 charm-color (便于临时排错)
func InitLogger(config *model.Config, flagVerbose bool) {
	style := config.App.LogStyle
	levelStr := config.App.LogLevel

	if flagVerbose {
		style = "charm-color"
		levelStr = "debug"
	}

	var handler slog.Handler

	switch style {
	case "charm-color", "charm-plain":
		// 初始化一个高颜值输出引擎
		opts := log.Options{
			ReportTimestamp: true,
			TimeFormat:      "15:04:05",
			Prefix:          "IPGW",
		}

		// 根据配置匹配正确的 Level
		switch levelStr {
		case "debug":
			opts.Level = log.DebugLevel
		case "info":
			opts.Level = log.InfoLevel
		case "warn":
			opts.Level = log.WarnLevel
		case "error":
			opts.Level = log.ErrorLevel
		default:
			opts.Level = log.InfoLevel
		}

		// 如果选择的是 plain（无着色）或者终端原生不支持颜色
		if style == "charm-plain" {
			// 强行设为 ASCII 输出，去除所有颜色转义符，但保留多层级的精美缩进排版
			opts.Formatter = log.TextFormatter
			l := log.NewWithOptions(os.Stderr, opts)
			l.SetColorProfile(termenv.Ascii)
			handler = l
		} else {
			handler = log.NewWithOptions(os.Stderr, opts)
		}

	case "native":
		// 极简原生服务器日志
		var slogLevel slog.Level
		switch levelStr {
		case "debug":
			slogLevel = slog.LevelDebug
		case "warn":
			slogLevel = slog.LevelWarn
		case "error":
			slogLevel = slog.LevelError
		default:
			slogLevel = slog.LevelInfo
		}

		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slogLevel,
		})

	default:
		// 兜底日志器
		handler = log.NewWithOptions(os.Stderr, log.Options{Level: log.InfoLevel})
	}

	// 暴露出去
	Log = slog.New(handler)
}
