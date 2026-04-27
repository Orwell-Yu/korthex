package agent

import (
	"fmt"
	"strings"
	"sync"

	"github.com/Orwell-Yu/korthex/internal/llm"
)

// HistoryManager manages conversation history with FIFO truncation and tool result compression.
// All methods are safe for concurrent use.
type HistoryManager struct {
	mu           sync.RWMutex
	systemPrompt string
	maxTurns     int
	turns        []turn // each turn = user + assistant + tool results
}

// turn represents one conversation turn (user query → assistant response → tool results).
type turn struct {
	messages []llm.Message
}

// NewHistoryManager creates a HistoryManager with the given max turns and system prompt.
func NewHistoryManager(maxTurns int, systemPrompt string) *HistoryManager {
	return &HistoryManager{
		systemPrompt: systemPrompt,
		maxTurns:     maxTurns,
	}
}

// AppendUserMessage starts a new turn with a user message.
func (h *HistoryManager) AppendUserMessage(content string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.turns = append(h.turns, turn{
		messages: []llm.Message{
			{Role: llm.RoleUser, Content: content},
		},
	})
	h.truncate()
}

// AppendAssistantMessage appends an assistant message to the current turn.
func (h *HistoryManager) AppendAssistantMessage(msg llm.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.turns) == 0 {
		h.turns = append(h.turns, turn{})
	}
	h.turns[len(h.turns)-1].messages = append(h.turns[len(h.turns)-1].messages, msg)
}

// AppendToolResult appends a tool result to the current turn, with compression if needed.
func (h *HistoryManager) AppendToolResult(toolCallID, toolName, result string) {
	compressed := compressToolResult(result)
	msg := llm.Message{
		Role:       llm.RoleTool,
		Content:    compressed,
		ToolCallID: toolCallID,
		Name:       toolName,
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.turns) == 0 {
		h.turns = append(h.turns, turn{})
	}
	h.turns[len(h.turns)-1].messages = append(h.turns[len(h.turns)-1].messages, msg)
}

// GetMessages returns system prompt + recent turn messages.
func (h *HistoryManager) GetMessages() []llm.Message {
	h.mu.RLock()
	defer h.mu.RUnlock()
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: h.systemPrompt},
	}
	for _, t := range h.turns {
		msgs = append(msgs, t.messages...)
	}
	return msgs
}

// ClearHistory resets conversation history, keeping the system prompt.
func (h *HistoryManager) ClearHistory() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.turns = nil
}

// SetSystemPrompt updates the system prompt.
func (h *HistoryManager) SetSystemPrompt(prompt string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.systemPrompt = prompt
}

// truncate applies FIFO eviction to keep at most maxTurns turns.
func (h *HistoryManager) truncate() {
	if h.maxTurns > 0 && len(h.turns) > h.maxTurns {
		h.turns = h.turns[len(h.turns)-h.maxTurns:]
	}
}

const (
	compressThreshold = 2000
	compressKeep      = 500
)

// compressToolResult truncates results exceeding compressThreshold to compressKeep chars + stats.
func compressToolResult(result string) string {
	if len(result) <= compressThreshold {
		return result
	}

	totalLines := strings.Count(result, "\n") + 1
	errorCount := strings.Count(strings.ToUpper(result), "ERROR")
	warnCount := strings.Count(strings.ToUpper(result), "WARN")

	truncated := result[:compressKeep]
	stats := fmt.Sprintf("\n\n[TRUNCATED] Total: %d lines, %d chars | ERROR: %d, WARN: %d",
		totalLines, len(result), errorCount, warnCount)

	return truncated + stats
}
