//go:build !android

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/crgimenes/glaze"
	"github.com/jcgay/glane/internal/store"
	"github.com/jcgay/glane/internal/web"
)

// AppKit (macOS) and GTK (Linux) only run on the process's main thread.
// Pinning the main goroutine to it here, before main runs, is what keeps
// `glane app` from dying with SIGILL; the other commands don't notice.
func init() { runtime.LockOSThread() }

// cmdApp serves the UI on a free loopback port and shows it in a native
// window; closing the window stops the server.
func cmdApp(args []string) {
	if len(args) > 0 {
		fatal(fmt.Errorf("usage: glane app"))
	}
	s, err := store.Open(dbPath())
	if err != nil {
		fatal(err)
	}
	defer s.Close()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal(err)
	}
	srv := &http.Server{Handler: web.Handler(s), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(l)
	url := "http://" + l.Addr().String() + "/"
	fmt.Printf("glane app serving %s on %s\n", dbPath(), url)

	_ = glaze.SetAppIcon(web.AppIcon()) // unsupported on some platforms: keep the default icon
	w, err := glaze.NewWithOptions(glaze.Options{
		HideUntilLoaded: true,
		OnNewWindow:     openExternal,
	})
	if err != nil {
		hint := "use glane serve"
		if runtime.GOOS == "linux" {
			hint = "on Debian/Ubuntu: apt install libwebkit2gtk-4.1-0, or use glane serve"
		}
		fatal(fmt.Errorf("webview unavailable: %w; %s", err, hint))
	}
	w.SetTitle("glane")
	w.SetSize(1200, 800, glaze.HintNone)
	w.Navigate(url)
	w.Run()
	w.Destroy()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// openExternal hands a link the page wants in a new window (target=_blank,
// window.open) to the default browser: the app window only shows glane.
// Only http(s): open and xdg-open would launch a file:// app just as well.
func openExternal(url string) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		fmt.Fprintf(os.Stderr, "glane: not opening %s: only http(s) links leave the app\n", url)
		return
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	if err := exec.Command(opener, url).Start(); err != nil {
		fmt.Fprintf(os.Stderr, "glane: open %s: %v\n", url, err)
	}
}
