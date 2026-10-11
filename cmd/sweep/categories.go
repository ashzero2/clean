package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/rahul-17280/sweep/internal/catalog"
	"github.com/rahul-17280/sweep/internal/config"
	"github.com/rahul-17280/sweep/internal/format"
	"github.com/rahul-17280/sweep/internal/scan"
	"github.com/rahul-17280/sweep/internal/scope"
)

func newCategoriesCmd() *cobra.Command {
	var (
		apparent bool
		workers  int
		jsonOut  string
	)

	cmd := &cobra.Command{
		Use:   "categories",
		Short: "Measure the built-in category catalog",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cats, err := catalog.Load(config.CatalogPath())
			if err != nil {
				return err
			}
			if workers <= 0 {
				workers = cfg.Workers
			}

			opts := scan.Options{Exclude: cfg.Exclude}
			results := scope.MeasureAll(ctx, cats, cfg.Roots, opts, workers)
			sort.SliceStable(results, func(i, j int) bool {
				return value(results[i], apparent) > value(results[j], apparent)
			})

			w := io.Writer(os.Stdout)
			if jsonOut == "-" {
				w = os.Stderr
			}
			printCategories(w, results, apparent)

			if jsonOut != "" {
				return writeCategoriesJSON(results, jsonOut)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&apparent, "apparent", "a", false, "report apparent size instead of allocated")
	cmd.Flags().IntVar(&workers, "workers", 0, "parallel scans (0 = from config)")
	cmd.Flags().StringVar(&jsonOut, "json", "", `write results as JSON to this file ("-" for stdout)`)
	return cmd
}

func value(r scope.CategoryResult, apparent bool) int64 {
	if apparent {
		return r.Apparent
	}
	return r.Allocated
}

func printCategories(w io.Writer, results []scope.CategoryResult, apparent bool) {
	var reclaimable int64
	width := 0
	for _, r := range results {
		if l := len(format.Bytes(value(r, apparent))); l > width {
			width = l
		}
		if r.Category.Tier.Reclaimable() {
			reclaimable += value(r, apparent)
		}
	}

	fmt.Fprintf(w, "Potentially reclaimable: %s\n\n", format.Bytes(reclaimable))
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(w, "  %-*s  %-12s %s  (%v)\n", width, "—", r.Category.Tier, r.Category.Label, r.Err)
			continue
		}
		if value(r, apparent) == 0 {
			continue
		}
		fmt.Fprintf(w, "  %-*s  %-12s %s\n", width, format.Bytes(value(r, apparent)), r.Category.Tier, r.Category.Label)
	}
}

type categoryJSON struct {
	ID        string      `json:"id"`
	Label     string      `json:"label"`
	Tier      string      `json:"tier"`
	Kind      string      `json:"kind"`
	Allocated int64       `json:"allocated"`
	Apparent  int64       `json:"apparent"`
	Items     int64       `json:"items"`
	Matches   []matchJSON `json:"matches,omitempty"`
	Error     string      `json:"error,omitempty"`
}

type matchJSON struct {
	Path      string `json:"path"`
	Allocated int64  `json:"allocated"`
	Items     int64  `json:"items"`
}

func writeCategoriesJSON(results []scope.CategoryResult, out string) error {
	doc := make([]categoryJSON, 0, len(results))
	for _, r := range results {
		entry := categoryJSON{
			ID:        r.Category.ID,
			Label:     r.Category.Label,
			Tier:      r.Category.Tier.String(),
			Kind:      r.Category.Kind.String(),
			Allocated: r.Allocated,
			Apparent:  r.Apparent,
			Items:     r.Items,
		}
		if r.Err != nil {
			entry.Error = r.Err.Error()
		}
		for _, m := range r.Matches {
			entry.Matches = append(entry.Matches, matchJSON{Path: m.Path, Allocated: m.Allocated, Items: m.Items})
		}
		doc = append(doc, entry)
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if out == "-" {
		_, err = os.Stdout.Write(b)
		return err
	}
	return os.WriteFile(out, b, 0o644)
}
