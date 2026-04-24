package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	gatewayExamplesPath = "docs/generated/gateway-rpc-examples.json"
	gatewayRPCDocPath   = "docs/gateway-rpc-api.md"
)

// main 执行 Gateway RPC 文档一致性校验，确保生成示例与主文档的关键方法声明不漂移。
func main() {
	if err := checkGatewayRPCDocConsistency(gatewayExamplesPath, gatewayRPCDocPath); err != nil {
		fmt.Fprintf(os.Stderr, "gateway docs consistency check failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("verified gateway docs consistency: %s <-> %s\n", gatewayExamplesPath, gatewayRPCDocPath)
}

// checkGatewayRPCDocConsistency 校验示例 JSON 中的 gateway 方法在主文档中均有对应 Method 小节。
func checkGatewayRPCDocConsistency(examplesPath, docPath string) error {
	examples, err := readGatewayExamples(examplesPath)
	if err != nil {
		return err
	}

	docContent, err := readGatewayRPCDoc(docPath)
	if err != nil {
		return err
	}
	if !containsAnyPathReference(docContent, pathReferenceCandidates(examplesPath)) {
		return fmt.Errorf("rpc doc %q must reference generated examples file %q", docPath, examplesPath)
	}

	missingSections := collectMissingMethodSections(docContent, collectGatewayMethods(examples))
	if len(missingSections) > 0 {
		return fmt.Errorf("rpc doc %q is missing sections for generated methods: %s", docPath, strings.Join(missingSections, ", "))
	}
	return nil
}

// readGatewayExamples 读取并解析生成的示例文件，统一错误包装。
func readGatewayExamples(examplesPath string) (map[string]json.RawMessage, error) {
	rawExamples, err := os.ReadFile(examplesPath)
	if err != nil {
		return nil, fmt.Errorf("read examples file %q: %w", examplesPath, err)
	}

	var examples map[string]json.RawMessage
	if err := json.Unmarshal(rawExamples, &examples); err != nil {
		return nil, fmt.Errorf("decode examples file %q: %w", examplesPath, err)
	}
	return examples, nil
}

// readGatewayRPCDoc 读取 Gateway RPC 主文档内容。
func readGatewayRPCDoc(docPath string) (string, error) {
	rawDoc, err := os.ReadFile(docPath)
	if err != nil {
		return "", fmt.Errorf("read rpc doc %q: %w", docPath, err)
	}
	return string(rawDoc), nil
}

// pathReferenceCandidates 返回示例文件可能出现的文档引用形式，兼容绝对路径与仓库相对路径。
func pathReferenceCandidates(examplesPath string) []string {
	normalizedInput := filepath.ToSlash(examplesPath)
	return []string{
		normalizedInput,
		filepath.ToSlash(filepath.Join("docs", "generated", filepath.Base(examplesPath))),
	}
}

// containsAnyPathReference 判断文档是否包含任意一个合法引用路径。
func containsAnyPathReference(content string, candidates []string) bool {
	for _, candidate := range candidates {
		if strings.Contains(content, candidate) {
			return true
		}
	}
	return false
}

// collectMissingMethodSections 收集文档中缺失的方法小节标题，便于稳定输出错误信息。
func collectMissingMethodSections(docContent string, methods []string) []string {
	missingSections := make([]string, 0)
	for _, method := range methods {
		heading := "## Method: " + method
		if !strings.Contains(docContent, heading) {
			missingSections = append(missingSections, heading)
		}
	}
	return missingSections
}

// collectGatewayMethods 从生成示例键中提取 gateway.* 方法名并排序，便于稳定校验与报错。
func collectGatewayMethods(examples map[string]json.RawMessage) []string {
	methods := make([]string, 0, len(examples))
	for key := range examples {
		if strings.HasPrefix(key, "gateway.") {
			methods = append(methods, key)
		}
	}
	sort.Strings(methods)
	return methods
}
