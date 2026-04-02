package agent_test

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/hankwenyx/claude-code-go/pkg/agent"
	"github.com/hankwenyx/claude-code-go/pkg/api"
	"github.com/hankwenyx/claude-code-go/pkg/tools"
	"github.com/hankwenyx/claude-code-go/pkg/tools/bash"
	"github.com/hankwenyx/claude-code-go/pkg/tools/fileread"
	"github.com/hankwenyx/claude-code-go/pkg/tools/glob"
	"github.com/hankwenyx/claude-code-go/pkg/tools/grep"
)

// ExampleRunAgentSync shows the simplest way to use the library: one-shot sync call.
func ExampleRunAgentSync() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fmt.Println("hello back")
		return
	}

	ctx := context.Background()
	text, err := agent.RunAgentSync(ctx, "say exactly: hello back", agent.AgentOptions{
		APIKey: apiKey,
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(strings.TrimSpace(text))
	// Output: hello back
}

// ExampleRunAgent shows streaming event consumption.
func ExampleRunAgent() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		// Not a real API call — demonstrate the event loop structure only.
		fmt.Println("text: hello")
		fmt.Println("done")
		return
	}

	ctx := context.Background()
	events := agent.RunAgent(ctx, "say exactly: hello", agent.AgentOptions{
		APIKey: apiKey,
	})

	var sb strings.Builder
	for event := range events {
		switch event.Type {
		case agent.EventText:
			sb.WriteString(event.Text)
		case agent.EventError:
			fmt.Println("error:", event.Error)
			return
		}
	}

	fmt.Println("text:", strings.TrimSpace(sb.String()))
	fmt.Println("done")
	// Output:
	// text: hello
	// done
}

// ExampleRunAgent_withTools shows how to register tools.
func ExampleRunAgent_withTools() {
	cwd, _ := os.Getwd()
	stateStore := fileread.NewStateStore()

	registry := tools.NewRegistry()
	registry.Register(bash.New())
	registry.Register(fileread.New(stateStore))
	registry.Register(glob.New(cwd))
	registry.Register(grep.New(cwd))

	sysBlocks := api.BuildSystemPrompt(api.BuildOptions{
		CWD:          cwd,
		EnabledTools: []string{"Bash", "Read", "Glob", "Grep"},
	})

	opts := agent.AgentOptions{
		APIKey:       os.Getenv("ANTHROPIC_API_KEY"),
		CWD:          cwd,
		SystemPrompt: sysBlocks,
		Registry:     registry,
	}

	ctx := context.Background()
	events := agent.RunAgent(ctx, "list .go files in the current directory", opts)

	for event := range events {
		switch event.Type {
		case agent.EventText:
			fmt.Print(event.Text)
		case agent.EventToolUse:
			fmt.Fprintf(os.Stderr, "[%s]\n", event.ToolCall.Name)
		case agent.EventError:
			fmt.Fprintln(os.Stderr, "error:", event.Error)
		}
	}
}
