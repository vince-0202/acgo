package keys

// AgentMessageRole describes high-level roles, including custom ones like notification.
type AgentMessageRole string

const (
	AgentRoleSystem       AgentMessageRole = "system"
	AgentRoleUser         AgentMessageRole = "user"
	AgentRoleAssistant    AgentMessageRole = "assistant"
	AgentRoleTool         AgentMessageRole = "tool"
	AgentRoleNotification AgentMessageRole = "notification"
)
