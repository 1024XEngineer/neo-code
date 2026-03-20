package security

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3" // 引入我们刚刚下载的 YAML 解析库
)

// Rule 定义了单条权限规则的结构
type Rule struct {
	// 目标匹配条件 (三选一)
	Target  string `yaml:"target,omitempty"`  // 匹配文件或目录路径，例如 "src/**/*.go"
	Command string `yaml:"command,omitempty"` // 匹配终端命令，例如 "rm -rf *"
	Domain  string `yaml:"domain,omitempty"`  // 匹配网络域名，例如 "github.com"

	// 权限动作设定 (allow, deny, ask)
	R string `yaml:"r,omitempty"` // 读权限 (Read)
	W string `yaml:"w,omitempty"` // 写权限 (Write)
	X string `yaml:"x,omitempty"` // 执行权限 (Execute)
	N string `yaml:"n,omitempty"` // 网络权限 (Network)
}

// Config 定义了整个 yaml 文件的根结构
type Config struct {
	// Rules 是一个切片 (Slice)，相当于一个可以动态扩容的数组 (类似于 std::vector<Rule>)
	Rules []Rule `yaml:"rules"`
}

// SecurityManager 是我们这个拦截器的核心管理器，它将把三个名单都存在内存里
type SecurityManager struct {
	BlackList  Config
	WhiteList  Config
	YellowList Config
}

// NewSecurityManager 是一个“构造函数”。
// 在 Go 语言中没有类的概念，通常用 New 开头的函数来初始化并返回一个结构体指针。
func NewSecurityManager(configDir string) (*SecurityManager, error) {
	// 1. 在内存中分配一个 SecurityManager 的实例，并拿到它的指针 (&)
	sm := &SecurityManager{}

	// 2. 拼接文件路径并依次加载三个名单
	// 使用 filepath.Join 可以自动处理 Windows(\) 和 Linux(/) 的路径分隔符差异

	err := loadConfigFile(filepath.Join(configDir, "blacklist.yaml"), &sm.BlackList)
	if err != nil {
		return nil, fmt.Errorf("加载黑名单失败: %w", err)
	}

	err = loadConfigFile(filepath.Join(configDir, "whitelist.yaml"), &sm.WhiteList)
	if err != nil {
		return nil, fmt.Errorf("加载白名单失败: %w", err)
	}

	err = loadConfigFile(filepath.Join(configDir, "yellowlist.yaml"), &sm.YellowList)
	if err != nil {
		return nil, fmt.Errorf("加载黄名单失败: %w", err)
	}

	// 3. 一切顺利，返回装满数据的 SecurityManager 指针
	return sm, nil
}

// loadConfigFile 是一个内部辅助函数，负责干脏活累活。
// 注意参数 target 是一个指针 (*Config)，因为我们需要在函数内部修改它，这和 C/C++ 是一样的。
func loadConfigFile(filePath string, target *Config) error {
	// 读取整个文件内容到内存中 (yamlBytes 是一个 []byte 字节数组)
	yamlBytes, err := os.ReadFile(filePath)
	if err != nil {
		// 友好的容错设计：如果文件不存在，我们认为这个名单是“空”的，不报错直接返回。
		// os.IsNotExist 是 Go 标准库用来判断文件缺失的方法。
		if os.IsNotExist(err) {
			return nil
		}
		// 如果是权限不足等其他错误，则向上抛出
		return err
	}

	// 最神奇的一步：将读取到的字节流，反序列化(Unmarshal)塞进 target 指针指向的内存里
	err = yaml.Unmarshal(yamlBytes, target)
	if err != nil {
		return fmt.Errorf("解析 YAML 语法失败: %w", err)
	}

	return nil
}
