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
	var apparent bool

	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Open the interactive dashboard",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDashboard(apparent)
		},
	}
	cmd.Flags().BoolVarP(&apparent, "apparent", "a", false, "report apparent size instead of allocated")
	return cmd
}

func runDashboard(apparent bool) error {
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

	m := ui.New(cats, cfg.Roots, cfg.Workers, cwd, apparent)
	_, err = tea.NewProgram(m).Run()
	return err
}
