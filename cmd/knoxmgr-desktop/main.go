//go:build windows

// Command knoxmgr-desktop is the portable Windows build: it runs the knoxmgr server on a loopback
// port and shows it in a WebView2 window. All state lives next to the executable.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"

	webview2 "github.com/jchv/go-webview2"
	"github.com/rs/zerolog"
	"golang.org/x/sys/windows"

	"github.com/DonovanDiamond/knoxmgr/src/server"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// errStopped means the server died while the window was open; the user has already been told.
var errStopped = errors.New("server stopped")

func main() {
	// WebView2 needs creation and the message loop on the same OS thread.
	runtime.LockOSThread()
	if err := run(); err != nil {
		if err == errStopped {
			os.Exit(1)
		}
		msgBox("knoxmgr failed:\n\n"+err.Error(), windows.MB_ICONERROR)
		os.Exit(1)
	}
}

func run() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)

	lock, err := acquireLock(filepath.Join(dir, "knoxmgr-desktop.lock"))
	if err != nil {
		if err == windows.ERROR_SHARING_VIOLATION {
			msgBox("knoxmgr is already running from this folder", windows.MB_ICONINFORMATION)
			return nil
		}
		return fmt.Errorf("lock file: %w", err)
	}
	defer windows.CloseHandle(lock)

	logFile, err := os.OpenFile(filepath.Join(dir, "knoxmgr-desktop.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	log := zerolog.New(logFile).With().Timestamp().Logger()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx, log, server.Options{
			DataDir:    dir,
			ConfigPath: filepath.Join(dir, "config.yaml"),
			Listener:   ln,
			Version:    version,
			OnReady:    func(setupCode string) { ready <- setupCode },
		})
	}()

	var url string
	select {
	case code := <-ready:
		url = "http://" + ln.Addr().String() + "/"
		if code != "" {
			// Fragment only: it never reaches the server or any log.
			url += "setup#code=" + code
		}
	case err := <-done:
		if err == nil {
			err = fmt.Errorf("server stopped before it was ready")
		}
		log.Error().Err(err).Msg("server run")
		return err
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath: filepath.Join(dir, "webview-data"),
		WindowOptions: webview2.WindowOptions{
			Title:  "knoxmgr",
			Width:  1280,
			Height: 800,
			Center: true,
		},
	})
	if w == nil {
		log.Warn().Msg("WebView2 unavailable, falling back to the default browser")
		if err := openBrowser(url); err != nil {
			log.Error().Err(err).Msg("open browser")
		}
		msgBox("The Microsoft WebView2 runtime is missing, so knoxmgr was opened in your default browser instead.\n\n"+
			"knoxmgr keeps running until you click OK here; clicking OK stops knoxmgr.", windows.MB_ICONINFORMATION)
	} else {
		stopped := make(chan error, 1)
		go func() {
			err := <-done
			stopped <- err
			if ctx.Err() != nil {
				return // normal shutdown after the window closed
			}
			log.Error().Err(err).Msg("server run")
			// The message box and Terminate must run on the webview thread.
			w.Dispatch(func() {
				msgBox("knoxmgr stopped: see knoxmgr-desktop.log", windows.MB_ICONERROR)
				w.Terminate()
			})
		}()
		w.Navigate(url)
		w.Run()
		w.Destroy()
		done = stopped
	}

	cancel()
	if err := <-done; err != nil {
		if ctx.Err() == nil {
			return errStopped
		}
		log.Error().Err(err).Msg("server run")
		return err
	}
	return nil
}

// acquireLock opens path with share mode 0, so a second process fails with a sharing violation.
func acquireLock(path string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
		windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
}

func msgBox(text string, icon uint32) {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString("knoxmgr")
	windows.MessageBox(0, t, c, icon|windows.MB_OK)
}

func openBrowser(url string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	u, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, u, nil, nil, windows.SW_SHOWNORMAL)
}
