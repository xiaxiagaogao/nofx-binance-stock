package kernel

import (
	"fmt"
	"sync"
	"time"

	"nofx/mcp"
)

// MockAIClient implements mcp.AIClient for chain tests. It returns scripted
// responses in order from CallWithMessages. Use NewMockAIClient + WithResponse
// to set up; Calls() returns total invocation count.
type MockAIClient struct {
	mu        sync.Mutex
	responses []string
	errors    []error
	calls     int
	lastSys   []string
	lastUser  []string
}

func NewMockAIClient() *MockAIClient {
	return &MockAIClient{}
}

func (m *MockAIClient) WithResponse(content string) *MockAIClient {
	m.responses = append(m.responses, content)
	m.errors = append(m.errors, nil)
	return m
}

func (m *MockAIClient) WithError(err error) *MockAIClient {
	m.responses = append(m.responses, "")
	m.errors = append(m.errors, err)
	return m
}

func (m *MockAIClient) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *MockAIClient) LastSystem(idx int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if idx < 0 || idx >= len(m.lastSys) {
		return ""
	}
	return m.lastSys[idx]
}

func (m *MockAIClient) LastUser(idx int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if idx < 0 || idx >= len(m.lastUser) {
		return ""
	}
	return m.lastUser[idx]
}

func (m *MockAIClient) SetAPIKey(_, _, _ string)   {}
func (m *MockAIClient) SetTimeout(_ time.Duration) {}

func (m *MockAIClient) CallWithMessages(systemPrompt, userPrompt string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastSys = append(m.lastSys, systemPrompt)
	m.lastUser = append(m.lastUser, userPrompt)
	if m.calls >= len(m.responses) {
		m.calls++
		return "", fmt.Errorf("mock: no scripted response for call #%d", m.calls)
	}
	resp, err := m.responses[m.calls], m.errors[m.calls]
	m.calls++
	return resp, err
}

func (m *MockAIClient) CallWithRequest(*mcp.Request) (string, error) { return "", nil }
func (m *MockAIClient) CallWithRequestStream(*mcp.Request, func(string)) (string, error) {
	return "", nil
}
func (m *MockAIClient) CallWithRequestFull(*mcp.Request) (*mcp.LLMResponse, error) {
	return &mcp.LLMResponse{}, nil
}
