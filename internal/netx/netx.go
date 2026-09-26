// Package netx is HTTP with retries and resumable downloads.
package netx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"
)

// UserAgent identifies us to mirrors and registries.
var UserAgent = "andronix"

// Client has sane timeouts for phone networks.
var Client = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   20 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       60 * time.Second,
	},
}

// StatusError is an HTTP failure with its code (429 = rate limited).
type StatusError struct {
	Code int
	URL  string
}

func (e *StatusError) Error() string { return fmt.Sprintf("HTTP %d from %s", e.Code, e.URL) }

// Get fetches a small document.
func Get(ctx context.Context, url string, headers map[string]string) ([]byte, http.Header, error) {
	var lastErr error
	for try := 0; try < 3; try++ {
		req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
		req.Header.Set("User-Agent", UserAgent)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := Client.Do(req)
		if err != nil {
			lastErr = err
			if !sleep(ctx, try) {
				return nil, nil, ctx.Err()
			}
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			lastErr = &StatusError{resp.StatusCode, url}
			if resp.StatusCode == 429 || resp.StatusCode < 500 {
				return body, resp.Header, lastErr
			}
			if !sleep(ctx, try) {
				return nil, nil, ctx.Err()
			}
			continue
		}
		return body, resp.Header, err
	}
	return nil, nil, lastErr
}

// Head returns the status and size of url (following redirects).
func Head(ctx context.Context, url string) (int, int64, error) {
	req, _ := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	req.Header.Set("User-Agent", UserAgent)
	resp, err := Client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, resp.ContentLength, nil
}

// Download fetches url into dst, resuming a partial file, with up to 5
// attempts. progress gets (bytes so far, total). Returns the sha256 of
// the finished file.
func Download(ctx context.Context, url, dst string, headers map[string]string, total int64, progress func(cur, total int64)) (string, error) {
	var lastErr error
	for try := 0; try < 5; try++ {
		err := downloadOnce(ctx, url, dst, headers, total, progress)
		if err == nil {
			return FileSHA256(dst)
		}
		lastErr = err
		if se, ok := err.(*StatusError); ok && se.Code != 416 && se.Code < 500 {
			return "", err
		}
		if !sleep(ctx, try) {
			return "", ctx.Err()
		}
	}
	return "", lastErr
}

func downloadOnce(ctx context.Context, url, dst string, headers map[string]string, total int64, progress func(cur, total int64)) error {
	var have int64
	if st, err := os.Stat(dst); err == nil {
		have = st.Size()
	}
	if total > 0 && have == total {
		return nil
	}
	if total > 0 && have > total {
		os.Remove(dst)
		have = 0
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", UserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if have > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(have, 10)+"-")
	}
	resp, err := Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY
	switch resp.StatusCode {
	case 206:
		flags |= os.O_APPEND
	case 200:
		flags |= os.O_TRUNC
		have = 0
	case 416:
		return nil // already complete
	default:
		return &StatusError{resp.StatusCode, url}
	}
	if total <= 0 && resp.ContentLength > 0 {
		total = have + resp.ContentLength
	}
	f, err := os.OpenFile(dst, flags, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 256<<10)
	cur := have
	for {
		// A stalled connection on mobile data: give up after 60 s of
		// silence and let the retry resume.
		n, rerr := readWithTimeout(resp.Body, buf, 60*time.Second)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			cur += int64(n)
			if progress != nil {
				progress(cur, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if total > 0 && cur < total {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func readWithTimeout(r io.Reader, buf []byte, d time.Duration) (int, error) {
	type res struct {
		n   int
		err error
	}
	ch := make(chan res, 1)
	go func() { n, err := r.Read(buf); ch <- res{n, err} }()
	select {
	case x := <-ch:
		return x.n, x.err
	case <-time.After(d):
		return 0, fmt.Errorf("download stalled for %s", d)
	}
}

// FileSHA256 hashes a file.
func FileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sleep(ctx context.Context, try int) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(time.Duration(2*(try+1)) * time.Second):
		return true
	}
}
