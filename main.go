package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jcgay/glane/internal/embed"
	"github.com/jcgay/glane/internal/enrich"
	"github.com/jcgay/glane/internal/search"
	"github.com/jcgay/glane/internal/store"
	"github.com/jcgay/glane/internal/twitter"
	"github.com/jcgay/glane/internal/web"
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
	case "serve":
		cmdServe(s, os.Args[2:])
	case "enrich":
		cmdEnrich(s, os.Args[2:])
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

// splitQueryArgs separates a leading multi-word query from trailing flags.
// The query is all args up to the first "-"-prefixed token; the remainder are
// flag args. Multi-word queries work unquoted; flags must come after the query.
func splitQueryArgs(args []string) (query string, flagArgs []string) {
	i := 0
	for i < len(args) && !strings.HasPrefix(args[i], "-") {
		i++
	}
	return strings.Join(args[:i], " "), args[i:]
}

func cmdSearch(s *store.Store, args []string) {
	query, flagArgs := splitQueryArgs(args)
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	source := fs.String("source", "", "filter by source")
	limit := fs.Int("limit", 20, "max results")
	fs.Parse(flagArgs)
	if query == "" {
		fatal(fmt.Errorf("usage: glane search <query> [--source X] [--limit N]  (flags after the query)"))
	}
	filter := store.Filter{Source: *source, Limit: *limit}
	ftsRes, err := s.SearchFTS(query, filter)
	if err != nil {
		fatal(err)
	}

	results := ftsRes
	// Optional semantic layer, fused via RRF when an embeddings endpoint exists.
	// Any failure here (no endpoint, network error, no stored vectors) falls
	// back silently to FTS-only results.
	if c := embed.FromEnv(); c != nil {
		if sem := semanticResults(s, c, query, filter); sem != nil {
			results = fuse(s, ftsRes, sem, *limit)
		}
	}
	for _, r := range results {
		fmt.Printf("[%s/%s] %s\n    %s\n", r.Source, r.Kind, trunc(r.Text, 120), r.URL)
	}
	fmt.Printf("(%d results)\n", len(results))
}

func semanticResults(s *store.Store, c *embed.Client, q string, f store.Filter) []int64 {
	qv, err := c.Embed(context.Background(), []string{q})
	if err != nil || len(qv) == 0 {
		return nil // fail soft to FTS only
	}
	embs, err := s.AllEmbeddings(c.Model, f)
	if err != nil || len(embs) == 0 {
		return nil
	}
	return search.SemanticIDs(qv[0], embs, 100)
}

func fuse(s *store.Store, fts []store.Result, semIDs []int64, limit int) []store.Result {
	ftsIDs := make([]int64, len(fts))
	for i, r := range fts {
		ftsIDs[i] = r.ID
	}
	fused := search.RRF([][]int64{ftsIDs, semIDs}, 60)
	if limit > 0 && len(fused) > limit {
		fused = fused[:limit]
	}
	items, err := s.GetItems(fused)
	if err != nil {
		return fts // DB error: fall back to full-text results rather than showing nothing
	}
	out := make([]store.Result, 0, len(fused))
	for _, id := range fused {
		if it, ok := items[id]; ok {
			out = append(out, store.Result{Item: it})
		}
	}
	return out
}

func cmdServe(s *store.Store, args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 8080, "listen port")
	fs.Parse(args)
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	fmt.Printf("glane serving on http://%s\n", addr)
	if err := web.Serve(s, addr); err != nil {
		fatal(err)
	}
}

func cmdEnrich(s *store.Store, args []string) {
	fs := flag.NewFlagSet("enrich", flag.ExitOnError)
	limit := fs.Int("limit", 100, "max items to fetch this run")
	fs.Parse(args)
	done, failed, err := enrich.Run(s, enrich.DefaultClient(), embed.FromEnv(), *limit)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("enriched %d, failed %d\n", done, failed)
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
