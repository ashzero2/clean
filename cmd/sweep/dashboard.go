package main

import (
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/rahul-17280/sweep/internal/catalog"
	"github.com/rahul-17280/sweep/internal/config"
	"github.com/rahul-17280/sweep/internal/ui"
)

func newDashboardCmd() *cobra.Command {
	var (
		apparent  bool
		noDelete  bool
		permanent bool
	)

	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Open the interactive dashboard",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDashboard(apparent, noDelete, permanent)
		},
	}
	cmd.Flags().BoolVarP(&apparent, "apparent", "a", false, "report apparent size instead of allocated")
	cmd.Flags().BoolVar(&noDelete, "no-delete", false, "disable marking and deleting")
	cmd.Flags().BoolVar(&permanent, "permanent", false, "delete permanently instead of moving to Trash")
	return cmd
}

func runDashboard(apparent, noDelete, permanent bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cats, err := catalog.Load(config.CatalogPath())
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "/"
	}

	opts := ui.Options{
		Roots:       cfg.Roots,
		Workers:     cfg.Workers,
		Root:        cwd,
		Apparent:    apparent,
		AllowDelete: !noDelete,
		Permanent:   permanent,
	}
	_, err = tea.NewProgram(ui.New(cats, opts)).Run()
	return err
}
