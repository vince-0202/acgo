package harness

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/llm"
)

const compactSystemAgentSDK = "You are a Claude agent, built on Anthropic's Claude Agent SDK."

const compactSystemSummarizer = "You are a helpful AI assistant tasked with summarizing conversations."

const continuationPrefix = "This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.\n\n"

// compactRubricUser is the English instruction block for structured development-oriented summarization (Claude Code /compact style).
const compactRubricUser = `Your task is to create a detailed summary of the conversation so far, paying close attention to the user's explicit requests and your previous actions.
This summary should be thorough in capturing technical details, code patterns, and architectural decisions that would be essential for continuing development work without losing context.

Before providing your final summary, wrap your analysis in <analysis> tags to organize your thoughts and ensure you've covered all necessary points. In your analysis process:

1. Chronologically analyze each message and section of the conversation. For each section thoroughly identify:
   - The user's explicit requests and intents
   - Your approach to addressing the user's requests
   - Key decisions, technical concepts and code patterns
   - Specific details like:
     - file names
     - full code snippets
     - function signatures
     - file edits
   - Errors that you ran into and how you fixed them
   - Pay special attention to specific user feedback that you received, especially if the user told you to do something differently.
2. Double-check for technical accuracy and completeness, addressing each required element thoroughly.

Your summary should include the following sections:

1. Primary Request and Intent: Capture all of the user's explicit requests and intents in detail
2. Key Technical Concepts: List all important technical concepts, technologies, and frameworks discussed.
3. Files and Code Sections: Enumerate specific files and code sections examined, modified, or created. Pay special attention to the most recent messages and include full code snippets where applicable and include a summary of why this file read or edit is important.
4. Errors and fixes: List all errors that you ran into, and how you fixed them. Pay special attention to specific user feedback that you received, especially if the user told you to do something differently.
5. Problem Solving: Document problems solved and any ongoing troubleshooting efforts.
6. All user messages: List ALL user messages that are not tool results. These are critical for understanding the users' feedback and changing intent.
7. Pending Tasks: Outline any pending tasks that you have explicitly been asked to work on.
8. Current Work: Describe in detail precisely what was being worked on immediately before this summary request, paying special attention to the most recent messages from both user and assistant. Include file names and code snippets where applicable.
9. Optional Next Step: List the next step that you will take that is related to the most recent work you were doing. IMPORTANT: ensure that this step is DIRECTLY in line with the user's most recent explicit requests, and the task you were working on immediately before this summary request.

Please provide your summary based on the conversation so far, following this structure and ensuring precision and thoroughness in your response.

There may be additional summarization instructions provided in the included context. If so, remember to follow these instructions when creating the above summary.
`

var (
	reAnalysis = regexp.MustCompile(`(?s)<analysis>(.*?)</analysis>`)
	reSummary  = regexp.MustCompile(`(?s)<summary>(.*?)</summary>`)
)

func (cc *ContextController) hasCompactLLM() bool {
	if cc.agent == nil {
		return false
	}
	model := cc.agent.Model()
	return strings.TrimSpace(model.Provider) != "" && strings.TrimSpace(model.ID) != ""
}

func (cc *ContextController) compactWithLLM(ctx context.Context, req *TrimMessageOptions) error {
	if !cc.hasCompactLLM() {
		return fmt.Errorf("compact: LLM provider or model not configured")
	}
	prefix, body := splitLeadingSystem(cc.context.MessageSnapshot())
	var msgs []communi.Message
	msgs = append(msgs, communi.NewSystemMessageWithoutId(compactSystemAgentSDK))
	msgs = append(msgs, communi.NewSystemMessageWithoutId(compactSystemSummarizer))
	msgs = append(msgs, body...)

	rubric := compactRubricUser
	if req != nil && strings.TrimSpace(req.ExtraInstructions) != "" {
		rubric += "\n\n## Additional instructions\n" + strings.TrimSpace(req.ExtraInstructions) + "\n"
	}
	msgs = append(msgs, communi.NewUserMessageWithoutId(rubric))

	opts := &llm.Options{
		ToolChoice:      "none",
		MaxOutputTokens: 20000,
	}
	assistant, _, err := cc.agent.Provider().Complete(ctx, cc.agent.Model(), msgs, opts)
	if err != nil {
		return err
	}
	text := assistant.ContentBlocksToText()
	tp := strings.TrimSpace(cc.transcriptPath)
	if req != nil && strings.TrimSpace(req.TranscriptPath) != "" {
		tp = strings.TrimSpace(req.TranscriptPath)
	}
	processed := postProcessCompactOutput(text, tp)

	sumMsg := communi.NewUserMessageWithoutId(processed)
	cc.context.ReplaceMessages(append(append([]communi.Message(nil), prefix...), sumMsg))
	return nil
}

func postProcessCompactOutput(raw, transcriptPath string) string {
	aMatch := reAnalysis.FindStringSubmatch(raw)
	sMatch := reSummary.FindStringSubmatch(raw)
	var b strings.Builder
	b.WriteString(continuationPrefix)
	if len(aMatch) > 1 {
		b.WriteString("Analysis:\n")
		b.WriteString(strings.TrimSpace(aMatch[1]))
		b.WriteString("\n\n")
	}
	if len(sMatch) > 1 {
		b.WriteString("Summary:\n")
		b.WriteString(strings.TrimSpace(sMatch[1]))
	} else {
		t := reAnalysis.ReplaceAllString(raw, "")
		t = reSummary.ReplaceAllString(t, "")
		t = strings.TrimSpace(t)
		if t != "" {
			b.WriteString("Summary:\n")
			b.WriteString(t)
		} else {
			b.WriteString(strings.TrimSpace(raw))
		}
	}
	if transcriptPath != "" {
		b.WriteString("\n\nIf you need specific details from before compaction (like exact code snippets, error messages, or content you generated), read the full transcript at: ")
		b.WriteString(transcriptPath)
	}
	return b.String()
}
