package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/health"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/modulecontract"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/rescue"
	rootruntime "github.com/youtubediscord/RKNnoVPN/daemon/internal/runtime/root"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/watcher"
)

type daemonOptions struct {
	ConfigPath string
	DataDir    string
	LogFile    string
	PIDFile    string
}

type daemonApp struct {
	daemon      *daemon
	modulePaths modulecontract.Paths
	config      profile.LoadedConfig
}

func parseDaemonOptions() daemonOptions {
	defaultPaths := modulecontract.NewPaths("")
	cfgPath := flag.String("config", filepath.Join(defaultPaths.ConfigDir(), "config.json"), "path to config.json")
	dataDir := flag.String("data-dir", defaultPaths.Dir(), "path to data directory")
	logFile := flag.String("log-file", "", "path to log file (default: stderr)")
	pidFile := flag.String("pid-file", "", "path to PID file")
	flag.Parse()
	return daemonOptions{
		ConfigPath: *cfgPath,
		DataDir:    *dataDir,
		LogFile:    *logFile,
		PIDFile:    *pidFile,
	}
}

func runDaemon(opts daemonOptions) error {
	cleanup, err := configureProcess(opts)
	if err != nil {
		return err
	}
	defer cleanup()

	log.Printf("daemon %s starting", Version)

	app, err := newDaemonApp(opts)
	if err != nil {
		return err
	}
	if err := app.start(); err != nil {
		return err
	}
	app.runSignalLoop()
	return nil
}

func configureProcess(opts daemonOptions) (func(), error) {
	var cleanup []func()
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
	cleanupAll := func() {
		for i := len(cleanup) - 1; i >= 0; i-- {
			cleanup[i]()
		}
	}

	if opts.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(opts.LogFile), 0750); err != nil {
			return nil, fmt.Errorf("mkdir log dir: %w", err)
		}
		f, err := os.OpenFile(opts.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		log.SetOutput(f)
		cleanup = append(cleanup, func() {
			_ = f.Close()
		})
	}

	if opts.PIDFile != "" {
		pid := os.Getpid()
		if err := writePID(opts.PIDFile, pid); err != nil {
			cleanupAll()
			return nil, fmt.Errorf("write pid: %w", err)
		}
		pidRefresh := time.AfterFunc(6*time.Second, func() {
			if err := refreshPIDIfMissingOrOwned(opts.PIDFile, pid); err != nil {
				log.Printf("refresh pid warning: %v", err)
			}
		})
		cleanup = append(cleanup, func() {
			pidRefresh.Stop()
			_ = removePIDIfOwned(opts.PIDFile, pid)
		})
	}

	for _, sub := range []string{"run", "data", "logs", "config/rendered", "backup"} {
		if err := os.MkdirAll(filepath.Join(opts.DataDir, sub), 0750); err != nil {
			cleanupAll()
			return nil, fmt.Errorf("mkdir %s: %w", sub, err)
		}
	}

	return cleanupAll, nil
}

func newDaemonApp(opts daemonOptions) (*daemonApp, error) {
	loadedConfig, err := profile.LoadConfigProjection(opts.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	cfg := loadedConfig.Config
	log.Printf("config loaded from %s", opts.ConfigPath)

	coreLogger := log.New(log.Writer(), "[core] ", log.LstdFlags)
	healthLogger := log.New(log.Writer(), "[health] ", log.LstdFlags)
	rescueLogger := log.New(log.Writer(), "[rescue] ", log.LstdFlags)
	watchLogger := log.New(log.Writer(), "[netwatch] ", log.LstdFlags)

	coreMgr := core.NewCoreManager(cfg, opts.DataDir, coreLogger)
	rescueMgr := rescue.NewRescueManager(coreMgr, cfg, opts.DataDir, cfg.Rescue.MaxAttempts, rescueCooldown(cfg.Rescue.CooldownSec), rescueLogger)

	healthConfig := newHealthRuntimeConfig(cfg)
	healthMon := health.NewHealthMonitor(coreMgr, healthConfig.Interval, healthConfig.Threshold, healthConfig.TProxyPort, healthConfig.DNSPort, healthConfig.RouteMark, healthConfig.CheckURL, healthConfig.Timeout, healthLogger)
	applyHealthRuntimeConfig(healthMon, healthConfig)

	var d *daemon
	netWatcher := watcher.NewNetworkWatcher(opts.DataDir, rootruntime.BuildScriptEnv(cfg, opts.DataDir), func() error {
		if d == nil {
			return nil
		}
		_, err := d.runtimeV2.HandleNetworkChange()
		return err
	}, watchLogger)

	modulePaths := modulecontract.NewPaths(opts.DataDir)
	d = &daemon{
		cfg:         cfg,
		cfgPath:     opts.ConfigPath,
		profilePath: loadedConfig.ProfilePath,
		dataDir:     opts.DataDir,
		coreMgr:     coreMgr,
		healthMon:   healthMon,
		rescueMgr:   rescueMgr,
		netWatcher:  netWatcher,
		ipcServer:   ipc.NewServer(modulePaths.DaemonSocket()),
	}
	d.initRuntimeV2()
	d.runtimeV2.SetOperationLogger(logRuntimeOperation)
	d.installRescueCallbacks()

	return &daemonApp{
		daemon:      d,
		modulePaths: modulePaths,
		config:      loadedConfig,
	}, nil
}

func rescueCooldown(seconds int) time.Duration {
	cooldown := time.Duration(seconds) * time.Second
	if cooldown <= 0 {
		return 60 * time.Second
	}
	return cooldown
}

func (a *daemonApp) start() error {
	if err := a.daemon.registerHandlers(); err != nil {
		return fmt.Errorf("ipc handler registration: %w", err)
	}
	if err := a.daemon.ipcServer.Start(); err != nil {
		return fmt.Errorf("ipc start: %w", err)
	}
	a.maybeAutostart()
	return nil
}

func (a *daemonApp) maybeAutostart() {
	cfg := a.config.Config
	if cfg.Autostart && fileMissing(a.modulePaths.ManualFlag()) {
		log.Printf("autostart enabled, starting proxy")
		if err := a.daemon.syncRuntimeV2DesiredState(); err != nil {
			log.Printf("autostart desired state sync failed: %v", err)
		}
		if _, err := a.daemon.runtimeV2.Start(); err != nil {
			log.Printf("autostart failed: %v", err)
		}
	} else if cfg.Autostart {
		log.Printf("autostart skipped: manual reset flag is present")
	}
}

func (a *daemonApp) runSignalLoop() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGUSR1)

	log.Printf("daemon ready, waiting for signals")
	for sig := range sigCh {
		switch sig {
		case syscall.SIGTERM, syscall.SIGINT:
			log.Printf("received %s, shutting down", sig)
			a.daemon.shutdown()
			log.Printf("goodbye")
			return
		case syscall.SIGHUP:
			log.Printf("received SIGHUP, reloading config")
			if err := a.daemon.reloadConfig(); err != nil {
				log.Printf("reload config failed: %v", err)
			}
		case syscall.SIGUSR1:
			log.Printf("received SIGUSR1, dumping state")
			a.daemon.dumpState()
		}
	}
}
