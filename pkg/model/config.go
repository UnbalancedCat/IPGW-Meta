package model

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// AppConfig 为配置文件结构体模型
type AppConfig struct {
	LogStyle string `mapstructure:"log_style"` // 可选值: native, charm-plain, charm-color
	LogLevel string `mapstructure:"log_level"` // 可选值: debug, info, warn, error
}

type AccountConfig struct {
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"` // Base64 编码存放
}

// GetDecryptedPassword 安全解出文本
func (a *AccountConfig) GetDecryptedPassword() string {
	dec, err := base64.StdEncoding.DecodeString(a.Password)
	if err != nil {
		return a.Password // 如果不是 base64 则说明是被强行重写的明文
	}
	return string(dec)
}

type Config struct {
	App            AppConfig       `mapstructure:"app"`
	Accounts       []AccountConfig `mapstructure:"accounts"`
	DefaultAccount string          `mapstructure:"default_account"`
}

// GlobalConfig 导出全局实例供外部读取
var GlobalConfig *Config

// InitConfig 初始化全局配置管家
func InitConfig(customPath string) error {
	// --------- 1. 注入默认配置参数 ---------
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	viper.SetDefault("app.log_style", "charm-color")
	viper.SetDefault("app.log_level", "info")
	viper.SetDefault("accounts", []AccountConfig{})
	viper.SetDefault("default_account", "")

	// --------- 2. 特定路径加载或多级查找路径 ---------
	if customPath != "" {
		viper.SetConfigFile(customPath)
		if err := viper.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); ok || os.IsNotExist(err) {
				dir := filepath.Dir(customPath)
				if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
					os.MkdirAll(dir, 0755)
				}
				if writeErr := viper.WriteConfigAs(customPath); writeErr != nil {
					fmt.Printf("[Warning] 无法在指定的自定义路径创建配置文件, 将启用默认值。错误: %v\n", writeErr)
				}
			} else {
				fmt.Printf("[Warning] 自定义配置文件解析异常 (文件损坏或权限问题), 将启用默认值。错误信息: %v\n", err)
			}
		}
	} else {
		var configDirs []string

		// 优先级 1: 系统全局配置目录 (如 ~/.config/ipgw 或 %APPDATA%\ipgw)
		if sysConfigDir, err := os.UserConfigDir(); err == nil {
			configDirs = append(configDirs, filepath.Join(sysConfigDir, "ipgw"))
		} else if homeDir, err := os.UserHomeDir(); err == nil {
			// 备用方案: 传统的 ~/.ipgw
			configDirs = append(configDirs, filepath.Join(homeDir, ".ipgw"))
		}

		// 优先级 2: 可执行文件同级目录 (便携模式)
		if exePath, err := os.Executable(); err == nil {
			configDirs = append(configDirs, filepath.Dir(exePath))
		} else {
			// 兜底: 尝试获取当前工作目录
			if cwd, err := os.Getwd(); err == nil {
				configDirs = append(configDirs, cwd)
			}
		}

		// 将所有收集到的有效路径注册到 viper
		for _, dir := range configDirs {
			viper.AddConfigPath(dir)
		}

		// --------- 3. 执行读取与优雅降级 ---------
		if err := viper.ReadInConfig(); err != nil {
			// 判断是否是“查无此文件”的错误
			if _, ok := err.(viper.ConfigFileNotFoundError); ok {
				fallbackCreated := false

				// 遍历收集到的目录，尝试阶梯式创建配置文件
				for _, dir := range configDirs {
					// 确保存储目录存在
					if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
						if mkdirErr := os.MkdirAll(dir, 0755); mkdirErr != nil {
							continue // 目录创建失败（无权限），跳过并尝试下一个优先级
						}
					}

					targetPath := filepath.Join(dir, "config.yaml")
					if writeErr := viper.WriteConfigAs(targetPath); writeErr == nil {
						fallbackCreated = true
						break // 只要有一个目录成功写入，就跳出循环
					}
				}

				// 如果所有物理路径都写入失败（极端无权限环境），仅在控制台给出弱提示，但不中断进程
				if !fallbackCreated {
					fmt.Println("[Warning] 无法在系统中创建配置文件，工具将以默认的无痕内存模式运行。")
				}
			} else {
				// 不是文件找不到的错误，可能是 yaml 格式语法被用户手改坏了。这种情况需要警告但不必须阻断。
				fmt.Printf("[Warning] 配置文件解析异常 (文件损坏或权限问题), 将启用默认值。错误信息: %v\n", err)
			}
		}
	}

	// --------- 4. 映射内存并移交控制权 ---------
	GlobalConfig = &Config{}

	// 这里即使之前文件没读到，viper 也会把 SetDefault 的默认值正确塞进单例里
	if err := viper.Unmarshal(GlobalConfig); err != nil {
		// 这种结构体反序列化级别的死锁，才值得阻断进程
		return fmt.Errorf("将配置反序列化到运行时结构体失利: %w", err)
	}

	return nil
}

// AddAccount 添加新账号，如果账号已存在则覆盖密码
func AddAccount(username, password string, isDefault bool) error {
	var accounts []AccountConfig
	if err := viper.UnmarshalKey("accounts", &accounts); err != nil {
		accounts = []AccountConfig{}
	}

	encodedPassword := base64.StdEncoding.EncodeToString([]byte(password))
	found := false
	for i, acc := range accounts {
		if acc.Username == username {
			accounts[i].Password = encodedPassword
			found = true
			break
		}
	}
	if !found {
		accounts = append(accounts, AccountConfig{
			Username: username,
			Password: encodedPassword,
		})
	}

	viper.Set("accounts", accounts)

	// 如果这是第一个账号，或者指定了默认，则设为默认
	if len(accounts) == 1 || isDefault {
		viper.Set("default_account", username)
	}

	return viper.WriteConfig()
}

// UpdateAccount 更新现有账号密码或设置其为默认账号
func UpdateAccount(username string, newPassword *string, setAsDefault *bool) error {
	var accounts []AccountConfig
	if err := viper.UnmarshalKey("accounts", &accounts); err != nil {
		accounts = []AccountConfig{}
	}

	found := false
	for i, acc := range accounts {
		if acc.Username == username {
			found = true
			if newPassword != nil {
				accounts[i].Password = base64.StdEncoding.EncodeToString([]byte(*newPassword))
			}
			break
		}
	}

	if !found {
		return fmt.Errorf("账号 %s 不存在", username)
	}

	viper.Set("accounts", accounts)

	if setAsDefault != nil && *setAsDefault {
		viper.Set("default_account", username)
	}

	return viper.WriteConfig()
}

// DeleteAccount 删除指定账号，如果删除的是默认账号，且还有其它账号，则选第一个为默认
func DeleteAccount(username string) error {
	var accounts []AccountConfig
	if err := viper.UnmarshalKey("accounts", &accounts); err != nil {
		accounts = []AccountConfig{}
	}

	var newAccounts []AccountConfig
	found := false
	for _, acc := range accounts {
		if acc.Username == username {
			found = true
			continue
		}
		newAccounts = append(newAccounts, acc)
	}

	if !found {
		return fmt.Errorf("账号 %s 不存在", username)
	}

	viper.Set("accounts", newAccounts)

	defaultAcc := viper.GetString("default_account")
	if defaultAcc == username {
		if len(newAccounts) > 0 {
			viper.Set("default_account", newAccounts[0].Username)
		} else {
			viper.Set("default_account", "")
		}
	}

	return viper.WriteConfig()
}

// GetAccounts 获取所有存储的账号
func GetAccounts() []AccountConfig {
	var accounts []AccountConfig
	if err := viper.UnmarshalKey("accounts", &accounts); err != nil {
		return []AccountConfig{}
	}
	return accounts
}

// GetDefaultAccount 获取当前默认账号
func GetDefaultAccount() (*AccountConfig, error) {
	accounts := GetAccounts()
	if len(accounts) == 0 {
		return nil, fmt.Errorf("未配置任何账号")
	}

	defaultAcc := viper.GetString("default_account")
	if defaultAcc == "" && len(accounts) > 0 {
		return &accounts[0], nil
	}

	for _, acc := range accounts {
		if acc.Username == defaultAcc {
			return &acc, nil
		}
	}

	// 容错：如果配置的 default_account 不存在，则返回第一个
	return &accounts[0], nil
}
