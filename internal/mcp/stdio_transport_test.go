package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStdioTransportRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	helperSrc := `package main

import (
	"bufio"
	"encoding/json"
	"os"
)

type message struct {
	JSONRPC string          ` + "`json:\"jsonrpc,omitempty\"`" + `
	ID      any             ` + "`json:\"id,omitempty\"`" + `
	Method  string          ` + "`json:\"method,omitempty\"`" + `
	Params  json.RawMessage ` + "`json:\"params,omitempty\"`" + `
}

func main() {
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	enc := json.NewEncoder(os.Stdout)
	for {
		var msg message
		if err := dec.Decode(&msg); err != nil {
			return
		}
		if msg.Method == "tools/list" {
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      msg.ID,
				"result": map[string]any{
					"tools": []map[string]any{{"name": "echo"}},
				},
			})
		}
	}
}
`

	helperFile := filepath.Join(dir, "helper.go")
	if err := os.WriteFile(helperFile, []byte(helperSrc), 0o600); err != nil {
		t.Fatalf("write helper: %v", err)
	}
	bin := filepath.Join(dir, "helper-bin")
	build := exec.Command("go", "build", "-o", bin, helperFile)
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, out)
	}

	transport := NewStdioTransport(StdioConfig{Command: bin})
	if err := transport.Open(ctx); err != nil {
		t.Fatalf("open transport: %v", err)
	}
	defer transport.Close(ctx)

	if err := transport.Send(ctx, Message{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	resp, err := transport.Recv(ctx)
	if err != nil {
		t.Fatalf("recv: %v", err)
	}
	if resp.JSONRPC != "2.0" {
		t.Fatalf("unexpected jsonrpc version: %#v", resp)
	}
	if resp.Method != "" {
		t.Fatalf("unexpected response method: %#v", resp)
	}
	if resp.ID == nil {
		t.Fatalf("missing response id: %#v", resp)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if string(raw) == "null" {
		t.Fatalf("missing result: %#v", resp)
	}
}
