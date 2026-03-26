package core

import (
	"context"

	"go-llm-demo/internal/tui/services"
	"go-llm-demo/internal/tui/todo"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type todoPanelAction int

const (
	todoPanelNoop todoPanelAction = iota
	todoPanelExit
	todoPanelPromptAdd
	todoPanelRefreshed
)

type todoPanelModel struct {
	client        services.ChatClient
	todos         []services.Todo
	cursor        int
	pendingAction todoPanelAction
	lastErr       error
}

func newTodoPanelModel(client services.ChatClient) todoPanelModel {
	return todoPanelModel{
		client: client,
		todos:  make([]services.Todo, 0),
	}
}

func (p *todoPanelModel) setTodos(todos []services.Todo) {
	if todos == nil {
		todos = make([]services.Todo, 0)
	}
	p.todos = append(p.todos[:0], todos...)
	if len(p.todos) == 0 {
		p.cursor = 0
		return
	}
	if p.cursor >= len(p.todos) {
		p.cursor = len(p.todos) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

func (p *todoPanelModel) refresh() error {
	if p.client == nil {
		return nil
	}
	todos, err := p.client.GetTodoList(context.Background())
	if err != nil {
		return err
	}
	p.setTodos(todos)
	return nil
}

func (p todoPanelModel) items() []services.Todo {
	return p.todos
}

func (p todoPanelModel) selectedIndex() int {
	return p.cursor
}

func (p *todoPanelModel) focus() tea.Cmd {
	return nil
}

func (p *todoPanelModel) blur() {}

func (p *todoPanelModel) consumeAction() (todoPanelAction, error) {
	action := p.pendingAction
	err := p.lastErr
	p.pendingAction = todoPanelNoop
	p.lastErr = nil
	return action, err
}

func (p *todoPanelModel) update(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	action, err := p.handleKey(keyMsg)
	p.pendingAction = action
	p.lastErr = err
	return nil
}

func (p *todoPanelModel) handleKey(msg tea.KeyMsg) (todoPanelAction, error) {
	switch {
	case key.Matches(msg, todo.Keys.Back):
		return todoPanelExit, nil

	case key.Matches(msg, todo.Keys.Up):
		if p.cursor > 0 {
			p.cursor--
		}
		return todoPanelNoop, nil

	case key.Matches(msg, todo.Keys.Down):
		if p.cursor < len(p.todos)-1 {
			p.cursor++
		}
		return todoPanelNoop, nil

	case key.Matches(msg, todo.Keys.Done):
		if len(p.todos) == 0 {
			return todoPanelNoop, nil
		}
		selected := p.todos[p.cursor]
		nextStatus := services.TodoInProgress
		switch selected.Status {
		case services.TodoPending:
			nextStatus = services.TodoInProgress
		case services.TodoInProgress:
			nextStatus = services.TodoCompleted
		case services.TodoCompleted:
			nextStatus = services.TodoPending
		}
		if p.client == nil {
			return todoPanelNoop, nil
		}
		if err := p.client.UpdateTodoStatus(context.Background(), selected.ID, nextStatus); err != nil {
			return todoPanelNoop, err
		}
		return todoPanelRefreshed, p.refresh()

	case key.Matches(msg, todo.Keys.Delete):
		if len(p.todos) == 0 {
			return todoPanelNoop, nil
		}
		selected := p.todos[p.cursor]
		if p.client == nil {
			return todoPanelNoop, nil
		}
		if err := p.client.RemoveTodo(context.Background(), selected.ID); err != nil {
			return todoPanelNoop, err
		}
		if p.cursor >= len(p.todos)-1 && p.cursor > 0 {
			p.cursor--
		}
		return todoPanelRefreshed, p.refresh()

	case key.Matches(msg, todo.Keys.Add):
		return todoPanelPromptAdd, nil
	}

	return todoPanelNoop, nil
}
