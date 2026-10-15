package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/F-e-n-y-x/NivaroOS/cli/update"
	"github.com/spf13/cobra"
)

// nivaroos update: the server self-updater (docs/guides/server-updates.md). With
// no subcommand it shows the status.
var updateCmd = &cobra.Command{
	Use:     "update",
	Short:   "Check for, install or roll back NivaroOS server updates",
	GroupID: RootGroupID,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		printStatus(update.LoadState(), update.LoadSettings())
		return nil
	},
}

func updateCtx() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func needRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("needs root: sudo nivaroos update ...")
	}
	return nil
}

func printStatus(st update.State, set update.Settings) {
	cur := st.Current
	if cur == "" {
		cur = "built from source"
	}
	fmt.Printf("NivaroOS %s - channel %s, automatic: %s (at about %s)\n", cur, set.Channel, map[string]string{"install": "install at night", "notify": "tell me only"}[set.Auto], set.Window)
	if !st.LastCheck.IsZero() {
		fmt.Printf("Last check  %s\n", st.LastCheck.Local().Format("2 Jan 15:04"))
	}
	fmt.Printf("State       %s", st.State)
	if st.Step != "" {
		fmt.Printf(" (%s)", st.Step)
	}
	fmt.Println()
	if a := st.Available; a != nil {
		fmt.Printf("Available   %s (%s, %d MB) - sudo nivaroos update apply\n", a.Version, a.Channel, a.Size>>20)
	}
	for _, s := range []string{st.Skipped, st.Unsupported, st.LastError} {
		if s != "" {
			fmt.Println("  " + s)
		}
	}
	if r := st.LastResult; r != nil {
		fmt.Printf("Last update %s -> %s: %s %s\n", r.From, r.To, strings.ReplaceAll(r.Result, "_", " "), r.Reason)
	}
}

func init() {
	check := &cobra.Command{
		Use:   "check",
		Short: "Ask GitHub for a newer server release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := needRoot(); err != nil {
				return err
			}
			ctx, cancel := updateCtx()
			defer cancel()
			st, err := update.Check(ctx)
			printStatus(st, update.LoadSettings())
			return err
		},
	}

	var mode, sums, sig string
	var allowDowngrade bool
	apply := &cobra.Command{
		Use:   "apply [nivaroos-server-X.Y.Z-linux-ARCH.tar.gz --sums SHA256SUMS --sig SHA256SUMS.minisig]",
		Short: "Download, verify and install the newest release now (or a local bundle)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := needRoot(); err != nil {
				return err
			}
			ctx, cancel := updateCtx()
			defer cancel()
			o := update.ApplyOptions{Manual: true, Mode: mode, Sums: sums, Sig: sig, AllowDowngrade: allowDowngrade, Out: os.Stdout}
			if len(args) == 1 {
				o.Local = args[0]
			}
			return update.Apply(ctx, o)
		},
	}
	apply.Flags().StringVar(&mode, "backups", "wait", "running backups: wait (for them to finish) or cancel_backups")
	apply.Flags().StringVar(&sums, "sums", "", "SHA256SUMS of a local bundle")
	apply.Flags().StringVar(&sig, "sig", "", "SHA256SUMS.minisig of a local bundle")
	apply.Flags().BoolVar(&allowDowngrade, "allow-downgrade", false, "install a local bundle that is not newer")

	var restoreData, auto bool
	var reason string
	rollback := &cobra.Command{
		Use:   "rollback",
		Short: "Put the previous release back (every file the last update changed)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := needRoot(); err != nil {
				return err
			}
			ctx, cancel := updateCtx()
			defer cancel()
			return update.Rollback(ctx, restoreData, reason, auto, os.Stdout)
		},
	}
	rollback.Flags().BoolVar(&restoreData, "restore-data", false, "also put the databases back as they were before the update (changes made since are lost)")
	rollback.Flags().StringVar(&reason, "reason", "", "why (shown in the history)")
	rollback.Flags().BoolVar(&auto, "auto", false, "the watchdog's rollback: the version is not offered again")
	_ = rollback.Flags().MarkHidden("auto")

	var channel, autoMode, window string
	set := &cobra.Command{
		Use:   "set",
		Short: "Change the update channel, automatic installs, or their time",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := needRoot(); err != nil {
				return err
			}
			s := update.LoadSettings()
			if channel != "" {
				s.Channel = channel
			}
			if autoMode != "" {
				s.Auto = autoMode
			}
			if window != "" {
				s.Window = window
			}
			if (s.Channel != "stable" && s.Channel != "beta") || (s.Auto != "install" && s.Auto != "notify") {
				return errors.New("--channel stable|beta, --auto install|notify")
			}
			if err := update.SaveSettings(s); err != nil {
				return err
			}
			printStatus(update.LoadState(), update.LoadSettings())
			return nil
		},
	}
	set.Flags().StringVar(&channel, "channel", "", "stable or beta")
	set.Flags().StringVar(&autoMode, "auto", "", "install (at night) or notify (tell me only)")
	set.Flags().StringVar(&window, "window", "", "time of the automatic install, HH:MM local")

	history := &cobra.Command{
		Use:   "history",
		Short: "Past updates and rollbacks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, r := range update.History() {
				fmt.Printf("%s  %s -> %s  %s %s\n", r.At.Local().Format("2006-01-02 15:04"), r.From, r.To, r.Result, r.Reason)
			}
			return nil
		},
	}

	// Run by the systemd units, not by hand.
	run := &cobra.Command{
		Use:    "run",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := updateCtx()
			defer cancel()
			return update.Run(ctx, os.Stdout)
		},
	}
	resume := &cobra.Command{
		Use:    "resume",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := updateCtx()
			defer cancel()
			return update.Resume(ctx, os.Stdout)
		},
	}

	download := &cobra.Command{
		Use:   "download",
		Short: "Download the newest release (nothing is installed)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := needRoot(); err != nil {
				return err
			}
			ctx, cancel := updateCtx()
			defer cancel()
			return update.Download(ctx, os.Stdout)
		},
	}
	verify := &cobra.Command{
		Use:   "verify",
		Short: "Check the downloaded release's signature and files (nothing is installed)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := needRoot(); err != nil {
				return err
			}
			ctx, cancel := updateCtx()
			defer cancel()
			return update.Verify(ctx, os.Stdout)
		},
	}

	updateCmd.AddCommand(check, download, verify, apply, rollback, set, history, run, resume)
	rootCmd.AddCommand(updateCmd)
}
