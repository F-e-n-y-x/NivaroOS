// nivaroos-backup is the Backup & Sync module: scheduled and
// condition-based copy, mirror and archive jobs to any NivaroOS storage
// (spec docs/specs/2026-09-24-backup-sync-app.md). It is optional and
// installed by installer/install.sh (--with-backup / --without-backup).
//
// Package jobs holds the job side and the REST API, package engine the
// data movement; this file only wires them up.
//
//	nivaroos-backup [flags]                          serve on 127.0.0.1:28643
//	nivaroos-backup [flags] release-scheduled-tasks  hand imported Scheduled
//	                                                 Tasks back to core (the
//	                                                 uninstaller runs this)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
	"github.com/F-e-n-y-x/NivaroOS/services/backup/jobs"
	"github.com/rclone/rclone/lib/atexit"
)

const version = "1.0.0"

type options struct {
	addr, dataDir, runtimePath, coreDB, rcloneConfig string
}

func main() {
	fs := flag.NewFlagSet("nivaroos-backup", flag.ContinueOnError)
	var o options
	fs.StringVar(&o.addr, "addr", jobs.ListenAddr, "address to listen on (loopback only; the gateway routes "+jobs.APIBase+" here)")
	fs.StringVar(&o.dataDir, "data-dir", "/var/lib/nivaroos/backup", "where the job store, run logs and staging live")
	fs.StringVar(&o.runtimePath, "runtime-path", "/var/run/nivaroos", "NivaroOS runtime directory (for locating user-service's JWKS endpoint and the other services)")
	fs.StringVar(&o.coreDB, "core-db", "", "core's database, read-only, for the credentials of saved network shares (default: DBPath from /etc/nivaroos/casaos.conf + /db/casaOS.db)")
	fs.StringVar(&o.rcloneConfig, "rclone-config", "", "rclone config file (default: rclone's own default, the file local-storage maintains)")
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: nivaroos-backup [flags]\n       nivaroos-backup [flags] %s\n\nflags:\n", jobs.ReleaseScheduledTasksCmd)
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return
		}
		os.Exit(2)
	}
	if *showVersion {
		fmt.Println(version)
		return
	}
	switch {
	case fs.NArg() == 0:
		if err := serve(o); err != nil {
			log.Fatal(err)
		}
	case fs.NArg() == 1 && fs.Arg(0) == jobs.ReleaseScheduledTasksCmd:
		os.Exit(release(o))
	default:
		fmt.Fprintf(os.Stderr, "nivaroos-backup: unknown command %q\n", fs.Arg(0))
		fs.Usage()
		os.Exit(2)
	}
}

// serve runs the service until SIGINT/SIGTERM.
func serve(o options) error {
	if err := loopbackOnly(o.addr); err != nil {
		return err
	}
	// rclone's atexit installs its own SIGINT/SIGTERM handler the first
	// time anything registers a cleanup, and that handler calls os.Exit
	// (status 143): a systemctl stop would then skip the post hooks that
	// restart the apps and VMs a running job stopped. Shutdown is ours;
	// rclone's cleanups run at the end of it (atexit.Run below).
	atexit.IgnoreSignals()
	defer atexit.Run()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	eng, closeEngine := newEngine(o)
	defer closeEngine()

	svc, err := jobs.New(jobs.Config{
		DataDir: o.dataDir, RuntimePath: o.runtimePath, Version: version, Engine: eng,
		SMB: &jobs.CoreDBSMB{Path: o.coreDB},
	})
	if svc == nil {
		return err
	}
	if err != nil {
		// Fatal for jobs, not for the process: /health still answers and
		// the app shows store_unavailable instead of disappearing.
		log.Printf("nivaroos-backup: job store unavailable: %v", err)
	} else if err := svc.Start(ctx); err != nil {
		log.Printf("nivaroos-backup: start: %v", err)
	}

	srv := &http.Server{
		Addr:              o.addr,
		Handler:           svc.Handler(),
		ReadHeaderTimeout: 20 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Printf("nivaroos-backup %s listening on %s", version, o.addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	// Running runs see the cancelled context: they stop their engine
	// jobs and run their post hooks before Wait returns. systemd's stop
	// timeout bounds this; whatever doesn't finish is replayed at the
	// next start.
	svc.Wait()
	return nil
}

// newEngine builds the real engine. If it can't start (its spool folder,
// rclone's config), the service still runs with an engine that answers
// engine_unavailable: runs wait instead of failing, and the log says why.
func newEngine(o options) (jobs.EngineClient, func()) {
	eng, err := engine.New(engine.Config{
		StagingDir:       filepath.Join(o.dataDir, jobs.StagingDir),
		SpoolDir:         filepath.Join(o.dataDir, "engine"),
		RcloneConfigPath: o.rcloneConfig,
	})
	if err != nil {
		log.Printf("nivaroos-backup: engine: %v (runs will wait until it starts)", err)
		return unavailableEngine{reason: err.Error()}, func() {}
	}
	return eng, func() {
		if err := eng.Close(); err != nil {
			log.Printf("nivaroos-backup: engine close: %v", err)
		}
	}
}

// release is the release-scheduled-tasks subcommand. It needs the store
// (to know each task's own enabled state) and core; the service itself
// should be stopped first, so it can't take a task back meanwhile.
func release(o options) int {
	svc, err := jobs.New(jobs.Config{
		DataDir: o.dataDir, RuntimePath: o.runtimePath, Version: version,
		Engine: unavailableEngine{reason: "not needed to release tasks"},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "nivaroos-backup: %v\n", err)
		return 1
	}
	defer svc.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	n, err := svc.ReleaseScheduledTasks(ctx)
	fmt.Printf("nivaroos-backup: released %d Scheduled Task(s)\n", n)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nivaroos-backup: %v\n", err)
		return 1
	}
	return 0
}

// loopbackOnly refuses a listen address that isn't loopback: the service
// runs as root and trusts loopback automation.
func loopbackOnly(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("-addr %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("-addr %q: nivaroos-backup only listens on loopback (the gateway routes %s to it)", addr, jobs.APIBase)
	}
	return nil
}
