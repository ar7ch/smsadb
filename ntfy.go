package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func runNtfy(ctx context.Context, dev Device, args []string) error {
	fset := flag.NewFlagSet("ntfy", flag.ExitOnError)
	url := fset.String("push-url", "", "ntfy topic URL, e.g. https://ntfy.sh/your-secret-topic (required)")
	interval := fset.Duration("interval", 30*time.Second, "poll interval")
	state := fset.String("state", "", "file with the last pushed message id (default: per phone, in the user cache dir)")
	fset.Parse(args)
	if *url == "" {
		return errors.New("ntfy: -push-url is required")
	}
	if *interval <= 0 {
		return errors.New("ntfy: -interval must be positive")
	}
	u, err := neturl.Parse(*url)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.Trim(u.Path, "/") == "" {
		return errors.New("ntfy: -push-url must be an http(s) URL with a topic, e.g. https://ntfy.sh/your-topic")
	}
	token := os.Getenv("NTFY_TOKEN")

	// Message ids are per phone, so pin the phone and keep one state file each.
	if dev.Serial == "" {
		out, err := exec.CommandContext(ctx, "adb", "get-serialno").Output()
		if err != nil {
			return fmt.Errorf("adb get-serialno: %w (pass -s when several phones are attached)", err)
		}
		dev.Serial = strings.TrimSpace(string(out))
		if dev.Serial == "" || dev.Serial == "unknown" {
			return errors.New("no phone attached")
		}
	}
	if *state == "" {
		*state = defaultStatePath(dev.Serial)
	}

	last, err := loadLastID(ctx, dev, *state)
	if err != nil {
		return err
	}
	// the topic name works as a password on public servers, keep it out of logs
	log.Printf("pushing inbox messages after id %d to %s every %s", last, u.Host, *interval)

	for {
		msgs, err := dev.Messages(ctx, Query{Box: "inbox", AfterID: last})
		if err != nil && ctx.Err() == nil {
			log.Print(err) // phone unplugged or locked, retry next tick
		}
		// newest first, push oldest first
		for i := len(msgs) - 1; i >= 0; i-- {
			if err := push(ctx, *url, token, msgs[i]); err != nil {
				log.Print(err)
				var re *rejectedError
				if !errors.As(err, &re) {
					break
				}
				// retrying a message the server refuses would block every later one
				log.Printf("skipping message %d", msgs[i].ID)
			}
			last = msgs[i].ID
			if err := os.WriteFile(*state, []byte(strconv.FormatInt(last, 10)), 0o600); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(*interval):
		}
	}
}

func defaultStatePath(serial string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "smsadb", "ntfy-last-id-"+serial)
}

// loadLastID reads the state file. Without one it starts from the newest
// inbox message, so the first run does not flood the topic with history.
func loadLastID(ctx context.Context, dev Device, path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		return strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	msgs, err := dev.Messages(ctx, Query{Box: "inbox", Limit: 1})
	if err != nil {
		return 0, err
	}
	var last int64
	if len(msgs) > 0 {
		last = msgs[0].ID
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	return last, os.WriteFile(path, []byte(strconv.FormatInt(last, 10)), 0o600)
}

func push(ctx context.Context, url, token string, m Message) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(m.Body))
	if err != nil {
		return err
	}
	// ntfy decodes RFC 2047 headers, sender names are often non-ASCII
	req.Header.Set("Title", mime.QEncoding.Encode("utf-8", m.Address))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		// url.Error repeats the topic URL
		var ue *neturl.Error
		if errors.As(err, &ue) {
			return fmt.Errorf("ntfy: %s %s: %w", ue.Op, req.URL.Host, ue.Err)
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	err = fmt.Errorf("ntfy: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		return err
	}
	if resp.StatusCode/100 == 4 {
		return &rejectedError{err}
	}
	return err
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// rejectedError is a 4xx caused by the message itself, retrying won't help.
type rejectedError struct{ error }

func (e *rejectedError) Unwrap() error { return e.error }
