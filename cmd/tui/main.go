package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"go-llm-demo/configs"
	"go-llm-demo/internal/tui/bootstrap"
)

func main() {
	workspaceFlag := flag.String("workspace", "", "指定工作区根目录")
	flag.Parse()

	setUTF8Mode()

	workspaceRoot, err := bootstrap.PrepareWorkspace(*workspaceFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析工作区失败: %v\n", err)
		os.Exit(1)
	}

	scanner := bufio.NewScanner(os.Stdin)
	ready, err := bootstrap.EnsureAPIKeyInteractive(context.Background(), scanner, "config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化配置失败: %v\n", err)
		os.Exit(1)
	}
	if !ready {
		fmt.Println("已退出 NeoCode")
		return
	}

	if err := configs.LoadAppConfig("config.yaml"); err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	persona, personaPath, err := configs.LoadPersonaPrompt(configs.GlobalAppConfig.Persona.FilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "警告: 人设加载失败: %v\n", err)
	} else if personaPath != "" && strings.TrimSpace(configs.GlobalAppConfig.Persona.FilePath) != personaPath {
		fmt.Fprintf(os.Stderr, "提示: 人设已从 %s 回退加载\n", personaPath)
	}
	historyTurns := configs.GlobalAppConfig.History.ShortTermTurns

	p, err := bootstrap.NewProgram(persona, historyTurns, "config.yaml", workspaceRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化失败: %v\n", err)
		os.Exit(1)
	}
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "运行失败: %v\n", err)
		os.Exit(1)
	}
}

func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || os.Getenv(key) != "" {
			continue
		}

		value = strings.Trim(value, `"'`)
		os.Setenv(key, value)
	}

	return nil
}

func loadPersonaPrompt(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

