package commands

import (
	"strings"

	"github.com/sahilm/fuzzy"
)

// SlashCommand 描述单个 slash 命令定义。
type SlashCommand struct {
	Usage       string
	Description string
}

// CommandSuggestion 表示输入匹配后的命令建议。
type CommandSuggestion struct {
	Command SlashCommand
	Match   bool
}

// MatchSlashCommands 根据输入匹配可展示的 slash 命令建议。
func MatchSlashCommands(input string, slashPrefix string, commands []SlashCommand) []CommandSuggestion {
	if !strings.HasPrefix(input, slashPrefix) {
		return nil
	}

	query := strings.ToLower(strings.TrimLeft(input, " \t\r\n"))
	if IsCompleteSlashCommand(query, commands) {
		return nil
	}

	if query == slashPrefix {
		out := make([]CommandSuggestion, 0, len(commands))
		for _, command := range commands {
			out = append(out, CommandSuggestion{Command: command, Match: true})
		}
		return out
	}

	needle := strings.TrimPrefix(query, slashPrefix)
	if needle == "" {
		return nil
	}

	targets := make([]string, 0, len(commands))
	indexes := make([]int, 0, len(commands))
	for idx, command := range commands {
		normalized := strings.ToLower(strings.TrimSpace(command.Usage))
		if normalized == "" {
			continue
		}
		targets = append(targets, strings.TrimPrefix(normalized, slashPrefix))
		indexes = append(indexes, idx)
	}

	if len(targets) == 0 {
		return nil
	}

	matches := fuzzy.Find(needle, targets)
	if len(matches) == 0 {
		return nil
	}

	out := make([]CommandSuggestion, 0, len(matches))
	for _, match := range matches {
		command := commands[indexes[match.Index]]
		out = append(out, CommandSuggestion{
			Command: command,
			Match:   true,
		})
	}
	return out
}

// IsCompleteSlashCommand 判断输入是否已完整匹配某个命令。
func IsCompleteSlashCommand(input string, commands []SlashCommand) bool {
	normalizedInput := strings.ToLower(strings.TrimLeft(input, " \t\r\n"))
	if strings.TrimRight(normalizedInput, " \t\r\n") != normalizedInput {
		return false
	}
	for _, command := range commands {
		if strings.EqualFold(strings.TrimSpace(command.Usage), normalizedInput) {
			return true
		}
	}
	return false
}

// SplitFirstWord 拆分首个 token 与其后续参数。
func SplitFirstWord(input string) (string, string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ""
	}
	index := strings.IndexAny(input, " \t")
	if index < 0 {
		return input, ""
	}
	return input[:index], strings.TrimSpace(input[index+1:])
}
