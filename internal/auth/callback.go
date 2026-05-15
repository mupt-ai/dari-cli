package auth

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// callbackResult is the data we extract from the OAuth redirect.
type callbackResult struct {
	Code  string
	State string
	Error string
}

// runCallbackServer binds to 127.0.0.1 on an ephemeral port and blocks on a
// single GET /callback request. The returned redirect URL is what gets
// threaded into the Supabase authorize URL.
//
// Usage:
//
//	server, err := startCallbackServer()
//	// ... open browser with server.RedirectURL ...
//	result, err := server.Wait(ctx, 5*time.Minute)
//	server.Close()
type callbackServer struct {
	RedirectURL string

	listener net.Listener
	srv      *http.Server
	result   chan callbackResult
	once     sync.Once
}

func startCallbackServer() (*callbackServer, error) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("bind local callback listener: %w", err)
	}
	addr := lis.Addr().(*net.TCPAddr)
	cb := &callbackServer{
		RedirectURL: fmt.Sprintf("http://127.0.0.1:%d/callback", addr.Port),
		listener:    lis,
		result:      make(chan callbackResult, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		res := callbackResult{
			Code:  q.Get("code"),
			State: q.Get("state"),
			Error: cmp.Or(q.Get("error_description"), q.Get("error")),
		}
		body := "Dari CLI login complete. You can close this tab.\n"
		status := http.StatusOK
		if res.Error != "" {
			body = "Dari CLI login failed: " + res.Error + "\n"
			status = http.StatusBadRequest
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))

		cb.once.Do(func() { cb.result <- res })
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	cb.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = cb.srv.Serve(lis) }()
	return cb, nil
}

// Wait blocks until the callback fires or the context/timeout expires.
func (cb *callbackServer) Wait(ctx context.Context, timeout time.Duration) (callbackResult, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	select {
	case res := <-cb.result:
		if res.Code == "" && res.Error == "" {
			return res, errors.New("browser callback did not include a code")
		}
		return res, nil
	case <-deadline.C:
		return callbackResult{}, errors.New("timed out waiting for browser login to complete")
	case <-ctx.Done():
		return callbackResult{}, ctx.Err()
	}
}

// Close tears down the callback listener. Safe to call multiple times.
func (cb *callbackServer) Close() {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = cb.srv.Shutdown(shutdownCtx)
}

// WaitOrInput blocks until either the local callback fires or the user pastes
// the redirected callback URL. The paste path makes `dari auth login` work
// from remote VMs without requiring a separate command or flag.
func (cb *callbackServer) WaitOrInput(ctx context.Context, r io.Reader, timeout time.Duration) (callbackResult, error) {
	if r == nil {
		r = os.Stdin
	}
	fmt.Fprint(os.Stderr, "Paste callback URL: ")

	lineCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(r)
		if scanner.Scan() {
			lineCh <- scanner.Text()
			return
		}
		if err := scanner.Err(); err != nil {
			errCh <- err
			return
		}
		errCh <- io.EOF
	}()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case res := <-cb.result:
			if res.Code == "" && res.Error == "" {
				return res, errors.New("browser callback did not include a code")
			}
			return res, nil
		case raw := <-lineCh:
			return parseManualCallback(raw)
		case err := <-errCh:
			if err != nil && !errors.Is(err, io.EOF) {
				return callbackResult{}, fmt.Errorf("read callback URL: %w", err)
			}
			errCh = nil
		case <-deadline.C:
			return callbackResult{}, errors.New("timed out waiting for browser login to complete")
		case <-ctx.Done():
			return callbackResult{}, ctx.Err()
		}
	}
}

func parseManualCallback(raw string) (callbackResult, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return callbackResult{}, errors.New("empty callback URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return callbackResult{}, fmt.Errorf("parse callback URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return callbackResult{}, errors.New("paste the full localhost callback URL, not just the code")
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return callbackResult{}, errors.New("callback URL must use http or https")
	}

	q := u.Query()
	res := callbackResult{
		Code:  q.Get("code"),
		State: q.Get("state"),
		Error: cmp.Or(q.Get("error_description"), q.Get("error")),
	}
	if res.Code == "" && res.Error == "" {
		return res, errors.New("callback URL did not include a code")
	}
	if res.Code != "" && res.State == "" {
		return res, errors.New("callback URL did not include state")
	}
	return res, nil
}
