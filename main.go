package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jacklicn/dipper-bot/agent"
	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/channels"
	"github.com/jacklicn/dipper-bot/config"
	"github.com/jacklicn/dipper-bot/cron"
	"github.com/jacklicn/dipper-bot/gateway"
	"github.com/jacklicn/dipper-bot/heartbeat"
	"github.com/jacklicn/dipper-bot/logging"
	"github.com/jacklicn/dipper-bot/os/unix"
	"github.com/jacklicn/dipper-bot/os/win32"
	"github.com/jacklicn/dipper-bot/providers"
	"github.com/jacklicn/dipper-bot/session"
	"github.com/jacklicn/dipper-bot/web"
	"github.com/peterh/liner"
)

const (
	logo    = "🐕"
	version = "1.0.0"
)

func exeName() string {
	if runtime.GOOS == "windows" {
		return "dipper-bot.exe"
	}
	return "dipper-bot"
}

func main() {
	if runtime.GOOS == "windows" {
		win32.SetConsoleUTF8()
	} else {
		unix.SetConsoleUTF8()
	}
	if len(os.Args) < 2 {
		runInteractiveShell()
		return
	}
	cmd := strings.ToLower(os.Args[1])
	args := os.Args[2:]

	switch cmd {
	case "onboard":
		runOnboard(args)
	case "agent":
		runAgent(args)
	case "gateway":
		runGateway(args)
	case "status":
		runStatus(args)
	case "channels":
		if len(args) >= 1 && strings.ToLower(args[0]) == "status" {
			runChannelsStatus(args[1:])
		} else if len(args) >= 1 && strings.ToLower(args[0]) == "login" {
			runChannelsLogin(args[1:])
		} else {
			fmt.Fprintf(os.Stderr, "Usage: %s channels [status|login]\n", exeName())
			os.Exit(1)
		}
	case "cron":
		runCron(args)
	case "provider":
		if len(args) >= 1 && strings.ToLower(args[0]) == "login" {
			runProviderLogin(args[1:])
		} else {
			fmt.Fprintf(os.Stderr, "Usage: %s provider login <openai-codex|github-copilot>\n", exeName())
			os.Exit(1)
		}
	case "version", "-v", "--version":
		fmt.Printf("%s dipper-bot v%s\n", logo, version)
	case "help", "-h", "--help":
		printUsage(true)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage(true)
		os.Exit(1)
	}
}

// parseWorkspaceFlag extracts --workspace/-w from args, returns (workspace, remainingArgs).
func parseWorkspaceFlag(args []string) (workspace string, remaining []string) {
	for i := 0; i < len(args); i++ {
		if (args[i] == "--workspace" || args[i] == "-w") && i+1 < len(args) {
			workspace = strings.TrimSpace(args[i+1])
			remaining = append(append([]string{}, args[:i]...), args[i+2:]...)
			return workspace, remaining
		}
	}
	return "", args
}

// loadConfigWithWorkspace loads config. If workspace is set, workspace/config.json takes priority over ~/.dipper-bot/config.json.
func initLogging(cfg *config.Config, wsPath string, gatewayVerbose bool) {
	if cfg == nil {
		return
	}
	if err := logging.SetupDefault(cfg, wsPath, gatewayVerbose); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: file logging: %v\n", err)
	}
}

func loadConfigWithWorkspace(workspace string) (*config.Config, string, error) {
	configPath, err := config.ResolveConfigPath(workspace)
	if err != nil {
		return nil, "", err
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, configPath, err
	}
	return cfg, configPath, nil
}

func printUsage(showExe bool) {
	prefix := ""
	if showExe {
		prefix = exeName() + " "
	}
	fmt.Printf(`%s dipper-bot - Personal AI Assistant

Usage:
  %sonboard [--workspace PATH]   Initialize config and workspace
  %sagent [--workspace PATH] [-m MSG] [--markdown|--no-markdown] [--logs|--no-logs] [--web [--host HOST] [-p PORT]]   Chat with the agent
  %sgateway [--workspace PATH] [--host HOST] [-p PORT] [-v|--verbose]   Start the gateway (default port 8090)
  %sstatus [--workspace PATH]   Show status
  %schannels status [--workspace PATH]   Show channel status
  %schannels login [--workspace PATH]   Link WhatsApp (QR)
  %scron [list|add|...] [--workspace PATH]   Cron jobs (list, add, remove, run, enable, disable)
  %scron add -n NAME -m MSG [-e SECONDS | -c|--cron EXPR [--tz TZ] | --at TIME] [-d|--deliver]
  %scron remove ID
  %scron run ID
  %scron enable ID [--disable]
  %scron disable ID
  %sprovider login <openai-codex|github-copilot>   OAuth login for LLM providers
`, logo, prefix, prefix, prefix, prefix, prefix, prefix, prefix, prefix, prefix, prefix, prefix, prefix, prefix)
}

// runInteractiveShell runs when no args (e.g. double-click). Shows help and REPL for commands.
func runInteractiveShell() {
	printUsage(false)
	fmt.Println()
	exe := exeName()
	fmt.Printf("Type a command (e.g. agent, status) or help for usage, exit/quit to quit.\n\n")

	lineReader := liner.NewLiner()
	defer lineReader.Close()
	lineReader.SetCtrlCAborts(true)

	for {
		prompt := exe + "> "
		input, err := lineReader.Prompt(prompt)
		if err != nil {
			if err == liner.ErrPromptAborted {
				fmt.Println("\nGoodbye!")
			}
			return
		}
		line := strings.TrimSpace(input)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if lower == "exit" || lower == "quit" || lower == ":q" {
			fmt.Println("Goodbye!")
			return
		}
		if lower == "help" || lower == "?" {
			printUsage(false)
			fmt.Println()
			continue
		}

		lineReader.AppendHistory(line)
		parts := strings.Fields(line)
		cmd := strings.ToLower(parts[0])
		args := parts[1:]

		dispatchCommand(cmd, args)
	}
}

func dispatchCommand(cmd string, args []string) {
	switch cmd {
	case "onboard":
		runOnboard(args)
	case "agent":
		runAgent(args)
	case "gateway":
		runGateway(args)
	case "status":
		runStatus(args)
	case "channels":
		if len(args) >= 1 && strings.ToLower(args[0]) == "status" {
			runChannelsStatus(args[1:])
		} else if len(args) >= 1 && strings.ToLower(args[0]) == "login" {
			runChannelsLogin(args[1:])
		} else {
			fmt.Fprintf(os.Stderr, "Usage: %s channels [status|login]\n", exeName())
		}
	case "cron":
		runCron(args)
	case "provider":
		if len(args) >= 1 && strings.ToLower(args[0]) == "login" {
			runProviderLogin(args[1:])
		} else {
			fmt.Fprintf(os.Stderr, "Usage: %s provider login <openai-codex|github-copilot>\n", exeName())
		}
	case "version", "-v", "--version":
		fmt.Printf("%s dipper-bot v%s\n", logo, version)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage(false)
	}
}

func runOnboard(args []string) {
	workspaceFlag, _ := parseWorkspaceFlag(args)

	// Design: workspace path saved in ~/.dipper-bot/config.json; other config in workspace/config.json.
	// Workspace config (workspace/config.json) takes priority over ~/.dipper-bot/config.json when loading.
	defaultConfigPath, _ := config.GetConfigPath()
	_ = os.MkdirAll(filepath.Dir(defaultConfigPath), 0o750)

	// Resolve workspace: flag > interactive prompt > existing in ~/.dipper-bot
	workspaceToUse := workspaceFlag
	if workspaceToUse == "" {
		lineReader := liner.NewLiner()
		lineReader.SetCtrlCAborts(true)
		existingWs, _ := config.GetWorkspaceFromDefaultConfig()
		defaultWs := existingWs
		if defaultWs == "" {
			defaultWs = "~/.dipper-bot/workspace"
		}
		prompt := fmt.Sprintf("Workspace path [%s]: ", defaultWs)
		answer, err := lineReader.Prompt(prompt)
		lineReader.Close()
		if err != nil && err != io.EOF {
			fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
			os.Exit(1)
		}
		if answer != "" {
			workspaceToUse = strings.TrimSpace(answer)
		} else if defaultWs != "~/.dipper-bot/workspace" {
			workspaceToUse = defaultWs
		} else {
			workspaceToUse = "~/.dipper-bot/workspace"
		}
	}

	// Save workspace path to ~/.dipper-bot/config.json
	if err := config.SaveWorkspaceToDefaultConfig(workspaceToUse); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving workspace to %s: %v\n", defaultConfigPath, err)
		os.Exit(1)
	}
	fmt.Printf("%s Workspace %s saved to ~/.dipper-bot/config.json\n", logo, workspaceToUse)

	// Full config (providers, channels, etc.) goes to workspace/config.json
	wsConfigPath, err := config.WorkspaceConfigPath(workspaceToUse)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	_ = os.MkdirAll(filepath.Dir(wsConfigPath), 0o750)

	var cfg *config.Config
	if _, err := os.Stat(wsConfigPath); err == nil {
		fmt.Printf("Workspace config already exists at %s\n", wsConfigPath)
		fmt.Println("  y = overwrite with defaults (existing values will be lost)")
		fmt.Println("  N = refresh config, keeping existing values and adding new fields")
		lineReader := liner.NewLiner()
		lineReader.SetCtrlCAborts(true)
		answer, err := lineReader.Prompt("Overwrite? [y/N]: ")
		lineReader.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
			os.Exit(1)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "y" || answer == "yes" {
			cfg = config.DefaultConfig()
			cfg.Agents.Defaults.Workspace = workspaceToUse
			if err := config.SaveConfig(cfg, wsConfigPath); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("%s Workspace config reset at %s\n", logo, wsConfigPath)
		} else {
			cfg, err = config.LoadConfig(wsConfigPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
				os.Exit(1)
			}
			cfg.Agents.Defaults.Workspace = workspaceToUse
			if err := config.SaveConfig(cfg, wsConfigPath); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("%s Workspace config refreshed at %s\n", logo, wsConfigPath)
		}
	} else {
		cfg = config.DefaultConfig()
		cfg.Agents.Defaults.Workspace = workspaceToUse
		if err := config.SaveConfig(cfg, wsConfigPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("%s Workspace config created at %s\n", logo, wsConfigPath)
	}

	workspace, _ := config.GetWorkspacePath(cfg)
	_ = os.MkdirAll(workspace, 0o750)
	if err := seedOnboardWorkspaceAndSkills(workspace, true); err != nil {
		fmt.Fprintf(os.Stderr, "Error seeding workspace: %v\n", err)
		os.Exit(1)
	}
	memDir := filepath.Join(workspace, "memory")
	historyPath := filepath.Join(memDir, "HISTORY.md")
	if _, err := os.Stat(historyPath); os.IsNotExist(err) {
		_ = os.MkdirAll(memDir, 0o750)
		_ = os.WriteFile(historyPath, []byte(""), 0o600)
		fmt.Printf("  Created memory/HISTORY.md\n")
	}
	fmt.Printf("\n%s dipper-bot is ready!\n", logo)
	fmt.Printf("  1. Add your API key to %s\n", wsConfigPath)
	fmt.Printf("  2. Chat: %s agent -m \"Hello!\"\n", exeName())
	if workspaceToUse != "~/.dipper-bot/workspace" {
		fmt.Printf("     (workspace %s saved in ~/.dipper-bot/config.json)\n", workspaceToUse)
	}
}

func runAgent(args []string) {
	workspace, args := parseWorkspaceFlag(args)
	if workspace == "" {
		workspace = os.Getenv("DIPPER_WORKSPACE")
	}
	cfg, configPath, err := loadConfigWithWorkspace(workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	model := cfg.Agents.Defaults.Model
	if model == "" {
		model = "anthropic/claude-opus-4-5"
	}
	provider, providerName, err := newProvider(cfg, model, configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	wsPath, _ := config.GetWorkspacePath(cfg)
	initLogging(cfg, wsPath, false)
	b := bus.NewMessageBus()
	sessMgr, _ := session.NewSessionManager(wsPath)
	loop, err := agent.NewAgentLoop(b, provider, providerName, cfg, wsPath, sessMgr, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	var reloadMu sync.Mutex
	reloadFn := func(ctx context.Context) (string, error) {
		reloadMu.Lock()
		defer reloadMu.Unlock()

		nextCfg, nextConfigPath, err := loadConfigWithWorkspace(workspace)
		if err != nil {
			return "", fmt.Errorf("reload failed: %w", err)
		}
		nextWsPath, _ := config.GetWorkspacePath(nextCfg)
		if nextWsPath != wsPath {
			return "", fmt.Errorf("reload blocked: workspace changed from %s to %s; restart required", wsPath, nextWsPath)
		}
		model := nextCfg.Agents.Defaults.Model
		if model == "" {
			model = "anthropic/claude-opus-4-5"
		}
		nextProvider, nextProviderName, err := newProvider(nextCfg, model, nextConfigPath)
		if err != nil {
			return "", fmt.Errorf("reload failed: %w", err)
		}
		nextLoop, err := agent.NewAgentLoop(b, nextProvider, nextProviderName, nextCfg, wsPath, sessMgr, nil)
		if err != nil {
			return "", fmt.Errorf("reload failed: %w", err)
		}

		loop = nextLoop
		configPath = nextConfigPath
		cfg = nextCfg
		return "✅ config.json 已重载并生效。", nil
	}

	var message, sessionKey string
	sessionKey = "cli:direct"
	markdown := true
	logs := false
	webMode := false
	webHost := ""
	webPort := 8600
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-m", "--message":
			if i+1 < len(args) {
				message = args[i+1]
				i++
			}
		case "-s", "--session":
			if i+1 < len(args) {
				sessionKey = args[i+1]
				if !strings.Contains(sessionKey, ":") {
					sessionKey = "cli:" + sessionKey
				}
				i++
			}
		case "--markdown":
			markdown = true
		case "--no-markdown":
			markdown = false
		case "--logs":
			logs = true
		case "--no-logs":
			logs = false
		case "--web":
			webMode = true
		case "--host":
			if i+1 < len(args) {
				webHost = args[i+1]
				i++
			}
		case "-p", "--port":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &webPort)
				i++
			}
		}
	}

	if webMode {
		if !logs {
			slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		}
		if webHost == "" && cfg != nil && cfg.Gateway.Host != "" {
			webHost = cfg.Gateway.Host
		}
		transcriber := providers.NewTranscriptionProviderFromConfig(cfg)
		srv := web.NewServer(loop, wsPath, webHost, webPort, transcriber)
		webReloadFn := func(ctx context.Context) (string, error) {
			msg, err := reloadFn(ctx)
			if err != nil {
				return "", err
			}
			srv.SetLoop(loop)
			return msg, nil
		}
		stopHotReload := watchConfigHotReload(configPath, 2*time.Second, func() {
			if _, err := webReloadFn(context.Background()); err != nil {
				slog.Error("config hot reload failed", "path", configPath, "error", err)
			} else {
				slog.Info("config hot reloaded", "path", configPath)
			}
		})
		displayHost := webHost
		if displayHost == "" || displayHost == "0.0.0.0" {
			displayHost = "localhost"
		}
		fmt.Printf("%s Web chat: http://%s:%d\n", logo, displayHost, webPort)
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sig
			fmt.Println("\nShutting down...")
			stopHotReload()
			_ = srv.Shutdown(context.Background())
		}()
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if !logs {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}

	ctx := context.Background()
	if message != "" {
		ch, chat := parseSessionKey(sessionKey)
		onProgress := makeProgressPrinter()
		resp, err := runWithThinkingSpinner(logs, func() (string, error) {
			return loop.ProcessDirect(ctx, message, sessionKey, ch, chat, onProgress)
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n%s dipper-bot\n%s\n\n", logo, formatAgentResponse(resp, markdown))
		return
	}

	fmt.Printf("%s Interactive mode (type exit or Ctrl+C to quit)\n\n", logo)
	lineReader := liner.NewLiner()
	defer lineReader.Close()
	lineReader.SetCtrlCAborts(true)
	for {
		input, err := lineReader.Prompt("You: ")
		if err != nil {
			if err == liner.ErrPromptAborted {
				fmt.Println("\nGoodbye!")
			}
			break
		}
		line := strings.TrimSpace(input)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if lower == "exit" || lower == "quit" || lower == "/exit" || lower == "/quit" || lower == ":q" {
			fmt.Println("Goodbye!")
			return
		}
		lineReader.AppendHistory(line)
		ch, chat := parseSessionKey(sessionKey)
		onProgress := makeProgressPrinter()
		resp, err := runWithThinkingSpinner(logs, func() (string, error) {
			return loop.ProcessDirect(ctx, line, sessionKey, ch, chat, onProgress)
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
		}
		fmt.Printf("\n%s dipper-bot\n%s\n\n", logo, formatAgentResponse(resp, markdown))
	}
}

func watchConfigHotReload(path string, interval time.Duration, onChange func()) func() {
	stop := make(chan struct{})
	var stopOnce sync.Once
	info, err := os.Stat(path)
	var lastMod time.Time
	if err == nil {
		lastMod = info.ModTime()
	}
	go func() {
		tk := time.NewTicker(interval)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				fi, statErr := os.Stat(path)
				if statErr != nil {
					continue
				}
				mod := fi.ModTime()
				if !mod.Equal(lastMod) {
					lastMod = mod
					onChange()
				}
			}
		}
	}()
	return func() {
		stopOnce.Do(func() { close(stop) })
	}
}

func makeProgressPrinter() func(string, string) {
	green, reset := "\033[32m", "\033[0m"
	return func(toolName, detail string) {
		if toolName == "exec" && detail != "" {
			fmt.Printf("  %s↳ %s: %s%s\n", green, toolName, detail, reset)
		} else {
			fmt.Printf("  %s↳ %s%s\n", green, toolName, reset)
		}
	}
}

// runWithThinkingSpinner runs fn and shows "dipper-bot is thinking..." spinner when logs is false.
// When logs is true, no spinner (user sees runtime logs).
func runWithThinkingSpinner(logs bool, fn func() (string, error)) (string, error) {
	if logs {
		return fn()
	}
	done := make(chan struct{})
	fmt.Print("\033[?25l") // hide cursor
	go func() {
		dots := []string{"", ".", "..", "..."}
		i := 0
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				fmt.Printf("\r\033[32m%s dipper-bot is thinking%s\033[0m   ", logo, dots[i%4])
				i++
			}
		}
	}()
	resp, err := fn()
	close(done)
	time.Sleep(50 * time.Millisecond)
	fmt.Print("\r\033[K\033[?25h") // clear line and show cursor
	return resp, err
}

func formatAgentResponse(resp string, markdown bool) string {
	if markdown {
		return resp
	}
	// Strip markdown for plain text (--no-markdown): remove code fences and header markers
	lines := strings.Split(resp, "\n")
	out := make([]string, 0, len(lines))
	inCode := false
	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			out = append(out, line)
			continue
		}
		trimmed := strings.TrimLeft(line, "# ")
		out = append(out, trimmed)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func runGateway(args []string) {
	workspace, args := parseWorkspaceFlag(args)
	if workspace == "" {
		workspace = os.Getenv("DIPPER_WORKSPACE")
	}
	host := ""
	port := 8090
	verbose := false
	for i := 0; i < len(args); i++ {
		if (args[i] == "--host") && i+1 < len(args) {
			host = args[i+1]
			i++
		} else if (args[i] == "-p" || args[i] == "--port") && i+1 < len(args) {
			fmt.Sscanf(args[i+1], "%d", &port)
			i++
		} else if args[i] == "-v" || args[i] == "--verbose" {
			verbose = true
		}
	}

	cfg, configPath, err := loadConfigWithWorkspace(workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	model := cfg.Agents.Defaults.Model
	if model == "" {
		model = "anthropic/claude-opus-4-5"
	}
	provider, providerName, err := newProvider(cfg, model, configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	wsPath, _ := config.GetWorkspacePath(cfg)
	initLogging(cfg, wsPath, verbose)

	b := bus.NewMessageBus()

	cronPath := filepath.Join(wsPath, "cron", "jobs.json")
	cronSvc := cron.NewService(cronPath, nil)

	sessMgr, _ := session.NewSessionManager(wsPath)
	cronRef := &agent.CronServiceRef{Service: cronSvc}
	loop, err := agent.NewAgentLoop(b, provider, providerName, cfg, wsPath, sessMgr, cronRef)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	cronSvc.OnJob = func(ctx context.Context, job *cron.Job) (string, error) {
		return loop.ProcessDirect(ctx, job.Payload.Message, "cron:"+job.ID, job.Payload.Channel, job.Payload.To, nil)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = cronSvc.Start(ctx)
	defer cronSvc.Stop()
	go loop.Run(ctx)

	// Outbound dispatcher: send agent replies to Telegram/WhatsApp/Discord
	go b.DispatchOutbound(ctx)

	// Channels (Telegram, WhatsApp, Discord)
	chMgr := channels.NewManager(cfg, b)
	chMgr.Start(ctx)
	defer chMgr.Stop()
	if names := chMgr.EnabledChannels(); len(names) > 0 {
		fmt.Printf("%s Channels: %s\n", logo, strings.Join(names, ", "))
	}

	hbRunner := func(ctx context.Context, prompt string) (string, error) {
		return loop.ProcessDirect(ctx, prompt, "heartbeat", "cli", "direct", nil)
	}
	hb := heartbeat.NewService(wsPath, hbRunner, heartbeat.DefaultInterval, true)
	hb.Start(ctx)
	defer hb.Stop()

	if host == "" && cfg != nil && cfg.Gateway.Host != "" {
		host = cfg.Gateway.Host
	}
	srv := gateway.NewServerWithRateLimitAndKeying(
		b,
		host,
		port,
		cfg.Gateway.RateLimitPerMinute,
		cfg.Gateway.RateLimitIPv4Prefix,
		cfg.Gateway.RateLimitIPv6Prefix,
		cfg.Gateway.RateLimitCIDRs,
	)
	go func() {
		_ = srv.Start()
	}()

	addr := fmt.Sprintf(":%d", port)
	if host != "" {
		addr = host + addr
	} else {
		addr = "0.0.0.0" + addr
	}
	fmt.Printf("%s Gateway listening on %s\n", logo, addr)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	fmt.Println("\nShutting down...")
	cancel()
	_ = srv.Shutdown(context.Background())
}

func newProvider(cfg *config.Config, model, configPath string) (providers.LLMProvider, string, error) {
	cfgHint := configPath
	if cfgHint == "" {
		cfgHint, _ = config.GetConfigPath()
	}
	if cfgHint == "" {
		cfgHint = "~/.dipper-bot/config.json"
	}
	_, providerName := config.MatchProvider(cfg, model)
	switch providerName {
	case "openai_codex":
		accessToken, accountID, err := providers.LoadCodexAuth()
		if err != nil {
			return nil, "", fmt.Errorf("failed to load Codex auth: %w", err)
		}
		if accessToken == "" || accountID == "" {
			return nil, "", fmt.Errorf("not logged in to OpenAI Codex. Run: %s provider login openai-codex", exeName())
		}
		resolvedModel := config.ResolveModelForAPI("openai_codex", model)
		if resolvedModel == "" {
			resolvedModel = "gpt-5.1-codex"
		}
		return providers.NewCodexProvider(accessToken, accountID, resolvedModel), "openai_codex", nil
	case "github_copilot":
		apiKey, apiBase, err := providers.LoadCopilotAPIKey()
		if err != nil {
			return nil, "", fmt.Errorf("failed to load Copilot auth: %w", err)
		}
		if apiKey == "" {
			return nil, "", fmt.Errorf("not logged in to GitHub Copilot. Run: %s provider login github-copilot", exeName())
		}
		resolvedModel := config.ResolveModelForAPI("github_copilot", model)
		if resolvedModel == "" {
			resolvedModel = "github_copilot/gpt-4o"
		}
		return providers.NewGitHubCopilotProvider(apiKey, apiBase, resolvedModel), "github_copilot", nil
	case "custom":
		apiKey := cfg.Providers.Custom.APIKey
		if apiKey == "" {
			return nil, "", fmt.Errorf("no API key. Set providers.custom.apiKey in %s", cfgHint)
		}
		apiBase := cfg.Providers.Custom.GetAPIBase()
		resolvedModel := config.ResolveModelForAPI("custom", model)
		return providers.NewOpenAIProvider(apiKey, apiBase, resolvedModel), "custom", nil
	default:
		if providerName == "" {
			return nil, "", fmt.Errorf("no provider for model %q. Set providers.custom.apiKey in %s", model, cfgHint)
		}
		return nil, "", fmt.Errorf("provider %q not fully supported", providerName)
	}
}

func runStatus(args []string) {
	workspace, _ := parseWorkspaceFlag(args)
	if workspace == "" {
		workspace = os.Getenv("DIPPER_WORKSPACE")
	}
	configPath, _ := config.ResolveConfigPath(workspace)
	cfg, _ := config.LoadConfig(configPath)
	wsPath, _ := config.GetWorkspacePath(cfg)

	fmt.Printf("%s %s Status\n\n", logo, exeName())
	fmt.Printf("Config: %s ", configPath)
	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("✓")
	} else {
		fmt.Println("✗")
	}
	fmt.Printf("Workspace: %s ", wsPath)
	if _, err := os.Stat(wsPath); err == nil {
		fmt.Println("✓")
	} else {
		fmt.Println("✗")
	}
	if cfg != nil {
		fmt.Printf("Model: %s\n", cfg.Agents.Defaults.Model)
		_, providerName := config.MatchProvider(cfg, cfg.Agents.Defaults.Model)
		switch providerName {
		case "openai_codex":
			accessToken, accountID, _ := providers.LoadCodexAuth()
			if accessToken != "" && accountID != "" {
				fmt.Printf("OpenAI Codex: ✓ (%s)\n", accountID)
			} else {
				fmt.Printf("OpenAI Codex: not logged in (run: %s provider login openai-codex)\n", exeName())
			}
		case "github_copilot":
			apiKey, _, _ := providers.LoadCopilotAPIKey()
			if apiKey != "" {
				fmt.Printf("GitHub Copilot: ✓\n")
			} else {
				fmt.Printf("GitHub Copilot: not logged in (run: %s provider login github-copilot)\n", exeName())
			}
		default:
			p := &cfg.Providers.Custom
			if p.APIKey != "" {
				fmt.Printf("Custom: ✓\n")
			} else {
				fmt.Printf("Custom: not set\n")
			}
		}
	}
}

func providerLabel(name string) string {
	if name == "custom" {
		return "Custom"
	}
	if len(name) > 0 {
		return strings.ToUpper(name[:1]) + name[1:]
	}
	return name
}

func runChannelsStatus(args []string) {
	workspace, _ := parseWorkspaceFlag(args)
	cfg, _, _ := loadConfigWithWorkspace(workspace)
	if cfg == nil {
		return
	}
	channels := []struct {
		name string
		en   bool
		cfg  string
	}{
		{"WhatsApp", cfg.Channels.WhatsApp.Enabled, cond(cfg.Channels.WhatsApp.BridgeURL != "", cfg.Channels.WhatsApp.BridgeURL, "not configured")},
		{"Discord", cfg.Channels.Discord.Enabled, cond(cfg.Channels.Discord.GatewayURL != "", cfg.Channels.Discord.GatewayURL, cond(cfg.Channels.Discord.Token != "", "token: ...", "not configured"))},
		{"Feishu", cfg.Channels.Feishu.Enabled && cfg.Channels.Feishu.AppID != "", cond(cfg.Channels.Feishu.AppID != "", "app_id: "+trunc(cfg.Channels.Feishu.AppID, 10)+"...", "not configured")},
		{"Mochat", cfg.Channels.Mochat.Enabled, cond(cfg.Channels.Mochat.BaseURL != "", cfg.Channels.Mochat.BaseURL, "not configured")},
		{"Telegram", cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.Token != "", cond(cfg.Channels.Telegram.Token != "", "token: ...", "not configured")},
		{"Slack", cfg.Channels.Slack.Enabled && cfg.Channels.Slack.BotToken != "", cond(cfg.Channels.Slack.BotToken != "" && cfg.Channels.Slack.AppToken != "", "socket", "not configured")},
		{"DingTalk", cfg.Channels.DingTalk.Enabled && cfg.Channels.DingTalk.ClientID != "", cond(cfg.Channels.DingTalk.ClientID != "", "client_id: ...", "not configured")},
		{"Email", cfg.Channels.Email.Enabled && cfg.Channels.Email.ConsentGranted, cond(cfg.Channels.Email.IMAPHost != "", "imap: ...", "not configured")},
		{"QQ", cfg.Channels.QQ.Enabled && cfg.Channels.QQ.AppID != "", cond(cfg.Channels.QQ.AppID != "", "app_id: ...", "not configured")},
		{"Wecom", cfg.Channels.Wecom.Enabled && cfg.Channels.Wecom.BotID != "", cond(cfg.Channels.Wecom.BotID != "", "bot_id: ...", "not configured")},
		{"Webhook", cfg.Channels.Webhook.Enabled, cond(cfg.Channels.Webhook.Enabled, "POST /message", "not configured")},
	}
	fmt.Println("Channel Status")
	fmt.Printf("%-10s %-8s %s\n", "Channel", "Enabled", "Configuration")
	fmt.Println(strings.Repeat("-", 50))
	for _, ch := range channels {
		en := "✗"
		if ch.en {
			en = "✓"
		}
		fmt.Printf("%-10s %-8s %s\n", ch.name, en, ch.cfg)
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func parseSessionKey(key string) (channel, chatID string) {
	if idx := strings.Index(key, ":"); idx >= 0 {
		return key[:idx], key[idx+1:]
	}
	return "cli", key
}

func cond(ok bool, a, b string) string {
	if ok {
		return a
	}
	return b
}

// parsePortFromURL extracts port from ws://host:port or wss://host:port.
func parsePortFromURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	_, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return ""
	}
	return port
}

func runChannelsLogin(args []string) {
	workspace, _ := parseWorkspaceFlag(args)
	if workspace == "" {
		workspace = os.Getenv("DIPPER_WORKSPACE")
	}
	cfg, _, _ := loadConfigWithWorkspace(workspace)
	wsPath, _ := config.GetWorkspacePath(cfg)
	authDir := filepath.Join(wsPath, "whatsapp-auth")
	bridgeDir := os.Getenv("DIPPER_BRIDGE_DIR")
	if bridgeDir == "" {
		home, _ := os.UserHomeDir()
		bridgeDir = filepath.Join(home, ".dipper-bot", "bridge")
		if _, err := os.Stat(filepath.Join(bridgeDir, "package.json")); err != nil {
			// Fallback: bridge next to executable
			if exe, err := os.Executable(); err == nil {
				exeAbs, _ := filepath.Abs(exe)
				exeDirBridge := filepath.Join(filepath.Dir(exeAbs), "bridge")
				if _, err := os.Stat(filepath.Join(exeDirBridge, "package.json")); err == nil {
					bridgeDir = exeDirBridge
				}
			}
		}
		if _, err := os.Stat(filepath.Join(bridgeDir, "package.json")); err != nil {
			// Fallback: ./bridge relative to cwd (development)
			if cwd, err := os.Getwd(); err == nil {
				cwdAbs, _ := filepath.Abs(cwd)
				cwdBridge := filepath.Join(cwdAbs, "bridge")
				if _, err := os.Stat(filepath.Join(cwdBridge, "package.json")); err == nil {
					bridgeDir = cwdBridge
				}
			}
		}
	}
	if _, err := os.Stat(filepath.Join(bridgeDir, "package.json")); err != nil {
		fmt.Fprintf(os.Stderr, "%s Bridge not found. Copy dipper-bot/bridge to ~/.dipper-bot/bridge and run:\n  cd ~/.dipper-bot/bridge && npm install && npm run build\n  Or set DIPPER_BRIDGE_DIR to the bridge directory.\n", logo)
		os.Exit(1)
	}
	dist := filepath.Join(bridgeDir, "dist", "index.js")
	if _, err := os.Stat(dist); err != nil {
		nodeModules := filepath.Join(bridgeDir, "node_modules")
		if _, err := os.Stat(nodeModules); err != nil {
			fmt.Printf("%s Installing bridge dependencies...\n", logo)
			if err := runCmd(bridgeDir, "npm", "install"); err != nil {
				fmt.Fprintf(os.Stderr, "npm install failed: %v\n", err)
				os.Exit(1)
			}
		}
		fmt.Printf("%s Building bridge...\n", logo)
		if err := runCmd(bridgeDir, "npm", "run", "build"); err != nil {
			fmt.Fprintf(os.Stderr, "Build failed: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Printf("%s Starting WhatsApp bridge. Scan the QR code with WhatsApp (Linked Devices).\n\n", logo)
	env := os.Environ()
	env = append(env, "AUTH_DIR="+authDir)
	bridgeURL := "ws://127.0.0.1:3001"
	if cfg != nil && cfg.Channels.WhatsApp.BridgeURL != "" {
		bridgeURL = cfg.Channels.WhatsApp.BridgeURL
	}
	if port := parsePortFromURL(bridgeURL); port != "" {
		env = append(env, "BRIDGE_PORT="+port)
	}
	if cfg != nil && cfg.Channels.WhatsApp.BridgeToken != "" {
		env = append(env, "BRIDGE_TOKEN="+cfg.Channels.WhatsApp.BridgeToken)
	}
	if err := runCmdEnv(bridgeDir, env, "npm", "start"); err != nil {
		fmt.Fprintf(os.Stderr, "Bridge failed: %v\n", err)
		os.Exit(1)
	}
}

func runProviderLogin(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s provider login <openai-codex|github-copilot>\n", exeName())
		os.Exit(1)
	}
	provider := strings.ToLower(strings.TrimSpace(args[0]))
	switch provider {
	case "openai-codex":
		runCodexLogin()
	case "github-copilot":
		runGitHubCopilotLogin()
	default:
		fmt.Fprintf(os.Stderr, "Unknown OAuth provider: %s. Supported: openai-codex, github-copilot\n", provider)
		os.Exit(1)
	}
}

func runCodexLogin() {
	// Try running `codex login` if available (official Codex CLI, shares ~/.codex/auth.json)
	if path, err := exec.LookPath("codex"); err == nil && path != "" {
		fmt.Printf("%s Running: codex login\n\n", logo)
		cmd := exec.Command("codex", "login")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			os.Exit(1)
		}
		accessToken, accountID, _ := providers.LoadCodexAuth()
		if accessToken != "" && accountID != "" {
			fmt.Printf("\n%s Authenticated with OpenAI Codex  %s\n", logo, accountID)
		}
		return
	}
	// Fallback: instruct user to run codex login or use Python
	fmt.Printf("%s OpenAI Codex OAuth\n\n", logo)
	fmt.Println("  Install the Codex CLI and run:")
	fmt.Println("    npm install -g @openai/codex   # or: brew install --cask codex")
	fmt.Println("    codex login")
	fmt.Println()
	fmt.Println("  Or use dipper-bot:")
	fmt.Println("    pip install oauth-cli-kit")
	fmt.Println("    dipper-bot provider login openai-codex")
	fmt.Println()
	fmt.Println("  Tokens are stored at ~/.codex/auth.json (shared across tools).")
}

func runGitHubCopilotLogin() {
	fmt.Printf("%s GitHub Copilot OAuth (device flow)\n\n", logo)
	if err := providers.RunCopilotDeviceFlow(); err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\n%s Authenticated with GitHub Copilot\n", logo)
}

func runCmd(dir, name string, args ...string) error {
	return runCmdEnv(dir, nil, name, args...)
}

func runCmdEnv(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCron(args []string) {
	workspace, args := parseWorkspaceFlag(args)
	if workspace == "" {
		workspace = os.Getenv("DIPPER_WORKSPACE")
	}
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s cron [list|add|remove|run|enable|disable] ... [--workspace PATH]\n", exeName())
		os.Exit(1)
	}
	cfg, _, err := loadConfigWithWorkspace(workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	wsPath, _ := config.GetWorkspacePath(cfg)
	initLogging(cfg, wsPath, false)
	cronPath := filepath.Join(wsPath, "cron", "jobs.json")
	svc := cron.NewService(cronPath, nil)

	sub := strings.ToLower(args[0])
	includeAll := false
	forceRun := false
	for i := 1; i < len(args); i++ {
		if args[i] == "--all" || args[i] == "-a" {
			includeAll = true
		}
		if args[i] == "--force" || args[i] == "-f" {
			forceRun = true
		}
	}

	switch sub {
	case "list":
		jobs, err := svc.ListJobs(includeAll)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(jobs) == 0 {
			fmt.Println("No scheduled jobs.")
			break
		}
		now := time.Now().UnixMilli()
		fmt.Printf("%-10s %-20s %-20s %-10s %s\n", "ID", "Name", "Schedule", "Status", "Next Run")
		fmt.Println(strings.Repeat("-", 80))
		for _, j := range jobs {
			en := "enabled"
			if !j.Enabled {
				en = "disabled"
			}
			sch := formatCronSchedule(&j.Schedule)
			nextMs := j.State.NextRunAtMs
			if nextMs <= 0 && j.Enabled {
				nextMs = cron.ComputeNextRun(&j.Schedule, now)
			}
			next := formatCronNextRun(nextMs, j.Schedule.Tz)
			fmt.Printf("%-10s %-20s %-20s %-10s %s\n", trunc(j.ID, 8), trunc(j.Name, 18), trunc(sch, 18), en, next)
		}
	case "add":
		var name, message, cronExpr, tz, atStr, channel, to string
		var every int
		var deliver bool
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "-n", "--name":
				if i+1 < len(args) {
					name = args[i+1]
					i++
				}
			case "-m", "--message":
				if i+1 < len(args) {
					message = args[i+1]
					i++
				}
			case "-e", "--every":
				if i+1 < len(args) {
					fmt.Sscanf(args[i+1], "%d", &every)
					i++
				}
			case "-c", "--cron":
				if i+1 < len(args) {
					cronExpr = args[i+1]
					i++
				}
			case "--tz":
				if i+1 < len(args) {
					tz = args[i+1]
					i++
				}
			case "--at":
				if i+1 < len(args) {
					atStr = args[i+1]
					i++
				}
			case "--deliver", "-d":
				deliver = true
			case "--channel":
				if i+1 < len(args) {
					channel = args[i+1]
					i++
				}
			case "--to":
				if i+1 < len(args) {
					to = args[i+1]
					i++
				}
			}
		}
		if name == "" || message == "" {
			fmt.Fprintf(os.Stderr, "Usage: %s cron add -n NAME -m MESSAGE [-e SECONDS | --cron EXPR [--tz TZ] | --at TIME] [-d|--deliver] [--channel CH] [--to TO]\n", exeName())
			os.Exit(1)
		}
		if tz != "" && cronExpr == "" {
			fmt.Fprintln(os.Stderr, "Error: --tz can only be used with --cron")
			os.Exit(1)
		}
		var sch cron.Schedule
		if cronExpr != "" {
			sch = cron.Schedule{Kind: cron.ScheduleCron, Expr: cronExpr, Tz: tz}
		} else if atStr != "" {
			t, err := parseCronAt(atStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing --at: %v\n", err)
				os.Exit(1)
			}
			sch = cron.Schedule{Kind: cron.ScheduleAt, AtMs: t.UnixMilli()}
		} else if every > 0 {
			sch = cron.Schedule{Kind: cron.ScheduleEvery, EveryMs: int64(every) * 1000}
		} else {
			fmt.Fprintln(os.Stderr, "Specify one of: -e SECONDS, --cron EXPR, or --at TIME")
			os.Exit(1)
		}
		job, err := svc.AddJob(name, sch, message, deliver, channel, to, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Added job %s (%s)\n", job.Name, job.ID)
	case "enable":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: %s cron enable JOB_ID [--disable]\n", exeName())
			os.Exit(1)
		}
		enableDisable := true
		for i := 2; i < len(args); i++ {
			if args[i] == "--disable" {
				enableDisable = false
				break
			}
		}
		job, err := svc.EnableJob(args[1], enableDisable)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if job == nil {
			fmt.Fprintln(os.Stderr, "Job not found")
			os.Exit(1)
		}
		if enableDisable {
			fmt.Println("Enabled", job.Name)
		} else {
			fmt.Println("Disabled", job.Name)
		}
	case "disable":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: %s cron disable JOB_ID\n", exeName())
			os.Exit(1)
		}
		job, err := svc.EnableJob(args[1], false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if job == nil {
			fmt.Fprintln(os.Stderr, "Job not found")
			os.Exit(1)
		}
		fmt.Println("Disabled", job.Name)
	case "remove":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: %s cron remove JOB_ID\n", exeName())
			os.Exit(1)
		}
		ok, err := svc.RemoveJob(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if !ok {
			fmt.Fprintln(os.Stderr, "Job not found")
			os.Exit(1)
		}
		fmt.Println("Removed", args[1])
	case "run":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: %s cron run JOB_ID [-f|--force]\n", exeName())
			os.Exit(1)
		}
		jobID := args[1]
		ok, err := svc.RunJob(context.Background(), jobID, forceRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if !ok {
			fmt.Fprintln(os.Stderr, "Job not found")
			os.Exit(1)
		}
		fmt.Println("Job executed")
	default:
		fmt.Fprintln(os.Stderr, "Unknown: cron", sub)
		os.Exit(1)
	}
}

func formatCronSchedule(sch *cron.Schedule) string {
	switch sch.Kind {
	case cron.ScheduleEvery:
		if sch.EveryMs > 0 {
			sec := sch.EveryMs / 1000
			return fmt.Sprintf("every %ds", sec)
		}
	case cron.ScheduleCron:
		if sch.Expr != "" {
			if sch.Tz != "" {
				return sch.Expr + " (" + sch.Tz + ")"
			}
			return sch.Expr
		}
	case cron.ScheduleAt:
		if sch.AtMs > 0 {
			return "one-time"
		}
	}
	return "-"
}

func formatCronNextRun(ms int64, tz string) string {
	if ms <= 0 {
		return "-"
	}
	t := time.UnixMilli(ms)
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			t = t.In(loc)
		}
	}
	return t.Format("2006-01-02 15:04")
}

// parseCronAt parses --at value: RFC3339 or unix milliseconds.
func parseCronAt(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.UnixMilli(ms), nil
	}
	return time.Parse(time.RFC3339, s)
}
