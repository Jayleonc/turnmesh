package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Jayleonc/turnmesh/internal/agent"
)

type researchInput struct {
	Topic string
	Steps []string
}

func main() {
	ctx := context.Background()

	runtime := agent.NewAgentRuntime(agent.RunnerFunc(runLocalSubagent))

	task, events, err := runtime.Start(ctx, agent.StartRequest{
		TaskID: "example-subagent-1",
		Definition: agent.Definition{
			ID:           "subagent.learning.researcher",
			Name:         "Local Research Subagent",
			Description:  "Demonstrates the task lifecycle without calling a real model.",
			SystemPrompt: "Break the topic into steps and report progress.",
			Background:   true,
			Isolated:     true,
			Metadata: map[string]string{
				"example": "subagent",
			},
		},
		Input: researchInput{
			Topic: "turnmesh subagent runtime",
			Steps: []string{
				"read task definition",
				"prepare isolated context",
				"emit progress events",
				"return final summary",
			},
		},
		Context: agent.TaskContext{
			SessionID:  "example-session",
			Background: true,
			Isolated:   true,
			Metadata: map[string]string{
				"source": "examples/subagent",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("started task: %s\n", task.ID())

	for {
		event := <-events
		printEvent(event)

		switch event.Type {
		case agent.EventTypeCompleted, agent.EventTypeFailed, agent.EventTypeStopped:
			snapshot, err := runtime.GetTask(ctx, task.ID())
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("final status: %s, progress: %.0f%%, summary: %s\n", snapshot.Status, snapshot.Progress*100, snapshot.Summary)
			return
		}
	}
}

func runLocalSubagent(ctx context.Context, req agent.RunRequest, emit func(agent.Event)) error {
	input, ok := req.Input.(researchInput)
	if !ok {
		input = researchInput{Topic: fmt.Sprint(req.Input)}
	}
	if len(input.Steps) == 0 {
		input.Steps = []string{"inspect input", "produce output"}
	}

	for i, step := range input.Steps {
		if err := ctx.Err(); err != nil {
			return err
		}

		progress := float64(i+1) / float64(len(input.Steps))
		emit(agent.Event{
			Type: agent.EventTypeProgress,
			Payload: agent.ProgressEvent{
				Progress: progress,
				Summary:  step,
			},
			Metadata: map[string]string{
				"agent_id": req.Definition.ID,
				"step":     fmt.Sprintf("%d", i+1),
			},
		})
		emit(agent.Event{
			Type:    agent.EventTypeMessage,
			Payload: fmt.Sprintf("[%s] %s", input.Topic, step),
			Metadata: map[string]string{
				"agent_id": req.Definition.ID,
			},
		})

		time.Sleep(80 * time.Millisecond)
	}

	return nil
}

func printEvent(event agent.Event) {
	switch payload := event.Payload.(type) {
	case agent.ProgressEvent:
		fmt.Printf("event=%s progress=%.0f%% summary=%s\n", event.Type, payload.Progress*100, payload.Summary)
	case agent.StatusChange:
		fmt.Printf("event=%s status=%s->%s reason=%s\n", event.Type, payload.From, payload.To, payload.Reason)
	case agent.TaskSnapshot:
		fmt.Printf("event=%s task=%s status=%s\n", event.Type, payload.ID, payload.Status)
	case string:
		fmt.Printf("event=%s message=%s\n", event.Type, payload)
	default:
		text := strings.TrimSpace(fmt.Sprint(payload))
		if text == "" {
			text = "<nil>"
		}
		fmt.Printf("event=%s payload=%s\n", event.Type, text)
	}
}
