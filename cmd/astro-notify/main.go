// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Command astro-notify runs astro-recommender with -json and, when the night
// has a long enough stretch without cloud and with good seeing, sends its
// forecast and targets to Telegram chats, Discord users and a webhook. It
// runs once: schedule it.
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/dkorunic/astro-recommender/internal/notify"
)

const (
	telegramTokenEnv = "TELEGRAM_BOT_TOKEN" //nolint:gosec // the variable's name
	telegramChatsEnv = "TELEGRAM_CHAT_IDS"
	discordTokenEnv  = "DISCORD_BOT_TOKEN" //nolint:gosec // the variable's name
	discordUsersEnv  = "DISCORD_USER_IDS"
	webhookURLEnv    = "WEBHOOK_URL"
	webhookHeaderEnv = "WEBHOOK_HEADERS" // one "Name: value" per line

	runTimeout     = 5 * time.Minute
	deliverTimeout = 5 * time.Minute
	deliverGrace   = 2 * time.Second
)

var (
	errNoDestination = errors.New("nowhere to send: set a Telegram, Discord or webhook destination, or use -dry-run")
	errTelegram      = errors.New("telegram needs both $" + telegramTokenEnv + " and -chat (or $" + telegramChatsEnv + ")")
	errDiscord       = errors.New("discord needs both $" + discordTokenEnv + " and -discord-user (or $" + discordUsersEnv + ")")
	errUnfinished    = errors.New("gave up waiting for destinations")
	errNoForecast    = errors.New("the report has no cloud or seeing forecast for the night (a failed fetch, -no-weather, or an astro-recommender too old to report conditions)")
)

// childEnv is the environment less the notification secrets, which
// astro-recommender has no use for. The rest stays: it reads its own API key.
func childEnv(env []string) []string {
	return slices.DeleteFunc(env, func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")

		return slices.Contains([]string{telegramTokenEnv, telegramChatsEnv, discordTokenEnv, discordUsersEnv, webhookURLEnv, webhookHeaderEnv}, name)
	})
}

// fetchReport runs astro-recommender; its warnings go to our stderr.
func fetchReport(ctx context.Context, bin string, args []string) (*notify.Report, error) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, append(args, "-json")...) //nolint:gosec // the operator's own command line
	cmd.Env = childEnv(os.Environ())
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if ctxErr := ctx.Err(); ctxErr != nil && err != nil { // say why it was killed
		return nil, fmt.Errorf("%s: %w (%w)", bin, ctxErr, err)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", bin, err)
	}
	var r notify.Report
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", bin, err)
	}

	return &r, nil
}

// headerLines is the headers of $WEBHOOK_HEADERS, blank lines aside.
func headerLines(env string) []string {
	var lines []string
	for line := range strings.Lines(env) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}

	return lines
}

func splitList(s string) []string {
	var items []string
	for item := range strings.SplitSeq(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}

	return items
}

// options is the validated command line. The secrets come from the
// environment, since a command line shows in ps.
type options struct {
	hook          *notify.Webhook
	bin           string
	telegramToken string
	discordToken  string
	chats, users  []string
	args          []string
	limits        notify.Limits
	dryRun        bool
}

func usage() {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s [flags] -- <astro-recommender flags>

Bot tokens come from $%s and $%s. $%s and $%s (one header per line)
keep a key in the webhook's URL or headers out of ps.

`, os.Args[0], telegramTokenEnv, discordTokenEnv, webhookURLEnv, webhookHeaderEnv)
	flag.PrintDefaults()
}

func parseOptions() (*options, error) {
	o := options{telegramToken: strings.TrimSpace(os.Getenv(telegramTokenEnv)), discordToken: strings.TrimSpace(os.Getenv(discordTokenEnv))}

	headers := headerLines(os.Getenv(webhookHeaderEnv))
	flag.Func("webhook-header", `a webhook header, "Name: value"; repeat for more (adds to $`+webhookHeaderEnv+`)`, func(s string) error {
		headers = append(headers, s)

		return nil
	})
	flag.StringVar(&o.bin, "bin", "astro-recommender", "the astro-recommender binary")
	// The variables are read after Parse: as flag defaults, usage would print
	// them, a key in the webhook's URL included.
	chats := flag.String("chat", "", "comma-separated Telegram chat IDs or @channels (else $"+telegramChatsEnv+")")
	users := flag.String("discord-user", "", "comma-separated Discord user IDs to message (else $"+discordUsersEnv+")")
	hook := flag.String("webhook", "", "URL to POST the report to (else $"+webhookURLEnv+")")
	format := flag.String("webhook-format", notify.FormatText, `the webhook's body: text, or json {"title","message"}`)
	flag.BoolVar(&o.dryRun, "dry-run", false, "print the message in every style, whatever the forecast, and send nothing")
	flag.Float64Var(&o.limits.Cloud, "max-cloud", 10, "the most effective cloud cover in a clear minute, %")
	flag.Float64Var(&o.limits.Seeing, "max-seeing", 2, "the worst seeing in a clear minute, arcseconds")
	flag.DurationVar(&o.limits.Clear, "min-clear", 2*time.Hour, "the shortest unbroken clear stretch worth a message, in which a listed target must be observable")
	flag.Usage = usage
	flag.Parse()

	if err := o.limits.Validate(); err != nil {
		return nil, err
	}
	if err := notify.CheckFormat(*format); err != nil {
		return nil, err
	}
	o.chats, o.users, o.args = splitList(cmp.Or(*chats, os.Getenv(telegramChatsEnv))), splitList(cmp.Or(*users, os.Getenv(discordUsersEnv))), flag.Args()
	hookURL := cmp.Or(*hook, os.Getenv(webhookURLEnv))
	switch {
	case hookURL != "":
		var err error
		if o.hook, err = notify.NewWebhook(hookURL, *format, headers); err != nil {
			return nil, err
		}
	case len(headers) > 0:
		return nil, notify.ErrWebhookURL
	}
	if o.dryRun {
		return &o, nil
	}

	return &o, o.destinations()
}

// destinations rejects a half-set destination, which would otherwise pass
// for a night that sent nothing.
func (o *options) destinations() error {
	switch {
	case (o.telegramToken == "") != (len(o.chats) == 0):
		return errTelegram
	case (o.discordToken == "") != (len(o.users) == 0):
		return errDiscord
	case o.telegramToken == "" && o.discordToken == "" && o.hook == nil:
		return errNoDestination
	}

	return nil
}

// deliver sends to every destination at once under one deadline, so a
// stalled one holds up none of the others.
func (o *options) deliver(ctx context.Context, r *notify.Report, runs []notify.ClearRun) error {
	ctx, cancel := context.WithTimeout(ctx, deliverTimeout)
	defer cancel()

	var sends []func() error
	if o.telegramToken != "" {
		sends = append(sends, func() error {
			return notify.SendTelegram(ctx, o.telegramToken, o.chats, notify.Message(r, o.limits, runs, &notify.Telegram))
		})
	}
	if o.discordToken != "" {
		sends = append(sends, func() error {
			return notify.SendDiscord(ctx, o.discordToken, o.users, notify.Message(r, o.limits, runs, &notify.Discord))
		})
	}
	if o.hook != nil {
		sends = append(sends, func() error { return o.hook.Send(ctx, notify.Message(r, o.limits, runs, &notify.Plain)) })
	}

	return runAll(ctx, deliverGrace, sends)
}

// runAll runs the sends at once and waits for them until ctx ends and grace
// more, then gives up on the rest: a library's uninterruptible sleep must not
// hold the process, and the errors of those that finished, for ever.
func runAll(ctx context.Context, grace time.Duration, sends []func() error) error {
	results := make(chan error, len(sends))
	for _, send := range sends {
		go func() { results <- send() }()
	}

	var errs []error
	done := ctx.Done()
	var giveUp <-chan time.Time
	for pending := len(sends); pending > 0; {
		select {
		case err := <-results:
			errs = append(errs, err)
			pending--
		case <-done:
			done, giveUp = nil, time.After(grace)
		case <-giveUp:
			return errors.Join(append(errs, fmt.Errorf("%w: %d", errUnfinished, pending))...)
		}
	}

	return errors.Join(errs...)
}

func run(ctx context.Context) error {
	o, err := parseOptions()
	if err != nil {
		return err
	}
	r, err := fetchReport(ctx, o.bin, o.args)
	if err != nil {
		return err
	}
	if r.Message != "" { // no night tonight: nothing failed, nothing to show
		fmt.Fprintln(os.Stderr, "not sending:", r.Message)

		return nil
	}

	runs := r.ClearRuns(o.limits)
	if o.dryRun {
		for _, st := range []*notify.Style{&notify.Telegram, &notify.Discord, &notify.Plain} {
			fmt.Printf("--- %s\n%s\n", st.Name, notify.Message(r, o.limits, runs, st))
		}
	}
	if !r.ForecastKnown() {
		return errNoForecast
	}
	why := r.Verdict(o.limits, runs)
	if o.dryRun {
		fmt.Fprintf(os.Stderr, "would send: %t %s\n", why == "", why)

		return nil
	}
	if why != "" {
		fmt.Fprintln(os.Stderr, "not sending:", why)

		return nil
	}

	return o.deliver(ctx, r, runs)
}

func main() {
	// SIGTERM too, as systemd and docker stop with: the cancelled context
	// stops astro-recommender rather than leaving it running.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "astro-notify:", err)
		os.Exit(1)
	}
}
