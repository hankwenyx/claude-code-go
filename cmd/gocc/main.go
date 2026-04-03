package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"

	"github.com/hankwenyx/claude-code-go/cmd/gocc/tui"
	"github.com/hankwenyx/claude-code-go/pkg/agent"
	"github.com/hankwenyx/claude-code-go/pkg/api"
	"github.com/hankwenyx/claude-code-go/pkg/config"
	"github.com/hankwenyx/claude-code-go/pkg/hooks"
	"github.com/hankwenyx/claude-code-go/pkg/mcp"
	"github.com/hankwenyx/claude-code-go/pkg/memory"
	"github.com/hankwenyx/claude-code-go/pkg/permissions"
	"github.com/hankwenyx/claude-code-go/pkg/session"
	"github.com/hankwenyx/claude-code-go/pkg/tools"
	"github.com/hankwenyx/claude-code-go/pkg/tools/bash"
	"github.com/hankwenyx/claude-code-go/pkg/tools/fileedit"
	"github.com/hankwenyx/claude-code-go/pkg/tools/fileread"
	"github.com/hankwenyx/claude-code-go/pkg/tools/filewrite"
	"github.com/hankwenyx/claude-code-go/pkg/tools/glob"
	"github.com/hankwenyx/claude-code-go/pkg/tools/grep"
	"github.com/hankwenyx/claude-code-go/pkg/tools/sendmsg"
	agentTask "github.com/hankwenyx/claude-code-go/pkg/tools/task"
	"github.com/hankwenyx/claude-code-go/pkg/tools/todo"
	"github.com/hankwenyx/claude-code-go/pkg/tools/webfetch"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	model             string
	maxTokens         int
	apiKey            string
	noTools           bool
	allowRules        []string
	bypassPermissions bool
	forceInteractive  bool
	resumeSession     string
	briefMode         bool
	permissiveMode    bool
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "gocc [message]",
	Short: "Claude Code CLI - AI-powered coding assistant",
	Long: `Claude Code CLI is an AI-powered coding assistant that helps you with
software engineering tasks. It can read files, edit code, run commands,
and more.

Examples:
  gocc "hello"
  gocc "list .go files"
  echo "what is this project?" | gocc`,
	Args: cobra.MaximumNArgs(1),
	Run:  run,
}

func init() {
	rootCmd.Flags().StringVarP(&model, "model", "m", "", "Model to use (default: claude-sonnet-4-6)")
	rootCmd.Flags().IntVarP(&maxTokens, "max-tokens", "t", 0, "Maximum output tokens (default: 4096)")
	rootCmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "Anthropic API key (default: from settings or env)")
	rootCmd.Flags().BoolVar(&noTools, "no-tools", false, "Disable all tools (single-turn text only)")
	rootCmd.Flags().StringArrayVar(&allowRules, "allow", nil, `Add a permission allow rule for this run, e.g. --allow "Bash(git *)" (repeatable)`)
	rootCmd.Flags().BoolVar(&bypassPermissions, "bypass-permissions", false, "Skip all permission checks for this run")
	rootCmd.Flags().BoolVarP(&forceInteractive, "interactive", "i", false, "Force interactive TUI mode (even when a message is provided)")
	rootCmd.Flags().StringVar(&resumeSession, "resume", "", "Resume a saved session by ID (use with -i or headless)")
	rootCmd.Flags().BoolVar(&briefMode, "brief", false, "Brief/chat mode: hide tool calls, use SendUserMessage for replies")
	rootCmd.Flags().BoolVar(&permissiveMode, "permissive", false, "Allow all reads/fetches/safe commands freely; ask user before anything else (e.g. rm, sudo)")
}

// isInteractive returns true when both stdin and stdout are terminals
func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) &&
		term.IsTerminal(int(os.Stdout.Fd()))
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

	// Get message and determine mode
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

	// Decide whether to launch the TUI.
	// TUI launches automatically when stdin and stdout are both terminals and
	// no message is provided (original Claude Code behaviour).
	// Use -i to force TUI even with a message; pipe input to force headless.
	launchTUI := forceInteractive || (isInteractive() && message == "")

	if !launchTUI && message == "" {
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

	// Load project MEMORY.md and append to CLAUDE.md content
	if memContent, err := memory.LoadMemory(cwd); err == nil && memContent != "" {
		claudeMdContent += "\n\n# Auto Memory\n" + memContent
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
		// Brief/chat mode: register SendUserMessage so the model uses it for replies
		if briefMode {
			registry.Register(sendmsg.New())
		}
		// Todo tools (Phase 4e): shared in-memory store
		todoStore := todo.NewStore()
		registry.Register(todo.NewWriteTool(todoStore))
		registry.Register(todo.NewReadTool(todoStore))
	}

	// Build permission checker
	var permRules config.PermissionRules
	if settings != nil {
		permRules = config.ParsePermissionRules(settings.Permissions)
	}
	// --allow flag appends rules for this run only
	permRules.Allow = append(permRules.Allow, allowRules...)

	// --permissive: allow all reads and common safe commands; deny dangerous destructive ones
	if permissiveMode {
		permRules.Allow = append(permRules.Allow,
			"Read",        // all file reads
			"Glob",        // file listing
			"Grep",        // file search
			"WebFetch",    // HTTP reads
			"Bash(git *)", // git read ops (log, diff, status…)
			"Bash(go *)",  // go tool
			"Bash(make *)",
			"Bash(cat *)",
			"Bash(ls *)",
			"Bash(find *)",
			"Bash(grep *)",
			"Bash(echo *)",
			"Bash(pwd)",
			"Bash(env)",
			"Bash(which *)",
			"Bash(type *)",
			"Bash(head *)",
			"Bash(tail *)",
			"Bash(wc *)",
			"Bash(sort *)",
			"Bash(uniq *)",
			"Bash(awk *)",
			"Bash(sed *)",
			"Bash(jq *)",
			"Bash(curl *)",
			"Bash(wget *)",
		)
		// Note: commands NOT in the allow list (e.g. rm, sudo) will still
		// trigger the normal ask/deny flow — user gets a confirmation prompt
		// in TUI mode, and is denied in headless mode. No extra deny rules needed.
	}

	permMode := permissions.ModeAuto
	if bypassPermissions {
		permMode = permissions.ModeBypassPermissions
	} else if settings != nil && settings.Permissions.DefaultMode != "" {
		permMode = permissions.Mode(settings.Permissions.DefaultMode)
	}
	permChecker := &permissions.Checker{
		Mode:           permMode,
		Rules:          permRules,
		CWD:            cwd,
		NonInteractive: !launchTUI,
	}
	if settings != nil {
		permChecker.AdditlDirs = settings.Permissions.AdditionalDirs
	}

	// Connect MCP servers from settings (best-effort; errors printed to stderr)
	mcpManager := mcp.NewManager()
	if settings != nil && len(settings.MCPServers) > 0 {
		mcpCfgs := make(map[string]mcp.ServerConfig, len(settings.MCPServers))
		for name, sc := range settings.MCPServers {
			mcpCfgs[name] = mcp.ServerConfig{
				Command: sc.Command,
				Args:    sc.Args,
				Env:     sc.Env,
				URL:     sc.URL,
			}
		}
		if errs := mcpManager.Connect(cmd.Context(), mcpCfgs); len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, "Warning: %v\n", e)
			}
		}
		mcpManager.RegisterAll(registry)
	}
	defer mcpManager.Close()

	// Task tool (Phase 4a/4b): sub-agent runner closes over opts built below.
	// We build a forward-declared runner so opts can reference taskManager.
	taskManager := agentTask.NewManager(32)
	// taskRunner is filled in after opts is fully constructed (below).
	var taskRunner agentTask.SubAgentRunner

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
		NonInteractive:  !launchTUI,
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
	opts.TaskManager = taskManager

	// Wire hooks runner from settings
	if len(settings.Hooks.PreToolUse) > 0 || len(settings.Hooks.PostToolUse) > 0 ||
		len(settings.Hooks.Notification) > 0 || len(settings.Hooks.Stop) > 0 {
		hr := hooks.New(settings.Hooks)
		hr.CWD = cwd
		opts.HookRunner = hr
	}

	// Wire up the Task tool now that opts is complete.
	// The runner captures opts by value so each sub-agent gets a clean copy
	// (no prior conversation history, no recursive TaskManager to avoid loops).
	if !noTools {
		taskRunner = func(ctx context.Context, prompt string) (string, error) {
			subOpts := opts
			subOpts.Messages = nil    // fresh conversation
			subOpts.TaskManager = nil // no nested async tasks
			subOpts.MaxTurns = 20     // safety limit for sub-agents
			return agent.RunAgentSync(ctx, prompt, subOpts)
		}
		registry.Register(agentTask.NewWithManager(taskRunner, taskManager))
	}

	// Dispatch: TUI or headless
	if launchTUI {
		// Resolve session: load existing or generate a new ID
		sessID, resumeHistory := resolveSession(cwd, resumeSession)
		opts.SessionID = sessID
		if len(resumeHistory) > 0 {
			opts.Messages = resumeHistory
		}
		if err := tui.Run(cmd.Context(), opts, permChecker, tui.RunOptions{
			SessionID:     sessID,
			ResumeHistory: resumeHistory,
			BriefMode:     briefMode,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Headless: run agent and stream to stdout/stderr
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

// resolveSession returns a session ID and prior message history.
// If resumeID is non-empty, it loads that session; otherwise it generates a fresh ID.
func resolveSession(cwd, resumeID string) (id string, history []api.APIMessage) {
	if resumeID != "" {
		rec, err := session.Load(cwd, resumeID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load session %q: %v\n", resumeID, err)
		} else {
			return rec.ID, rec.Messages
		}
	}
	return fmt.Sprintf("%016x", rand.Int63()), nil
}
