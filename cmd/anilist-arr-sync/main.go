// Command anilist-arr-sync runs the production service or a single CLI sync.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/server"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/updater"
	"github.com/mattn/go-isatty"
)

var (
	version         = "dev"
	commit          = "unknown"
	date            = "unknown"
	updatePublicKey = ""
)

type options struct {
	configPath, dataDir, bindAddress string
	port                             int
	sync, noBrowser, showVersion     bool
	portSet, bindSet                 bool
}

func parseFlags(args []string, out io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("anilist-arr-sync", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&o.configPath, "config", "", "Path to config.xml")
	fs.StringVar(&o.dataDir, "data-dir", "", "Data directory")
	fs.IntVar(&o.port, "port", 0, "HTTP port override")
	fs.StringVar(&o.bindAddress, "bind-address", "", "Bind address override")
	fs.BoolVar(&o.sync, "sync", false, "Run one sync and exit")
	fs.BoolVar(&o.noBrowser, "no-browser", false, "Suppress browser launch")
	fs.BoolVar(&o.showVersion, "version", false, "Print build version")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "port":
			o.portSet = true
		case "bind-address":
			o.bindSet = true
		}
	})
	if fs.NArg() != 0 {
		return o, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if o.portSet && (o.port < 1 || o.port > 65535) {
		return o, fmt.Errorf("port must be between 1 and 65535")
	}
	return o, nil
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--update-worker" {
		if err := updater.RunWorker(os.Args[2]); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	if handled, err := runPlatformService(); handled || err != nil {
		if err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	return runWithReady(ctx, args, out, func() {})
}

func runWithReady(ctx context.Context, args []string, out io.Writer, ready func()) error {
	o, err := parseFlags(args, out)
	if err != nil {
		return err
	}
	if o.showVersion {
		_, err := fmt.Fprintf(out, "anilist-arr-sync %s (commit %s, built %s)\n", version, commit, date)
		return err
	}
	paths, err := resolvePaths(o, runtime.GOOS, server.IsContainerized(""))
	if err != nil {
		return err
	}
	sessions := server.NewSessionStore(24 * time.Hour)
	sessionDatabase := ""
	sessionSSL := false
	var handoff *server.RestartHandoff
	defer func() {
		if handoff != nil {
			handoff.Close()
		}
	}()
	failRestart := func(err error) error {
		if handoff != nil {
			log.Printf("restart failed: %v", err)
			handoff.Fail()
			handoff.Wait(ctx)
		}
		return err
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		r, err := bootstrap(ctx, o, paths)
		if err != nil {
			return failRestart(err)
		}
		database := dataPath(paths.data, r.cfg.DatabasePath)
		if sessionDatabase != "" && (sessionDatabase != database || sessionSSL != r.listenerCfg.EnableSsl) {
			sessions.RevokeAll()
		}
		sessionDatabase = database
		sessionSSL = r.listenerCfg.EnableSsl
		r.server.SetSessionStore(sessions)
		if o.sync {
			err := r.syncOnce(ctx, out)
			return errors.Join(err, r.close())
		}
		if err := r.server.Start(); err != nil {
			return failRestart(errors.Join(err, r.close()))
		}
		ready()
		if handoff != nil {
			if err := handoff.Ready(ctx, r.server); err != nil {
				log.Printf("restart readiness: %v", err)
			}
		}
		r.start()
		url := r.browserURL()
		log.Printf("listening at %s", url)
		if r.listenerCfg.LaunchBrowser && !o.noBrowser && out == os.Stdout && isatty.IsTerminal(os.Stdout.Fd()) && !server.IsContainerized(r.listenerCfg.UpdateMechanism) {
			if err := openBrowser(url); err != nil {
				log.Printf("browser: %v", err)
			}
		}
		var listenerErr error
		watched := make(chan struct{})
		stopWatching := make(chan struct{})
		go func() {
			defer close(watched)
			select {
			case <-ctx.Done():
				r.server.NotifyLifecycle(server.LifecycleShutdown)
			case listenerErr = <-r.server.Errors():
				r.server.NotifyLifecycle(server.LifecycleShutdown)
			case <-stopWatching:
			}
		}()
		action := r.server.WaitLifecycle()
		close(stopWatching)
		if handoff != nil {
			handoff.Close()
			handoff = nil
		}
		pendingHandoff := r.server.RestartHandoff()
		closeErr := r.close()
		<-watched
		if action == server.LifecycleRestart && ctx.Err() == nil && pendingHandoff != nil {
			if err := pendingHandoff.Start(ctx); err != nil {
				log.Printf("restart handoff unavailable: %v", err)
				pendingHandoff.Close()
			} else {
				handoff = pendingHandoff
			}
		}
		if err := errors.Join(listenerErr, closeErr); err != nil {
			return failRestart(err)
		}
		if action != server.LifecycleRestart || ctx.Err() != nil {
			return nil
		}
		log.Print("restarting from configuration")
	}
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
