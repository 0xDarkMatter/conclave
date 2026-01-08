package tui

import (
	"time"
)

// Phase represents the current execution phase
type Phase int

const (
	PhaseQuerying Phase = iota
	PhaseSynthesizing
	PhaseComplete
)

// Status represents a provider's execution status
type Status int

const (
	StatusPending Status = iota
	StatusRunning
	StatusSuccess
	StatusError
)

// ProviderInfo holds display info for a provider
type ProviderInfo struct {
	Name        string // short name like "claude"
	DisplayName string // full name like "Anthropic Claude Opus 4.5"
}

// ProviderState tracks the current state of a provider
type ProviderState struct {
	Info      ProviderInfo
	Status    Status
	StartTime time.Time
	Duration  time.Duration
	Tokens    int
	Error     error
}

// --- Messages ---

// ProviderStartMsg is sent when a provider starts querying
type ProviderStartMsg struct {
	Provider string
}

// ProviderDoneMsg is sent when a provider completes
type ProviderDoneMsg struct {
	Provider string
	Duration time.Duration
	Tokens   int
	Error    error
}

// SynthesisStartMsg is sent when synthesis begins
type SynthesisStartMsg struct{}

// SynthesisDoneMsg is sent when synthesis completes
type SynthesisDoneMsg struct {
	Tokens int
	Error  error
}

// VerbTickMsg triggers verb rotation during synthesis
type VerbTickMsg struct{}

// CompleteMsg signals that all work is done
type CompleteMsg struct {
	TotalTokens int
}

// QuitMsg signals the program should exit
type QuitMsg struct{}
