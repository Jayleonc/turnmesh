package main

import (
	"testing"

	"github.com/Jayleonc/turnmesh/internal/agent"
	"github.com/Jayleonc/turnmesh/internal/core"
	"github.com/Jayleonc/turnmesh/internal/orchestrator"
)

func TestTurnInputFromAgentRunPreservesAgentMetadataForCoreTurnInput(t *testing.T) {
	input := turnInputFromAgentRun(agent.RunRequest{
		TaskID: "task-1",
		Definition: agent.Definition{
			ID: "agent-1",
		},
		Input: core.TurnInput{
			SessionID: "session-1",
			Metadata: map[string]string{
				"source": "payload",
			},
		},
		Context: agent.TaskContext{
			ParentTaskID: "parent-1",
			Metadata: map[string]string{
				"ctx": "1",
			},
		},
	})

	if input.Metadata["task_id"] != "task-1" {
		t.Fatalf("task_id = %q, want task-1", input.Metadata["task_id"])
	}
	if input.Metadata["agent_id"] != "agent-1" {
		t.Fatalf("agent_id = %q, want agent-1", input.Metadata["agent_id"])
	}
	if input.Metadata["parent_task_id"] != "parent-1" {
		t.Fatalf("parent_task_id = %q, want parent-1", input.Metadata["parent_task_id"])
	}
	if input.Metadata["source"] != "payload" || input.Metadata["ctx"] != "1" {
		t.Fatalf("metadata merge failed: %#v", input.Metadata)
	}
}

func TestTurnTranscriptPrefersRequestedMessagesPlusEvents(t *testing.T) {
	report := orchestrator.TurnReport{
		Requested: core.TurnInput{
			Messages: []core.Message{
				{Role: core.MessageRoleUser, Content: "user prompt"},
			},
		},
		Prepared: core.TurnInput{
			Messages: []core.Message{
				{Role: core.MessageRoleUser, Content: "user prompt"},
				{Role: core.MessageRoleAssistant, Content: "tool continuation"},
			},
		},
		Events: []core.TurnEvent{
			{
				Kind:    core.TurnEventMessage,
				Message: &core.Message{Role: core.MessageRoleAssistant, Content: "tool continuation"},
			},
		},
	}

	transcript := turnTranscript(report)
	if len(transcript) != 2 {
		t.Fatalf("len(transcript) = %d, want 2 without duplication", len(transcript))
	}
	if transcript[0].Content != "user prompt" || transcript[1].Content != "tool continuation" {
		t.Fatalf("transcript = %#v, want requested + event messages", transcript)
	}
}
