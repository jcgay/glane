package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jcgay/glane/internal/bluesky"
	"github.com/jcgay/glane/internal/embed"
	"github.com/jcgay/glane/internal/enrich"
	"github.com/jcgay/glane/internal/github"
	"github.com/jcgay/glane/internal/mastodon"
	"github.com/jcgay/glane/internal/search"
	"github.com/jcgay/glane/internal/store"
	"github.com/jcgay/glane/internal/summarize"
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
		fmt.Fprintln(os.Stderr, "usage: glane <import|sync|search|enrich|summarize|tags|serve> ...")
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
	case "summarize":
		cmdSummarize(s, os.Args[2:])
	case "tags":
		cmdTags(s)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func syncClient() *http.Client { return &http.Client{Timeout: 30 * time.Second} }

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
		fatal(fmt.Errorf("usage: glane sync <github, mastodon, bluesky, all>"))
	}
	switch args[0] {
	case "github":
		token := os.Getenv("GITHUB_TOKEN")
		if token == "" {
			fatal(fmt.Errorf("set GITHUB_TOKEN to sync GitHub stars"))
		}
		n, err := github.Sync(s, token, syncClient())
		if err != nil {
			fatal(err)
		}
		fmt.Printf("synced %d new stars\n", n)
	case "mastodon":
		base, token := os.Getenv("GLANE_MASTODON_URL"), os.Getenv("GLANE_MASTODON_TOKEN")
		if base == "" || token == "" {
			fatal(fmt.Errorf("set GLANE_MASTODON_URL and GLANE_MASTODON_TOKEN to sync Mastodon"))
		}
		n, err := mastodon.Sync(s, base, token, syncClient())
		if err != nil {
			fatal(err)
		}
		fmt.Printf("synced %d new mastodon items\n", n)
	case "bluesky":
		handle, pw := os.Getenv("GLANE_BLUESKY_HANDLE"), os.Getenv("GLANE_BLUESKY_APP_PASSWORD")
		if handle == "" || pw == "" {
			fatal(fmt.Errorf("set GLANE_BLUESKY_HANDLE and GLANE_BLUESKY_APP_PASSWORD to sync Bluesky"))
		}
		n, err := bluesky.Sync(s, handle, pw, syncClient())
		if err != nil {
			fatal(err)
		}
		fmt.Printf("synced %d new bluesky items\n", n)
	case "all":
		cmdSyncAll(s)
	default:
		fatal(fmt.Errorf("unknown sync source %q (known: github, mastodon, bluesky, all)", args[0]))
	}
}

// cmdSyncAll runs every connector whose config is present, skipping the rest
// (reported, not errored). One connector's failure is logged and makes the whole
// command exit non-zero (so scheduled runs are monitorable), but does not stop
// the others; each connector advances only its own cursor.
func cmdSyncAll(s *store.Store) {
	hc := syncClient()
	total := 0
	failed := false
	var ran, skipped []string

	record := func(name string, n int, err error) {
		total += n
		if err != nil {
			failed = true
			fmt.Fprintf(os.Stderr, "glane: %s sync error: %v\n", name, err)
			ran = append(ran, fmt.Sprintf("%s:%d(partial)", name, n))
		} else {
			ran = append(ran, fmt.Sprintf("%s:%d", name, n))
		}
	}

	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		n, err := github.Sync(s, tok, hc)
		record("github", n, err)
	} else {
		skipped = append(skipped, "github")
	}
	if base, tok := os.Getenv("GLANE_MASTODON_URL"), os.Getenv("GLANE_MASTODON_TOKEN"); base != "" && tok != "" {
		n, err := mastodon.Sync(s, base, tok, hc)
		record("mastodon", n, err)
	} else {
		skipped = append(skipped, "mastodon")
	}
	if h, pw := os.Getenv("GLANE_BLUESKY_HANDLE"), os.Getenv("GLANE_BLUESKY_APP_PASSWORD"); h != "" && pw != "" {
		n, err := bluesky.Sync(s, h, pw, hc)
		record("bluesky", n, err)
	} else {
		skipped = append(skipped, "bluesky")
	}

	fmt.Printf("synced %d new items [%s]", total, strings.Join(ran, " "))
	if len(skipped) > 0 {
		fmt.Printf(" (skipped, not configured: %s)", strings.Join(skipped, ", "))
	}
	fmt.Println()
	if failed {
		os.Exit(1)
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
	tag := fs.String("tag", "", "filter by tag")
	fs.Parse(flagArgs)
	sinceTs, err := parseSince(*since)
	if err != nil {
		fatal(err)
	}
	filter := store.Filter{Source: *source, Limit: *limit, Since: sinceTs, Tag: *tag}

	var res []store.Result
	if query == "" {
		if *tag == "" {
			fatal(fmt.Errorf("usage: glane search <query> [--source X] [--tag T] [--since Y] [--limit N]"))
		}
		res, err = s.ByTag(*tag, filter)
	} else {
		res, err = search.Hybrid(s, embed.FromEnv(), query, filter)
	}
	if err != nil {
		fatal(err)
	}
	if err := s.AttachTags(res); err != nil {
		fatal(err)
	}
	for _, r := range res {
		snippet := r.Text
		if r.ArticleSummary != "" {
			snippet = r.ArticleSummary
		}
		tagStr := ""
		if len(r.Tags) > 0 {
			tagStr = "  #" + strings.Join(r.Tags, " #")
		}
		fmt.Printf("[%s/%s] %s%s\n    %s\n", r.Source, r.Kind, trunc(snippet, 160), tagStr, r.URL)
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

func cmdSummarize(s *store.Store, args []string) {
	fs := flag.NewFlagSet("summarize", flag.ExitOnError)
	limit := fs.Int("limit", 100, "max items to summarize this run")
	fs.Parse(args)
	c := summarize.FromEnv()
	if c == nil {
		fatal(fmt.Errorf("set GLANE_SUMMARY_URL to generate summaries"))
	}
	items, err := s.PendingSummary(*limit)
	if err != nil {
		fatal(err)
	}
	done, failed := 0, 0
	for _, it := range items {
		res, err := c.Summarize(context.Background(), it.ArticleTitle, it.ArticleText)
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "glane: summarize item %d: %v\n", it.ID, err)
			continue
		}
		if err := s.SaveSummary(it.ID, res.Summary, res.Tags); err != nil {
			fatal(err)
		}
		done++
	}
	fmt.Printf("summarized %d items (%d failed)\n", done, failed)
}

func cmdTags(s *store.Store) {
	tags, err := s.TagCounts()
	if err != nil {
		fatal(err)
	}
	for _, tc := range tags {
		fmt.Printf("%-24s %d\n", tc.Tag, tc.Count)
	}
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
