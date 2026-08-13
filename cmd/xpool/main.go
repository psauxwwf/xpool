package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"xpool/internal/xpool"
)

const defaultLogLevel = "info"

type rootOptions struct {
	LogLevel string
	LogPath  string
}

type runOptions struct {
	InputPath         string
	ConfigPath        string
	XrayPath          string
	APIAddress        string
	RotationInterval  string
	CheckURLsRaw      string
	CheckInterval     string
	ReadyTTL          string
	StartupTimeout    string
	CheckTimeout      string
	PingTimeout       string
	Sampling          int
	GeneratedLogLevel string
}

func main() {
	if err := fang.Execute(context.Background(), rootCmd(), fang.WithoutVersion()); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var options rootOptions
	root := &cobra.Command{
		Use:           "xpool",
		Short:         "Run an Xray proxy ready pool",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			return configureLogger(options.LogLevel, options.LogPath)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}
	root.SetHelpCommand(&cobra.Command{Hidden: true})
	root.PersistentFlags().StringVar(&options.LogLevel, "log-level", defaultLogLevel, "log level: debug, info, warn, error")
	root.PersistentFlags().StringVar(&options.LogPath, "log-path", "", "optional JSON log file path")
	root.AddCommand(runCmd())

	return root
}

func runCmd() *cobra.Command {
	options := runOptions{
		InputPath:         xpool.DefaultInputPath,
		ConfigPath:        xpool.DefaultConfigPath,
		XrayPath:          xpool.DefaultXrayPath,
		APIAddress:        xpool.DefaultAPIAddress,
		RotationInterval:  xpool.DefaultRotationInterval.String(),
		CheckURLsRaw:      xpool.DefaultCheckURL,
		CheckInterval:     xpool.DefaultCheckInterval.String(),
		ReadyTTL:          xpool.DefaultReadyTTL.String(),
		StartupTimeout:    xpool.DefaultStartupTimeout.String(),
		CheckTimeout:      xpool.DefaultCheckTimeout.String(),
		PingTimeout:       xpool.DefaultPingTimeout.String(),
		Sampling:          xpool.DefaultSampling,
		GeneratedLogLevel: xpool.DefaultGeneratedLogLevel,
	}
	cmd := &cobra.Command{
		Use:           "run [proxies.txt]",
		Short:         "Generate config, run Xray, and rotate ready outbounds",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				options.InputPath = args[0]
			}
			rotationInterval, err := xpool.ParseDuration("rotation-interval", options.RotationInterval)
			if err != nil {
				return err
			}
			checkURLs, err := xpool.ParseURLs(options.CheckURLsRaw)
			if err != nil {
				return err
			}
			checkInterval, err := xpool.ParseDuration("check-interval", options.CheckInterval)
			if err != nil {
				return err
			}
			readyTTL, err := xpool.ParseDuration("ready-ttl", options.ReadyTTL)
			if err != nil {
				return err
			}
			startupTimeout, err := xpool.ParseDuration("startup-timeout", options.StartupTimeout)
			if err != nil {
				return err
			}
			checkTimeout, err := xpool.ParseDuration("check-timeout", options.CheckTimeout)
			if err != nil {
				return err
			}
			pingTimeout, err := xpool.ParseDuration("ping-timeout", options.PingTimeout)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return xpool.Run(ctx, xpool.Options{
				InputPath:         options.InputPath,
				ConfigPath:        options.ConfigPath,
				XrayPath:          options.XrayPath,
				APIAddress:        options.APIAddress,
				RotationInterval:  rotationInterval,
				CheckURLs:         checkURLs,
				CheckInterval:     checkInterval,
				ReadyTTL:          readyTTL,
				StartupTimeout:    startupTimeout,
				CheckTimeout:      checkTimeout,
				PingTimeout:       pingTimeout,
				Sampling:          options.Sampling,
				GeneratedLogLevel: options.GeneratedLogLevel,
			})
		},
	}
	cmd.Flags().StringVar(&options.ConfigPath, "config", options.ConfigPath, "Xray config path")
	cmd.Flags().StringVar(&options.XrayPath, "xray", options.XrayPath, "Xray executable path")
	cmd.Flags().StringVar(&options.APIAddress, "api-addr", options.APIAddress, "Xray API address")
	cmd.Flags().StringVar(&options.RotationInterval, "rotation-interval", options.RotationInterval, "ready outbound rotation interval")
	cmd.Flags().StringVar(&options.CheckURLsRaw, "check-url", options.CheckURLsRaw, "comma-separated full-download check URLs")
	cmd.Flags().StringVar(&options.CheckInterval, "check-interval", options.CheckInterval, "background check interval")
	cmd.Flags().StringVar(&options.ReadyTTL, "ready-ttl", options.ReadyTTL, "maximum age of a successful check in the ready pool")
	cmd.Flags().StringVar(&options.StartupTimeout, "startup-timeout", options.StartupTimeout, "maximum wait for Xray API and initial ready pool")
	cmd.Flags().StringVar(&options.CheckTimeout, "check-timeout", options.CheckTimeout, "full-download background check timeout")
	cmd.Flags().StringVar(&options.PingTimeout, "ping-timeout", options.PingTimeout, "Xray burst observatory ping timeout")
	cmd.Flags().IntVar(&options.Sampling, "sampling", options.Sampling, "Xray burst observatory sampling count")
	cmd.Flags().StringVar(&options.GeneratedLogLevel, "xray-log-level", options.GeneratedLogLevel, "generated Xray log level")

	return cmd
}

func configureLogger(levelText, logPath string) error {
	var parsedLevel slog.Level
	if err := parsedLevel.UnmarshalText([]byte(levelText)); err != nil {
		return fmt.Errorf("invalid log level %q: %w", levelText, err)
	}

	h := []slog.Handler{
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			AddSource: true,
			Level:     parsedLevel,
		}),
	}

	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return fmt.Errorf("failed to create log dir for %q: %w", logPath, err)
		}

		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("failed to open log file %q: %w", logPath, err)
		}

		h = append(h,
			slog.NewJSONHandler(logFile, &slog.HandlerOptions{
				AddSource: true,
				Level:     parsedLevel,
			}),
		)
	}

	slog.SetDefault(slog.New(slog.NewMultiHandler(h...)))

	return nil
}
