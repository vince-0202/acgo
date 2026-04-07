package harness

import "context"

// SubAgentInfo describes a registered sub-agent for listing and debugging.
type SubAgentInfo struct {
	SubID   string // logical key chosen at create time
	AgentID string // full agent id (unique)
	WorkDir string
}

// SubAgentRuntime is implemented by agent.SubAgentController so tools can drive
// sub-agents without importing the agent package (avoids import cycles).
type SubAgentRuntime interface {
	Create(subID string) (childAgentID string, err error)
	RunTask(ctx context.Context, subID, prompt string) (result string, err error)
	List() []SubAgentInfo
	Remove(subID string) error
}
