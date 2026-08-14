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

	appconfig "xpool/internal/config"
	"xpool/internal/xpool"
)

type rootOptions struct {
	ConfigFilePath string
	SaveConfig     bool
	MinimumLevel   string
	FilePath       string
}

func main() {
	if err := fang.Execute(context.Background(), rootCmd(), fang.WithoutVersion()); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	options := rootOptions{ConfigFilePath: appconfig.DefaultConfigPath}
	root := &cobra.Command{
		Use:           "xpool",
		Short:         "Run an Xray proxy ready pool",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if options.SaveConfig {
				return saveDefaultConfig(options.ConfigFilePath)
			}

			return cmd.Help()
		},
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}
	root.SetHelpCommand(&cobra.Command{Hidden: true})
	root.PersistentFlags().StringVar(&options.ConfigFilePath, "config", options.ConfigFilePath, "path to yaml config")
	root.PersistentFlags().StringVar(&options.MinimumLevel, "log-level", "", "override yaml log level: debug, info, warn, error")
	root.PersistentFlags().StringVar(&options.FilePath, "log-path", "", "optional JSON log file path")
	root.Flags().BoolVar(&options.SaveConfig, "save-config", false, "save default yaml config and exit")
	root.AddCommand(runCmd(&options))

	return root
}

func runCmd(options *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "run",
		Short:         "Generate config, run Xray, and rotate ready outbounds",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return runConfigured(ctx, cmd, *options)
		},
	}

	return cmd
}

func runConfigured(ctx context.Context, cmd *cobra.Command, options rootOptions) error {
	fileConfig, err := appconfig.New(commandConfigPath(cmd, options.ConfigFilePath))
	if err != nil {
		return err
	}
	if flagChanged(cmd, "log-level") {
		fileConfig.Log.MinimumLevel = options.MinimumLevel
	}
	if flagChanged(cmd, "log-path") {
		fileConfig.Log.FilePath = options.FilePath
	}
	if err := configureLogger(fileConfig.Log.MinimumLevel, fileConfig.Log.FilePath); err != nil {
		return err
	}

	return xpool.Run(ctx, fileConfig)
}

func commandConfigPath(cmd *cobra.Command, configuredPath string) string {
	if flagChanged(cmd, "config") {
		return configuredPath
	}

	return appconfig.ExistingPath("")
}

func flagChanged(cmd *cobra.Command, name string) bool {
	flag := cmd.Root().PersistentFlags().Lookup(name)
	return flag != nil && flag.Changed
}

func saveDefaultConfig(path string) error {
	if err := appconfig.Save(path, appconfig.Default()); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "saved config to %s\n", path)
	return nil
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
