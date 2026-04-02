package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/hankwenyx/claude-code-go/pkg/agent"
	"github.com/hankwenyx/claude-code-go/pkg/api"
	"github.com/hankwenyx/claude-code-go/pkg/config"
	"github.com/hankwenyx/claude-code-go/pkg/permissions"
	"github.com/hankwenyx/claude-code-go/pkg/tools"
	"github.com/hankwenyx/claude-code-go/pkg/tools/bash"
	"github.com/hankwenyx/claude-code-go/pkg/tools/fileedit"
	"github.com/hankwenyx/claude-code-go/pkg/tools/fileread"
	"github.com/hankwenyx/claude-code-go/pkg/tools/filewrite"
	"github.com/hankwenyx/claude-code-go/pkg/tools/glob"
	"github.com/hankwenyx/claude-code-go/pkg/tools/grep"
	"github.com/hankwenyx/claude-code-go/pkg/tools/webfetch"
)

var (
	model     string
	maxTokens int
	apiKey    string
	noTools   bool
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "claude [message]",
	Short: "Claude Code CLI - AI-powered coding assistant",
	Long: `Claude Code CLI is an AI-powered coding assistant that helps you with
software engineering tasks. It can read files, edit code, run commands,
and more.

Examples:
  claude "hello"
  claude "list .go files"
  echo "what is this project?" | claude`,
	Args: cobra.MaximumNArgs(1),
	Run:  run,
}

func init() {
	rootCmd.Flags().StringVarP(&model, "model", "m", "", "Model to use (default: claude-sonnet-4-6)")
	rootCmd.Flags().IntVarP(&maxTokens, "max-tokens", "t", 0, "Maximum output tokens (default: 4096)")
	rootCmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "Anthropic API key (default: from settings or env)")
	rootCmd.Flags().BoolVar(&noTools, "no-tools", false, "Disable all tools (single-turn text only)")
}

func run(cmd *cobra.Command, args []string) {
	// Get working directory
	cwd, _ := os.Getwd()

	// Load settings first — env block is applied immediately so downstream
	// code (including SDK env fallbacks) sees ANTHROPIC_AUTH_TOKEN / BASE_URL etc.
	settings, _ := config.LoadSettings(cwd)
	if settings != nil {
		for k, v := range settings.Env {
			os.Setenv(k, v)
		}
	}

	// Get API key: --api-key flag > settings.env > ANTHROPIC_AUTH_TOKEN > ANTHROPIC_API_KEY env > .credentials.json
	key := config.LoadAPIKey(apiKey)
	if key == "" && settings != nil {
		key = settings.APIKey()
	}
	if key == "" {
		fmt.Fprintln(os.Stderr, "Error: no API key found.\n  Set ANTHROPIC_AUTH_TOKEN or ANTHROPIC_API_KEY, or use --api-key.")
		os.Exit(1)
	}

	// Get message
	var message string
	if len(args) > 0 {
		message = args[0]
	} else {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			scanner := bufio.NewScanner(os.Stdin)
			var lines []string
			for scanner.Scan() {
				lines = append(lines, scanner.Text())
			}
			message = strings.Join(lines, "\n")
		}
	}
	if message == "" {
		fmt.Fprintln(os.Stderr, "Error: no message provided")
		os.Exit(1)
	}

	// Build API client options from settings
	var clientOpts []api.ClientOption
	if settings != nil {
		if u := settings.APIBaseURL(); u != "" {
			clientOpts = append(clientOpts, api.WithBaseURL(u))
		}
		if h := settings.CustomHeaders(); len(h) > 0 {
			clientOpts = append(clientOpts, api.WithCustomHeaders(h))
		}
	}

	// Load CLAUDE.md files
	claudeMds, _ := config.LoadClaudeMds(cwd)
	claudeMdContent := ""
	if len(claudeMds) > 0 {
		claudeMdContent = config.FormatClaudeMdMessage(claudeMds, config.CurrentDate())
	}

	// Build tool registry
	registry := tools.NewRegistry()
	if !noTools {
		stateStore := fileread.NewStateStore()
		registry.Register(fileread.New(stateStore))
		registry.Register(fileedit.New(stateStore))
		registry.Register(filewrite.New(stateStore))
		registry.Register(bash.New())
		registry.Register(glob.New(cwd))
		registry.Register(grep.New(cwd))
		registry.Register(webfetch.New())
	}

	// Build permission checker
	var permRules config.PermissionRules
	if settings != nil {
		permRules = config.ParsePermissionRules(settings.Permissions)
	}
	permMode := permissions.ModeAuto
	if settings != nil && settings.Permissions.DefaultMode != "" {
		permMode = permissions.Mode(settings.Permissions.DefaultMode)
	}
	permChecker := &permissions.Checker{
		Mode:           permMode,
		Rules:          permRules,
		CWD:            cwd,
		NonInteractive: true,
	}
	if settings != nil {
		permChecker.AdditlDirs = settings.Permissions.AdditionalDirs
	}

	// Build system prompt
	var toolNames []string
	for _, t := range registry.All() {
		toolNames = append(toolNames, t.Name())
	}
	sysBlocks := api.BuildSystemPrompt(api.BuildOptions{
		CWD:          cwd,
		EnabledTools: toolNames,
	})

	// Build agent options
	opts := agent.AgentOptions{
		APIKey:          key,
		CWD:             cwd,
		SystemPrompt:    sysBlocks,
		ClaudeMdContent: claudeMdContent,
		Registry:        registry,
		PermChecker:     permChecker,
		NonInteractive:  true,
	}
	// Priority: --model flag > ANTHROPIC_MODEL env (from settings) > settings.model field > default
	if model != "" {
		opts.Model = model
	} else if settings != nil {
		if m := settings.Env["ANTHROPIC_MODEL"]; m != "" {
			opts.Model = m
		} else if settings.Model != "" {
			parsedModel, parsedTokens := settings.ParsedModel()
			opts.Model = parsedModel
			if parsedTokens > 0 && maxTokens == 0 {
				opts.MaxTokens = parsedTokens
			}
		}
	}
	if maxTokens > 0 {
		opts.MaxTokens = maxTokens // --max-tokens flag always wins
	}

	// Create client with settings-derived options and attach to agent
	opts.Client = api.NewClient(key, append(clientOpts,
		api.WithModel(opts.Model),
	)...)

	// Run agent
	events := agent.RunAgent(cmd.Context(), message, opts)

	for event := range events {
		switch event.Type {
		case agent.EventText:
			fmt.Print(event.Text)
		case agent.EventToolUse:
			fmt.Fprintf(os.Stderr, "\n  > %s %s", event.ToolCall.Name, toolSummary(event.ToolCall.Name, event.ToolCall.Input))
		case agent.EventToolResult:
			if event.ToolResult.IsError {
				// Trim the verbose permission prefix to keep it short
				msg := event.ToolResult.Content
				msg = strings.TrimPrefix(msg, "permission denied: ")
				fmt.Fprintf(os.Stderr, "  [denied: %s]\n", msg)
			} else {
				fmt.Fprintln(os.Stderr) // end the tool line
			}
		case agent.EventError:
			fmt.Fprintf(os.Stderr, "\nError: %v\n", event.Error)
			os.Exit(1)
		case agent.EventMessage:
			// Done
		}
	}

	fmt.Println() // Final newline
}

// toolSummary extracts a short human-readable description from a tool's JSON input.
func toolSummary(name string, input []byte) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(input, &m); err != nil {
		return ""
	}
	// Pick the most relevant field per tool
	keys := map[string]string{
		"Bash":     "command",
		"Read":     "file_path",
		"Write":    "file_path",
		"Edit":     "file_path",
		"Glob":     "pattern",
		"Grep":     "pattern",
		"WebFetch": "url",
	}
	key, ok := keys[name]
	if !ok {
		// fallback: first string field
		for _, v := range m {
			var s string
			if json.Unmarshal(v, &s) == nil {
				return truncate(s, 80)
			}
		}
		return ""
	}
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return truncate(s, 80)
}

func truncate(s string, n int) string {
	// collapse newlines for single-line display
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
