package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Orwell-Yu/korthex/internal/app"
	"github.com/Orwell-Yu/korthex/internal/config"

	"gopkg.in/yaml.v3"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const logo = `
 ██╗  ██╗ ██████╗ ██████╗ ████████╗██╗  ██╗███████╗██╗  ██╗
 ██║ ██╔╝██╔═══██╗██╔══██╗╚══██╔══╝██║  ██║██╔════╝╚██╗██╔╝
 █████╔╝ ██║   ██║██████╔╝   ██║   ███████║█████╗   ╚███╔╝
 ██╔═██╗ ██║   ██║██╔══██╗   ██║   ██╔══██║██╔══╝   ██╔██╗
 ██║  ██╗╚██████╔╝██║  ██║   ██║   ██║  ██║███████╗██╔╝ ██╗
 ╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═╝   ╚═╝   ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝
`

const splashArt = `
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣾⣿⣿⣿⣿⣷⢸⣿⣿⡜⢯⣷⡌⡻⣿⣿⣿⣆⢈⠻⠿⢿⣿⣿⣿⣿⣿⣿⣷⣦⣤⣀
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⡁⢳⣿⣿⣿⣿⣿⣿⡜⣿⣿⣧⢀⢻⣷⠰⠈⢿⣿⣿⣧⢣⠉⠑⠪⢙⠿⠿⠿⠿⠿⠿⠿⠋⠁
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢀⣱⡇⡞⣿⣿⣿⣿⣿⣿⡇⣿⣿⡏⡄⣧⠹⡇⠧⠈⢻⣿⣿⡇⢧⢢⠀⠀⠑
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢸⣿⣇⢃⢿⣿⣿⣿⣿⣿⣷⣿⣿⠇⢃⣡⣤⡹⠐⣿⣀⢻⣿⣿⢸⡎⠳⡄
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠈⣾⣿⣿⠘⡸⣿⣿⣿⣿⣿⣿⣿⡿⣰⣿⣿⢟⡷⠈⠋⠃⠎⢿⣿⡏⣿⠀⠘⢆
⠀⠀⠀⠀⠀⠀⠀⠀⠀⡐⢹⣿⣿⡐⢡⢹⣿⣿⣿⣿⡏⣿⢣⣿⣿⡑⠁⠔⠀⠉⠉⠢⡘⣿⡇⣿⡇⠀⡀⠡⡀
⠀⠀⠀⠀⠀⠀⠀⠀⠀⡇⠘⣿⣿⣇⠇⢣⢻⣿⣿⣿⡇⢇⣾⣿⣿⡆⢸⣤⡀⠚⢂⠀⢡⢿⡇⣿⡇⠀⢿⠀⠀⠄
⠀⠀⠀⠀⠀⠀⠀⠀⠀⣿⠠⠹⣿⣿⡘⣆⢣⠻⣿⣿⢈⣾⣿⣿⣿⣶⣸⣏⢀⣬⣋⡼⣠⢸⢹⣿⡇⢠⣼⠙⡄
⠀⠀⠀⠀⠀⠀⠀⠀⠀⢹⡇⠁⠹⣿⣇⠹⡃⠃⠙⡇⠘⢿⣿⣿⣿⣿⣿⣏⣓⣉⣭⣴⣿⠘⢸⣿⠁⠘⠋⠀⠹⠄
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠈⢷⠀⠀⠈⢿⣇⠂⣷⠄⠐⠀⠘⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⢠⢸⡏⠀⢀⣠⣴⣾⣿⣶⣄
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠈⢆⠀⠀⠀⠙⠆⠈⠢⠲⠥⣰⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣷⡞⣸⠁⠀⢸⣿⣿⣿⣿⣿⣿⡆
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢶⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⠟⠄⠃⠀⠀⠘⣿⣿⣿⣿⣿⣿⣿
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠙⢿⣿⣿⣿⣿⡏⠹⣿⣿⡿⠫⠊⠀⠀⠀⣶⠀⢻⣿⣿⣿⣿⡿⡇
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠈⠙⠛⠻⠿⠿⠿⢋⠀⠀⠀⠀⢀⣼⣿⡆⠈⣿⣿⣿⡟⣱⡷
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢁⣁⡀⠨⣛⠿⠶⠄⢀⣠⣾⣿⣿⣷⠀⢹⣿⡟⣴⠈⢃⣶⠔
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣾⣿⣿⡄⢸⣿⣿⣿⣿⣿⣿⣿⣿⣿⡄⠈⣿⣿⡿⠀⡀⣿⣷
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢙⠻⣿⣿⢀⠙⠻⠿⣿⣿⣿⣿⣿⣿⡇⠁⣿⠟⡀⠈⣧⢰⣿⠆
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠈⠿⠴⠮⣥⠻⢧⣤⣄⣀⡉⢩⣭⣍⣃⣀⣩⠎⢀⣼⠉⣼⡯
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠑⠁⣛⠓⢒⣒⣢⡭⢁⡈⠿⠿⠟⠹⠛⠁⠀⠀⠀⠰⠃⠂
`

var spinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func main() {
	var configPath string
	var showVersion bool
	flag.StringVar(&configPath, "config", "", "config file path")
	flag.BoolVar(&showVersion, "version", false, "show version")
	flag.Parse()

	if showVersion {
		fmt.Printf("korthex %s (%s) built %s\n", version, commit, date)
		os.Exit(0)
	}

	mgr := config.NewManager(configPath)
	cfg, loadErr := mgr.Load()
	wizard := config.NewWizard(configPath)

	if loadErr != nil || wizard.NeedsSetup() {
		detected := detectKubeconfig()
		var err error
		cfg, err = wizard.Run(detected)
		if err != nil {
			fatal(err)
		}
		if err := mgr.Save(cfg); err != nil {
			fatal(err)
		}
	}

	if err := mgr.Validate(cfg); err != nil {
		fatal(err)
	}

	stopSplash := startSplash()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM)
		<-ch
		cancel()
	}()

	a, err := app.New(cfg)
	stopSplash()
	if err != nil {
		fatal(err)
	}
	defer a.Shutdown()

	if err := a.Run(ctx); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "korthex: %v\n", err)
	os.Exit(1)
}

func detectKubeconfig() config.KubeDetection {
	path := os.Getenv("KUBECONFIG")
	if path != "" {
		// KUBECONFIG may contain multiple colon-separated paths; use the first one.
		if paths := filepath.SplitList(path); len(paths) > 0 {
			path = paths[0]
		}
	} else {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".kube", "config")
	}

	data, err := os.ReadFile(path) //nolint:gosec // kubeconfig path is user-provided by design
	if err != nil {
		return config.KubeDetection{KubeconfigPath: path}
	}

	var kc struct {
		CurrentContext string `yaml:"current-context"`
		Contexts       []struct {
			Name string `yaml:"name"`
		} `yaml:"contexts"`
	}
	if err := yaml.Unmarshal(data, &kc); err != nil {
		return config.KubeDetection{KubeconfigPath: path}
	}

	contexts := make([]string, len(kc.Contexts))
	for i, c := range kc.Contexts {
		contexts[i] = c.Name
	}
	return config.KubeDetection{
		KubeconfigPath: path,
		Contexts:       contexts,
		CurrentContext: kc.CurrentContext,
	}
}

// startSplash displays the animated splash screen and returns a stop function.
// The spinner animates on the "Initializing..." line until stop is called.
func startSplash() func() {
	fmt.Print("\033[2J\033[H") // clear screen
	fmt.Print(logo)

	// Print the splash art and logo side by side is tricky;
	// just stack them vertically with the art indented.
	artLines := strings.Split(strings.TrimRight(splashArt, "\n"), "\n")
	for _, line := range artLines {
		fmt.Println(line)
	}
	fmt.Println()
	fmt.Printf("  Korthex %s\n", version)
	fmt.Println("  Speak to your cluster, see everything.")
	fmt.Println()

	// Save cursor position at the spinner line, then animate.
	// Use ANSI: save cursor (\033[s), restore (\033[u), clear line (\033[2K)
	fmt.Print("  ⠋ Initializing...")
	fmt.Print("\033[s") // save cursor position

	var once sync.Once
	done := make(chan struct{})

	go func() {
		idx := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				idx = (idx + 1) % len(spinnerFrames)
				fmt.Print("\033[u")  // restore cursor
				fmt.Print("\033[2K") // clear line
				fmt.Printf("\r  %s Initializing...", spinnerFrames[idx])
				fmt.Print("\033[s") // save again
			}
		}
	}()

	return func() {
		once.Do(func() { close(done) })
	}
}
