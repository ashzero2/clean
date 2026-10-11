package main

import (
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/rahul-17280/sweep/internal/format"
	"github.com/rahul-17280/sweep/internal/scan"
	"github.com/rahul-17280/sweep/internal/tree"
	"github.com/rahul-17280/sweep/internal/volume"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "sweep:", err)
		os.Exit(1)
	}
}

type options struct {
	apparent       bool
	followSymlinks bool
	oneFileSystem  bool
	noHidden       bool
	exclude        []string
	top            int
	depth          int
	jsonOut        string
	progress       bool
}

func newRootCmd() *cobra.Command {
	var o options

	cmd := &cobra.Command{
		Use:          "sweep [path...]",
		Short:        "Fast disk-space analyzer",
		Long:         "sweep measures disk usage of one or more directory trees.\n\nBy default it prints totals without building a tree, so its memory use stays\nflat even on huge directories. Pass --top or --json to build a full tree.",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				args = []string{"."}
			}
			return run(o, args)
		},
	}

	fl := cmd.Flags()
	fl.BoolVarP(&o.apparent, "apparent", "a", false, "report logical (apparent) size instead of allocated")
	fl.BoolVarP(&o.followSymlinks, "follow-symlinks", "L", false, "follow file symlinks")
	fl.BoolVarP(&o.oneFileSystem, "one-file-system", "x", false, "do not cross filesystem boundaries")
	fl.BoolVar(&o.noHidden, "no-hidden", false, "skip dotfiles and dot-directories")
	fl.StringArrayVar(&o.exclude, "exclude", nil, "path or name glob to skip (repeatable)")
	fl.IntVarP(&o.top, "top", "t", 0, "also list the N largest entries (builds a full tree)")
	fl.IntVar(&o.depth, "depth", 3, "depth of the tree included by --json")
	fl.StringVar(&o.jsonOut, "json", "", `write results as JSON to this file ("-" for stdout)`)
	fl.BoolVarP(&o.progress, "progress", "p", false, "show scan progress on stderr")

	cmd.AddCommand(newCategoriesCmd())
	return cmd
}

func (o options) scanOptions() scan.Options {
	opts := scan.Options{
		FollowSymlinks: o.followSymlinks,
		OneFileSystem:  o.oneFileSystem,
		NoHidden:       o.noHidden,
		Exclude:        o.exclude,
	}
	if o.progress {
		opts.Progress = func(p scan.Progress) {
			fmt.Fprintf(os.Stderr, "\r\033[K  %s entries  %s  %s",
				format.Thousands(p.Entries), format.Bytes(p.Bytes), clip(p.Current, 48))
		}
	}
	return opts
}

func run(o options, paths []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := o.scanOptions()
	defer func() {
		if o.progress {
			fmt.Fprint(os.Stderr, "\r\033[K")
		}
	}()

	needTree := o.top > 0 || o.jsonOut != ""

	if needTree {
		for _, p := range paths {
			if err := runTree(ctx, o, p, opts); err != nil {
				return err
			}
		}
		return nil
	}

	var total scan.Sizes
	for _, p := range paths {
		s, err := scan.Measure(ctx, p, opts)
		printSummary(os.Stdout, o, s)
		total.Allocated += s.Allocated
		total.Apparent += s.Apparent
		total.Entries += s.Entries
		total.Dirs += s.Dirs
		total.Errors += s.Errors
		if err != nil {
			return err
		}
	}
	if len(paths) > 1 {
		fmt.Println()
		fmt.Printf("total across %d paths: %s in %s entries\n",
			len(paths), sizeString(o, total), format.Thousands(total.Entries))
	}
	return nil
}

func runTree(ctx context.Context, o options, root string, opts scan.Options) error {
	res, err := scan.ScanTree(ctx, root, opts)
	if err != nil {
		return err
	}
	// Keep stdout clean when JSON is piped; send the summary to stderr.
	summary := os.Stdout
	if o.jsonOut == "-" {
		summary = os.Stderr
	}
	printSummary(summary, o, res.Sizes)

	if o.top > 0 {
		printTop(summary, o, res.Tree, o.top)
	}
	if o.jsonOut != "" {
		if err := writeJSON(res, o.depth, o.jsonOut); err != nil {
			return err
		}
		if o.jsonOut != "-" {
			fmt.Printf("wrote JSON to %s\n", o.jsonOut)
		}
	}
	return nil
}

func printSummary(w io.Writer, o options, s scan.Sizes) {
	mode := "allocated"
	if o.apparent {
		mode = "apparent"
	}
	fmt.Fprintf(w, "%s  (%s)\n", s.Root, mode)
	fmt.Fprintf(w, "  size     %s\n", sizeString(o, s))
	if v, err := volume.Stat(s.Root); err == nil {
		fmt.Fprintf(w, "  volume   %s used of %s (%s)\n",
			format.Bytes(v.Used), format.Bytes(v.Total), format.Percent(v.Used, v.Total))
	}
	fmt.Fprintf(w, "  entries  %s   dirs %s   errors %s\n",
		format.Thousands(s.Entries), format.Thousands(s.Dirs), format.Thousands(s.Errors))
	fmt.Fprintf(w, "  time     %s\n", s.Duration.Round(time.Millisecond))
}

func sizeString(o options, s scan.Sizes) string {
	if o.apparent {
		return format.Bytes(s.Apparent)
	}
	return format.Bytes(s.Allocated)
}

func printTop(w io.Writer, o options, tr *tree.Tree, n int) {
	idxs := topNodes(tr, n)
	if len(idxs) == 0 {
		return
	}
	width := 0
	for _, i := range idxs {
		if l := len(format.Bytes(nodeSize(tr, i))); l > width {
			width = l
		}
	}
	fmt.Fprintf(w, "\n  top %d largest\n", len(idxs))
	for _, i := range idxs {
		node := tr.Node(i)
		kind := " "
		if node.IsDir() {
			kind = "d"
		}
		fmt.Fprintf(w, "  %-*s  %s %s\n", width, format.Bytes(nodeSize(tr, i)), kind, nodePath(tr, i))
	}
}

func nodeSize(tr *tree.Tree, i int32) int64 {
	return tr.Node(i).Size
}

// topNodes returns the n largest non-duplicate entries using a bounded heap,
// rather than sorting the whole tree.
func topNodes(tr *tree.Tree, n int) []int32 {
	if n <= 0 || tr.Len() <= 1 {
		return nil
	}
	h := &minHeap{}
	for i := int32(1); i < int32(tr.Len()); i++ {
		node := tr.Node(i)
		if node.Flags&tree.FlagHardlinkDup != 0 {
			continue
		}
		switch {
		case h.Len() < n:
			heap.Push(h, heapItem{idx: i, size: node.Size})
		case node.Size > (*h)[0].size:
			(*h)[0] = heapItem{idx: i, size: node.Size}
			heap.Fix(h, 0)
		}
	}
	out := make([]int32, 0, h.Len())
	for _, it := range *h {
		out = append(out, it.idx)
	}
	sort.Slice(out, func(a, b int) bool {
		na, nb := tr.Node(out[a]), tr.Node(out[b])
		if na.Size != nb.Size {
			return na.Size > nb.Size
		}
		return na.Name < nb.Name
	})
	return out
}

// nodePath walks parents to rebuild a path; the depth bound guards a malformed tree.
func nodePath(tr *tree.Tree, i int32) string {
	parts := make([]string, 0, 16)
	for depth := 0; i >= 0 && depth <= tr.Len(); depth++ {
		parts = append(parts, tr.Node(i).Name)
		if i == tr.Root {
			break
		}
		i = tr.Node(i).Parent
	}
	for l, r := 0, len(parts)-1; l < r; l, r = l+1, r-1 {
		parts[l], parts[r] = parts[r], parts[l]
	}
	return filepath.Join(parts...)
}

type heapItem struct {
	idx  int32
	size int64
}

type minHeap []heapItem

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i].size < h[j].size }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x any)        { *h = append(*h, x.(heapItem)) }
func (h *minHeap) Pop() any {
	old := *h
	n := len(old)
	it := old[n-1]
	*h = old[:n-1]
	return it
}

type jsonResult struct {
	Root       string   `json:"root"`
	Allocated  int64    `json:"allocated"`
	Apparent   int64    `json:"apparent"`
	Entries    int64    `json:"entries"`
	Dirs       int64    `json:"dirs"`
	Errors     int64    `json:"errors"`
	DurationMS int64    `json:"duration_ms"`
	Node       jsonNode `json:"node"`
}

type jsonNode struct {
	Name     string     `json:"name"`
	Size     int64      `json:"size"`
	Apparent int64      `json:"apparent"`
	Dir      bool       `json:"dir,omitempty"`
	Children []jsonNode `json:"children,omitempty"`
}

func writeJSON(res *scan.Result, depth int, out string) error {
	if depth < 1 {
		depth = 1
	}
	if depth > 64 {
		depth = 64
	}
	doc := jsonResult{
		Root:       res.Root,
		Allocated:  res.Allocated,
		Apparent:   res.Apparent,
		Entries:    res.Entries,
		Dirs:       res.Dirs,
		Errors:     res.Errors,
		DurationMS: res.Duration.Milliseconds(),
		Node:       buildJSONNode(res.Tree, res.Tree.Root, depth),
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

func buildJSONNode(tr *tree.Tree, i int32, depth int) jsonNode {
	n := tr.Node(i)
	out := jsonNode{Name: n.Name, Size: n.Size, Apparent: n.Apparent, Dir: n.IsDir()}
	if depth > 0 {
		kids := tr.Children(i)
		if len(kids) > 0 {
			out.Children = make([]jsonNode, 0, len(kids))
			for _, c := range kids {
				out.Children = append(out.Children, buildJSONNode(tr, c, depth-1))
			}
		}
	}
	return out
}

// clip keeps the last n runes of s (the informative end of a path), rune-safe.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return "…" + string(r[len(r)-(n-1):])
}
