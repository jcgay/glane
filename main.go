package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jcgay/glane/internal/embed"
	"github.com/jcgay/glane/internal/enrich"
	"github.com/jcgay/glane/internal/github"
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
		fmt.Fprintln(os.Stderr, "usage: glane <import|sync|search|enrich|serve> ...")
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
	case "sync":
		cmdSync(s, os.Args[2:])
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

func cmdSync(s *store.Store, args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: glane sync <github>"))
	}
	switch args[0] {
	case "github":
		token := os.Getenv("GITHUB_TOKEN")
		if token == "" {
			fatal(fmt.Errorf("set GITHUB_TOKEN to sync GitHub stars"))
		}
		n, err := github.Sync(s, token, &http.Client{Timeout: 30 * time.Second})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("synced %d new stars\n", n)
	default:
		fatal(fmt.Errorf("unknown sync source %q (known: github)", args[0]))
	}
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
	since := fs.String("since", "", "only items on/after this date (YYYY or YYYY-MM-DD)")
	fs.Parse(flagArgs)
	if query == "" {
		fatal(fmt.Errorf("usage: glane search <query> [--source X] [--limit N] [--since YYYY[-MM-DD]]  (flags after the query)"))
	}
	sinceTs, err := parseSince(*since)
	if err != nil {
		fatal(err)
	}
	filter := store.Filter{Source: *source, Limit: *limit, Since: sinceTs}

	res, err := search.Hybrid(s, embed.FromEnv(), query, filter)
	if err != nil {
		fatal(err)
	}
	for _, r := range res {
		fmt.Printf("[%s/%s] %s\n    %s\n", r.Source, r.Kind, trunc(r.Text, 120), r.URL)
	}
	fmt.Printf("(%d results)\n", len(res))
}

// parseSince converts "YYYY" or "YYYY-MM-DD" to a Unix timestamp (start of that day/year).
func parseSince(v string) (int64, error) {
	if v == "" {
		return 0, nil
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t.Unix(), nil
	}
	if t, err := time.Parse("2006", v); err == nil {
		return t.Unix(), nil
	}
	return 0, fmt.Errorf("invalid --since %q (want YYYY or YYYY-MM-DD)", v)
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
	return cutRunes(s, n) + "…"
}

// cutRunes truncates s to at most n runes, never splitting a multibyte rune.
func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "glane:", err)
	os.Exit(1)
}
