package harness

import "context"

// SubAgentProfile describes role/capability/constraints for a sub-agent.
type SubAgentProfile struct {
	Role         string   `json:"role,omitempty"`
	RolePrompt   string   `json:"role_prompt,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	AllowedPeers []string `json:"allowed_peers,omitempty"`
	Constraints  []string `json:"constraints,omitempty"`
}

// SubAgentCreateOptions controls creation behavior for a sub-agent.
type SubAgentCreateOptions struct {
	SubID   string          `json:"sub_id"`
	Profile SubAgentProfile `json:"profile"`
}

// SubAgentInfo describes a registered sub-agent for listing and debugging.
type SubAgentInfo struct {
	SubID   string // logical key chosen at create time
	AgentID string // full agent id (unique)
	WorkDir string
	Role    string
}

// SubAgentRuntime is implemented by agent.SubAgentController so tools can drive
// sub-agents without importing the agent package (avoids import cycles).
type SubAgentRuntime interface {
	Create(subID string) (childAgentID string, err error)
	CreateWithOptions(opts SubAgentCreateOptions) (childAgentID string, err error)
	RunTask(ctx context.Context, subID, prompt string) (result string, err error)
	List() []SubAgentInfo
	GetProfile(subID string) (SubAgentProfile, bool)
	Remove(subID string) error
}
