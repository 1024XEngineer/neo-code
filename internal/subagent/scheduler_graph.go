package subagent

import (
	"fmt"
	"slices"
	"strings"

	agentsession "neo-code/internal/session"
)

type taskNode struct {
	todo       agentsession.TodoItem
	dependents []string
}

// taskGraph 保存调度时的 Todo DAG 快照，供就绪判定和依赖推进复用。
type taskGraph struct {
	nodes map[string]*taskNode
	order []string
}

// buildTaskGraph 从当前 Todo 列表构建 DAG，并校验依赖合法性和无环约束。
func buildTaskGraph(items []agentsession.TodoItem) (*taskGraph, error) {
	graph := &taskGraph{
		nodes: make(map[string]*taskNode, len(items)),
		order: make([]string, 0, len(items)),
	}
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			return nil, errorsf("scheduler graph contains empty todo id")
		}
		if _, exists := graph.nodes[id]; exists {
			return nil, errorsf("scheduler graph contains duplicate todo id %q", id)
		}
		graph.nodes[id] = &taskNode{todo: item.Clone()}
		graph.order = append(graph.order, id)
	}
	for _, id := range graph.order {
		node := graph.nodes[id]
		for _, dep := range node.todo.Dependencies {
			dependencyID := strings.TrimSpace(dep)
			dependency, ok := graph.nodes[dependencyID]
			if !ok {
				return nil, errorsf("scheduler graph todo %q references unknown dependency %q", id, dep)
			}
			dependency.dependents = append(dependency.dependents, id)
		}
	}
	if err := detectTaskCycle(graph); err != nil {
		return nil, err
	}
	return graph, nil
}

// detectTaskCycle 使用 Kahn 算法检测图中环路，避免调度阶段死锁。
func detectTaskCycle(graph *taskGraph) error {
	inDegree := make(map[string]int, len(graph.nodes))
	for _, id := range graph.order {
		inDegree[id] = 0
	}
	for _, id := range graph.order {
		node := graph.nodes[id]
		for _, dep := range node.todo.Dependencies {
			inDegree[id]++
			if strings.TrimSpace(dep) == id {
				return fmt.Errorf("%w: %q", agentsession.ErrCyclicDependency, id)
			}
		}
	}

	queue := make([]string, 0, len(graph.order))
	for _, id := range graph.order {
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}

	visited := 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		visited++

		for _, dependentID := range graph.nodes[current].dependents {
			inDegree[dependentID]--
			if inDegree[dependentID] == 0 {
				queue = append(queue, dependentID)
			}
		}
	}
	if visited == len(graph.order) {
		return nil
	}

	cycleNodes := make([]string, 0)
	for _, id := range graph.order {
		if inDegree[id] > 0 {
			cycleNodes = append(cycleNodes, id)
		}
	}
	slices.Sort(cycleNodes)
	return fmt.Errorf("%w: %s", agentsession.ErrCyclicDependency, strings.Join(cycleNodes, ", "))
}
