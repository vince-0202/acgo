package memory

// MemoryType describes a kind of memory that can be extracted, stored, and
// later recalled.
type MemoryType string

const (
	// DialogueRaw stores a raw (non-summarized) user->assistant exchange.
	DialogueRaw MemoryType = "dialogue_raw"
)

// MemoryTypeHandler converts conversation fragments into a memory text + metadata.
type MemoryTypeHandler interface {
	Type() MemoryType

	// Build produces the text and any extra metadata for this memory type.
	Build(userText string, assistantText string) (text string, metadata map[string]any, err error)
}

type dialogueRawHandler struct{}

func (h *dialogueRawHandler) Type() MemoryType { return DialogueRaw }

func (h *dialogueRawHandler) Build(userText string, assistantText string) (string, map[string]any, error) {
	// Keep it simple: store the literal exchange so recall can match on any key details.
	// Later we can add a summarizing variant as another MemoryType.
	text := "User: " + userText + "\nAssistant: " + assistantText
	return text, nil, nil
}
