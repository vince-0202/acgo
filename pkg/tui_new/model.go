package tui_new

import (
	"fmt"
	"regexp"
	"sync/atomic"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/vince-0202/acgo/pkg/agent_new"
	bootstrapagent "github.com/vince-0202/acgo/pkg/bootstrap/agent"
	bootsession "github.com/vince-0202/acgo/pkg/bootstrap/session"
	"github.com/vince-0202/acgo/pkg/bootstrap/setting"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/session"
)

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

type ModelOptions struct {
	Settings  *config.Settings
	SessionID string
}

type Model struct {
	agent   *agent_new.Agent
	session *session.Session

	textarea   textarea.Model
	viewport   viewport.Model
	history    []string
	autoFollow bool
	width      int
	height     int

	streamingContent  string
	streamingThinking string
	err               error

	usageIn    int
	usageOut   int
	usageTotal int

	toolPendingIdx map[string]int
	commands       map[string]commandSpec
	commandSpecs   []commandSpec

	subAgentPanels  map[string]*subAgentPanelState
	subAgentSubKeys map[string]func()
	subViewports    map[string]viewport.Model
	streamDone      *atomic.Bool
}

type commandSpec struct {
	Name    string
	Aliases []string
	Usage   string
	Help    string
	Handle  commandHandler
}

type commandHandler func(m *Model, arg string) (reply string, quit bool)

type streamEvent struct {
	Delta         string
	ThinkingDelta string
	FinalContent  string
	FinalThinking string
	ToolLine      *toolLineStreamEvent
	TurnUsage     *communi.Usage
	Done          bool
	Err           error
	Ch            chan streamEvent
	SubID         string
}

type toolLineStreamEvent struct {
	Phase      string
	ToolCallID string
	ToolName   string
	Failed     bool
}

const (
	toolLinePhaseStart = "start"
	toolLinePhaseEnd   = "end"
	panelFrameExtra    = 3
)

type subAgentPanelState struct {
	history           []string
	streamingContent  string
	streamingThinking string
	toolPendingIdx    map[string]int
}

func NewModel(opts *ModelOptions) (*Model, error) {
	if opts == nil || opts.Settings == nil {
		return nil, fmt.Errorf("settings are required")
	}

	agent, _, err := bootstrapagent.BuildAgentRuntime(
		opts.Settings,
		bootstrapagent.WithId("tui-new"),
		bootstrapagent.WithDefaultTools(),
	)
	if err != nil {
		return nil, err
	}
	sess, err := bootsession.LoadSession(opts.SessionID, opts.Settings.Session)
	if err != nil {
		return nil, err
	}

	ta := textarea.New()
	ta.Placeholder = "Ask acgo..."
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.Focus()

	model := &Model{
		agent:           agent,
		session:         sess,
		textarea:        ta,
		viewport:        viewport.New(78, 16),
		autoFollow:      true,
		width:           detectTerminalWidth(80),
		height:          24,
		toolPendingIdx:  make(map[string]int),
		commands:        make(map[string]commandSpec),
		subAgentPanels:  make(map[string]*subAgentPanelState),
		subAgentSubKeys: make(map[string]func()),
		subViewports:    make(map[string]viewport.Model),
	}
	if len(sess.Message) > 0 {
		model.agent.ContextManager().ReplaceMessages(sess.Message)
		model.history = append(model.history, renderHistoryFromMessages(sess.Message)...)
	}
	model.registerBuiltinCommands()
	return model, nil
}

func Run(sessionID string) error {
	settings, err := setting.LoadAndRuntimeInit()
	if err != nil {
		return err
	}
	model, err := NewModel(&ModelOptions{Settings: settings, SessionID: sessionID})
	if err != nil {
		return err
	}
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = program.Run()
	return err
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, initialWindowSizeCmd())
}
