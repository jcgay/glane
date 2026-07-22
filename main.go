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

var version = "dev" // set by goreleaser via -ldflags

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
		fmt.Fprintln(os.Stderr, "usage: glane <import|sync|search|enrich|summarize|update|tags|serve|version> ...")
		os.Exit(2)
	}
	if a := os.Args[1]; a == "version" || a == "--version" || a == "-v" {
		fmt.Println(version)
		return
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
	case "update":
		cmdUpdate(s)
	case "tags":
		cmdTags(s)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func syncClient() *http.Client { return &http.Client{Timeout: 30 * time.Second} }

func stderrProgress(msg string) { fmt.Fprintln(os.Stderr, msg) }

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
		n, err := github.Sync(s, token, syncClient(), stderrProgress)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("synced %d new stars\n", n)
	case "mastodon":
		base, token := os.Getenv("MASTODON_INSTANCE_URL"), os.Getenv("MASTODON_ACCESS_TOKEN")
		if base == "" || token == "" {
			fatal(fmt.Errorf("set MASTODON_INSTANCE_URL and MASTODON_ACCESS_TOKEN to sync Mastodon"))
		}
		n, err := mastodon.Sync(s, base, token, syncClient(), stderrProgress)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("synced %d new mastodon items\n", n)
	case "bluesky":
		handle, pw := os.Getenv("BLUESKY_HANDLE"), os.Getenv("BLUESKY_APP_PASSWORD")
		if handle == "" || pw == "" {
			fatal(fmt.Errorf("set BLUESKY_HANDLE and BLUESKY_APP_PASSWORD to sync Bluesky"))
		}
		n, err := bluesky.Sync(s, handle, pw, syncClient(), stderrProgress)
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
	if syncAll(s) {
		os.Exit(1)
	}
}

// syncAll runs every configured connector (skipping the rest), prints the
// summary line, and returns whether any configured connector failed. It does
// not exit — callers decide (cmdSyncAll exits; cmdUpdate keeps going).
func syncAll(s *store.Store) bool {
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
		n, err := github.Sync(s, tok, hc, stderrProgress)
		record("github", n, err)
	} else {
		skipped = append(skipped, "github")
	}
	if base, tok := os.Getenv("MASTODON_INSTANCE_URL"), os.Getenv("MASTODON_ACCESS_TOKEN"); base != "" && tok != "" {
		n, err := mastodon.Sync(s, base, tok, hc, stderrProgress)
		record("mastodon", n, err)
	} else {
		skipped = append(skipped, "mastodon")
	}
	if h, pw := os.Getenv("BLUESKY_HANDLE"), os.Getenv("BLUESKY_APP_PASSWORD"); h != "" && pw != "" {
		n, err := bluesky.Sync(s, h, pw, hc, stderrProgress)
		record("bluesky", n, err)
	} else {
		skipped = append(skipped, "bluesky")
	}

	fmt.Printf("synced %d new items [%s]", total, strings.Join(ran, " "))
	if len(skipped) > 0 {
		fmt.Printf(" (skipped, not configured: %s)", strings.Join(skipped, ", "))
	}
	fmt.Println()
	return failed
}

const drainLimit = 100000 // effectively "all pending" at personal scale, single pass

// enrichAll enriches the whole pending backlog in one pass. Injectable clients
// keep it testable. Returns counts and whether enrich.Run hard-errored.
func enrichAll(s *store.Store, hc *http.Client, emb *embed.Client) (int, int, bool) {
	done, failed, err := enrich.Run(s, hc, emb, drainLimit, stderrProgress)
	fmt.Printf("enriched %d, failed %d\n", done, failed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "glane: enrich error: %v\n", err)
		return done, failed, true
	}
	return done, failed, false
}

// summarizeAll summarizes the whole pending backlog in one pass (no re-loop, so a
// permanently-failing item can't spin). Failed items stay pending for next run.
func summarizeAll(s *store.Store, c *summarize.Client) (int, int) {
	items, err := s.PendingSummary(drainLimit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "glane: summarize error: %v\n", err)
		return 0, 1 // treat as a failure so update signals it
	}
	known := topTags(s, 50)
	done, failed := 0, 0
	for i, it := range items {
		stderrProgress(fmt.Sprintf("summarize [%d/%d]…", i+1, len(items)))
		res, serr := c.Summarize(context.Background(), it.ArticleTitle, it.ArticleText, known)
		if serr != nil {
			failed++
			fmt.Fprintf(os.Stderr, "glane: summarize item %d: %v\n", it.ID, serr)
			continue
		}
		if serr := s.SaveSummary(it.ID, res.Summary, res.Tags); serr != nil {
			failed++
			fmt.Fprintf(os.Stderr, "glane: summarize save item %d: %v\n", it.ID, serr)
			continue
		}
		done++
	}
	fmt.Printf("summarized %d items (%d failed)\n", done, failed)
	return done, failed
}

// cmdUpdate runs the full pipeline for a scheduled/one-shot refresh: sync all →
// enrich → summarize. Each phase runs regardless of earlier failures; update
// exits non-zero if any phase hard-failed (for scheduler alerting).
func cmdUpdate(s *store.Store) {
	failed := false
	if syncAll(s) {
		failed = true
	}
	if _, _, errored := enrichAll(s, enrich.DefaultClient(), embed.FromEnv()); errored {
		failed = true
	}
	if c := summarize.FromEnv(); c != nil {
		// A total summarize wipeout (some attempted, none succeeded) signals a
		// down endpoint; a few per-item failures don't trip the exit code.
		done, sfailed := summarizeAll(s, c)
		if done == 0 && sfailed > 0 {
			failed = true
		}
	} else {
		fmt.Println("summarize skipped (GLANE_SUMMARY_URL not set)")
	}
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
	fmt.Print(renderResults(res))
}

// renderResults formats search results as a scannable list: one numbered entry
// per result, a flattened single-line snippet, then an indented line with the
// URL and any tags. A blank line separates entries. The [source/kind] label is
// padded to a common width so all snippets start at the same column.
func renderResults(res []store.Result) string {
	labelWidth := 0
	for _, r := range res {
		if w := len(r.Source) + len(r.Kind) + 3; w > labelWidth { // "[/]" = 3
			labelWidth = w
		}
	}
	var b strings.Builder
	for i, r := range res {
		snippet := r.Text
		if r.ArticleSummary != "" {
			snippet = r.ArticleSummary
		}
		snippet = strings.Join(strings.Fields(snippet), " ") // collapse newlines/runs of whitespace
		label := fmt.Sprintf("[%s/%s]", r.Source, r.Kind)
		fmt.Fprintf(&b, "%3d. %-*s %s\n", i+1, labelWidth, label, trunc(snippet, 160))
		fmt.Fprintf(&b, "     %s", r.URL)
		if len(r.Tags) > 0 {
			fmt.Fprintf(&b, "  #%s", strings.Join(r.Tags, " #"))
		}
		b.WriteByte('\n')
		if ex := r.Excerpt(); ex != "" {
			ex = strings.Join(strings.Fields(ex), " ") // collapse newlines/whitespace
			fmt.Fprintf(&b, "     %s\n", termHighlight(ex))
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "(%d results)\n", len(res))
	return b.String()
}

// termHighlight turns snippet match sentinels into ANSI bold when stdout is a
// terminal, and strips them otherwise so piped/redirected output stays clean.
func termHighlight(s string) string {
	if fi, err := os.Stdout.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return strings.NewReplacer(store.MarkStart, "\033[1m", store.MarkEnd, "\033[0m").Replace(s)
	}
	return strings.NewReplacer(store.MarkStart, "", store.MarkEnd, "").Replace(s)
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
	done, failed, err := enrich.Run(s, enrich.DefaultClient(), embed.FromEnv(), *limit, stderrProgress)
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
	known := topTags(s, 50)
	done, failed := 0, 0
	for i, it := range items {
		stderrProgress(fmt.Sprintf("summarize [%d/%d]…", i+1, len(items)))
		res, err := c.Summarize(context.Background(), it.ArticleTitle, it.ArticleText, known)
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

// topTags returns the n most-used existing tags to steer the LLM toward reusing
// the established taxonomy instead of inventing near-duplicates. Best-effort: a
// query error just means no hint this run.
func topTags(s *store.Store, n int) []string {
	counts, err := s.TagCounts()
	if err != nil {
		return nil
	}
	out := []string{}
	for _, tc := range counts {
		if len(out) >= n {
			break
		}
		out = append(out, tc.Tag)
	}
	return out
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
