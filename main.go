package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jcgay/glane/internal/store"
	"github.com/jcgay/glane/internal/twitter"
)

func dbPath() string {
	if p := os.Getenv("GLANE_DB"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".local", "share", "glane")
	os.MkdirAll(p, 0o755)
	return filepath.Join(p, "glane.db")
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: glane <import|search|serve|enrich> ...")
		os.Exit(2)
	}
	s, err := store.Open(dbPath())
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	switch os.Args[1] {
	case "import":
		cmdImport(s, os.Args[2:])
	case "search":
		cmdSearch(s, os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func cmdImport(s *store.Store, args []string) {
	if len(args) < 2 || args[0] != "twitter" {
		fatal(fmt.Errorf("usage: glane import twitter <archive-dir>"))
	}
	likes, tweets, err := twitter.Import(s, args[1])
	if err != nil {
		fatal(err)
	}
	fmt.Printf("imported %d likes, %d tweets\n", likes, tweets)
}

func cmdSearch(s *store.Store, args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	source := fs.String("source", "", "filter by source")
	limit := fs.Int("limit", 20, "max results")
	fs.Parse(args)
	if fs.NArg() == 0 {
		fatal(fmt.Errorf("usage: glane search <query> [--source] [--limit]"))
	}
	res, err := s.SearchFTS(fs.Arg(0), store.Filter{Source: *source, Limit: *limit})
	if err != nil {
		fatal(err)
	}
	for _, r := range res {
		fmt.Printf("[%s/%s] %s\n    %s\n", r.Source, r.Kind, trunc(r.Text, 120), r.URL)
	}
	fmt.Printf("(%d results)\n", len(res))
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "glane:", err)
	os.Exit(1)
}
