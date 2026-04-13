package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Jayleonc/turnmesh/internal/feedback"
)

func main() {
	runtime, err := BuildRuntime(context.Background(), Config{
		Sink: feedback.NewStdoutSink(os.Stdout),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "engine assembly failed: %v\n", err)
		os.Exit(1)
	}
	defer runtime.Close()

	if err := runtime.Engine.Boot(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "engine bootstrap failed: %v\n", err)
		os.Exit(1)
	}
}
