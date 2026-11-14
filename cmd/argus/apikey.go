package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"
)

// apikeyClient talks to a running Argus server as an admin user, same shape
// as restartClient (each cmd file owns its own small HTTP client rather than
// sharing one -- there's no other caller yet to justify the abstraction).
type apikeyClient struct {
	base  string
	http  *http.Client
	in    io.Reader
	out   io.Writer
	token string
}

type apiKeyView struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	KeyPrefix  string  `json:"key_prefix"`
	Label      string  `json:"label"`
	RateLimit  int     `json:"rate_limit"`
	LastUsedAt *string `json:"last_used_at"`
	RevokedAt  *string `json:"revoked_at"`
	CreatedAt  string  `json:"created_at"`
}

func (c *apikeyClient) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v1"+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error struct{ Message string } `json:"error"`
		}
		if json.Unmarshal(b, &e) == nil && e.Error.Message != "" {
			return fmt.Errorf("%s (HTTP %d)", e.Error.Message, resp.StatusCode)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

func (c *apikeyClient) login(ctx context.Context, email, password string) error {
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := c.do(ctx, http.MethodPost, "/auth/login", map[string]string{"email": email, "password": password}, &tok); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	c.token = tok.AccessToken
	return nil
}

func (c *apikeyClient) list(ctx context.Context) ([]apiKeyView, error) {
	var keys []apiKeyView
	if err := c.do(ctx, http.MethodGet, "/api-keys", nil, &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

func (c *apikeyClient) create(ctx context.Context, name, label string, rateLimit int) (apiKeyView, string, error) {
	in := map[string]any{"name": name}
	if label != "" {
		in["label"] = label
	}
	if rateLimit > 0 {
		in["rate_limit"] = rateLimit
	}
	var out struct {
		apiKeyView
		Key string `json:"key"`
	}
	if err := c.do(ctx, http.MethodPost, "/api-keys", in, &out); err != nil {
		return apiKeyView{}, "", err
	}
	return out.apiKeyView, out.Key, nil
}

func (c *apikeyClient) revoke(ctx context.Context, id string, yes bool) error {
	if !yes {
		fmt.Fprintf(c.out, "Revoke API key %s? [y/N] ", id)
		line, _ := bufio.NewReader(c.in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			return errors.New("aborted")
		}
	}
	return c.do(ctx, http.MethodDelete, "/api-keys/"+url.PathEscape(id), nil, nil)
}

func printKeyTable(out io.Writer, keys []apiKeyView) {
	if len(keys) == 0 {
		fmt.Fprintln(out, "no API keys")
		return
	}
	for _, k := range keys {
		status := "active"
		if k.RevokedAt != nil {
			status = "revoked"
		}
		lastUsed := "never used"
		if k.LastUsedAt != nil {
			lastUsed = "last used " + *k.LastUsedAt
		}
		fmt.Fprintf(out, "%s  %-24s %s…  %-8s rate=%d  %s  created %s\n",
			k.ID, k.Name, k.KeyPrefix, status, k.RateLimit, lastUsed, k.CreatedAt)
	}
}

func apikeyAdminClient(url string) (*apikeyClient, string, string) {
	email, pw := os.Getenv("ARGUS_EMAIL"), os.Getenv("ARGUS_PASSWORD")
	return &apikeyClient{base: strings.TrimRight(url, "/"), http: &http.Client{Timeout: 30 * time.Second},
		in: os.Stdin, out: os.Stdout}, email, pw
}

func runApikey(args []string, log *slog.Logger) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	sub, rest := args[0], args[1:]

	fs := flag.NewFlagSet("apikey "+sub, flag.ContinueOnError)
	srv := fs.String("url", envOr("ARGUS_URL", "http://localhost:8080"), "Argus server URL")

	switch sub {
	case "list":
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		c, email, pw := apikeyAdminClient(*srv)
		if email == "" || pw == "" {
			fmt.Fprintln(os.Stderr, "argus apikey: set ARGUS_EMAIL and ARGUS_PASSWORD (an admin account)")
			return 2
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if err := c.login(ctx, email, pw); err != nil {
			log.Error("apikey list", "err", err)
			return 1
		}
		keys, err := c.list(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "argus apikey list:", err)
			return 1
		}
		printKeyTable(c.out, keys)
		return 0

	case "create":
		label := fs.String("label", "", "optional label, e.g. a cluster or host name")
		rateLimit := fs.Int("rate-limit", 0, "optional lines/sec rate limit (0 = server default)")
		if err := fs.Parse(rest); err != nil || fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "usage: argus apikey create <name> [-label L] [-rate-limit N]")
			return 2
		}
		c, email, pw := apikeyAdminClient(*srv)
		if email == "" || pw == "" {
			fmt.Fprintln(os.Stderr, "argus apikey: set ARGUS_EMAIL and ARGUS_PASSWORD (an admin account)")
			return 2
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if err := c.login(ctx, email, pw); err != nil {
			log.Error("apikey create", "err", err)
			return 1
		}
		view, key, err := c.create(ctx, fs.Arg(0), *label, *rateLimit)
		if err != nil {
			fmt.Fprintln(os.Stderr, "argus apikey create:", err)
			return 1
		}
		fmt.Fprintf(c.out, "created %s (id %s)\n", view.Name, view.ID)
		fmt.Fprintln(c.out, "key (copy this now -- it will not be shown again):")
		fmt.Fprintln(c.out, key)
		return 0

	case "revoke":
		yes := fs.Bool("yes", false, "do not ask for confirmation")
		if err := fs.Parse(rest); err != nil || fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "usage: argus apikey revoke <id> [-yes]")
			return 2
		}
		c, email, pw := apikeyAdminClient(*srv)
		if email == "" || pw == "" {
			fmt.Fprintln(os.Stderr, "argus apikey: set ARGUS_EMAIL and ARGUS_PASSWORD (an admin account)")
			return 2
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if err := c.login(ctx, email, pw); err != nil {
			log.Error("apikey revoke", "err", err)
			return 1
		}
		if err := c.revoke(ctx, fs.Arg(0), *yes); err != nil {
			fmt.Fprintln(os.Stderr, "argus apikey revoke:", err)
			return 1
		}
		fmt.Fprintln(c.out, "revoked")
		return 0
	}

	fmt.Fprintf(os.Stderr, "unknown apikey subcommand %q\n\n%s", sub, usage)
	return 2
}
