package tui

import (
	"fmt"
	"regexp"
	"sync/atomic"

	"github.com/vince-0202/acgo/pkg/agent"
	bootstrapagent "github.com/vince-0202/acgo/pkg/bootstrap/agent"
	bootstrapharness "github.com/vince-0202/acgo/pkg/bootstrap/harness"
	bootsession "github.com/vince-0202/acgo/pkg/bootstrap/session"
	"github.com/vince-0202/acgo/pkg/bootstrap/setting"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/harness"
	"github.com/vince-0202/acgo/pkg/session"
)

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

type ModelOptions struct {
	Settings  *config.Settings
	SessionID string
}

type Model struct {
	agent   *agent.Agent
	harness *harness.Harness
	session *session.Session

	history []string

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
	streamDone      *atomic.Bool

	mainScrollLocked bool
	subScrollLocked  bool
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

	defaultHarness := bootstrapharness.BuildDefaultHarness()
	agent, err := bootstrapagent.BuildAgent(
		opts.Settings,
		bootstrapagent.WithId("tui-new"),
		bootstrapagent.WithDefaultToolsExcept("rag_search"),
	)
	if err != nil {
		return nil, err
	}
	if err := defaultHarness.Attach(agent); err != nil {
		return nil, err
	}
	sess, err := bootsession.LoadSession(opts.SessionID, opts.Settings.Session)
	if err != nil {
		return nil, err
	}

	model := &Model{
		agent:            agent,
		harness:          defaultHarness,
		session:          sess,
		toolPendingIdx:   make(map[string]int),
		commands:         make(map[string]commandSpec),
		subAgentPanels:   make(map[string]*subAgentPanelState),
		subAgentSubKeys:  make(map[string]func()),
		mainScrollLocked: false,
		subScrollLocked:  false,
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
	return runWithTView(model)
}
