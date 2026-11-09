// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/dkorunic/astro-recommender/internal/fetch"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/slack-go/slack"
)

// The webhook's body formats.
const (
	FormatText = "text"
	FormatJSON = "json"
)

const (
	webhookTimeout = 30 * time.Second
	gatewayTimeout = 15 * time.Second
	maxRetryWait   = time.Minute // a daily alert can wait longer than a lookup
	embedGreen     = 0x2ecc71
)

// ErrWebhookURL is a webhook address that is not http or https.
var ErrWebhookURL = errors.New("the webhook needs an http or https URL")

var (
	errWebhookFormat  = errors.New("-webhook-format must be text or json")
	errWebhookHeader  = errors.New(`a webhook header must be "Name: value"`)
	errGatewayTimeout = errors.New("no answer from the gateway")
	errSlackNotPosted = errors.New("the answer names no posted message")
)

// concurrently sends to every recipient at once, so slow ones do not use up
// the others' deadline, and joins the errors in the recipients' order.
func concurrently(recipients []string, send func(string) error) error {
	errs := make([]error, len(recipients))
	var wg sync.WaitGroup
	for i, recipient := range recipients {
		wg.Go(func() { errs[i] = send(recipient) })
	}
	wg.Wait()

	return errors.Join(errs...)
}

// retried runs send and, when wait says the failure may pass, once more: the
// tool runs once a night, so one 502 would lose the alert. A message a lost
// answer had delivered goes out twice, the better of the two failures.
func retried(ctx context.Context, wait func(error) (time.Duration, bool), send func() error) error {
	err := send()
	if err == nil || ctx.Err() != nil {
		return err
	}
	pause, ok := wait(err)
	if !ok {
		return err
	}
	select {
	case <-ctx.Done():
		return err
	case <-time.After(pause):
	}

	return send()
}

// telegramWait retries a flood limit as long as it asks and anything else
// but Telegram's refusals (a network error, a 5xx).
func telegramWait(err error) (time.Duration, bool) {
	if e, ok := errors.AsType[*bot.TooManyRequestsError](err); ok {
		return fetch.RetryWait(strconv.Itoa(e.RetryAfter), maxRetryWait)
	}
	if _, ok := errors.AsType[*bot.MigrateError](err); ok {
		return 0, false
	}
	for _, refused := range []error{bot.ErrorBadRequest, bot.ErrorForbidden, bot.ErrorUnauthorized, bot.ErrorNotFound, bot.ErrorConflict} {
		if errors.Is(err, refused) {
			return 0, false
		}
	}

	return fetch.RetryDelay, true
}

// capped is the pause for a rate limit that asks for after: none past
// maxRetryWait, or negative, as a huge Retry-After overflows in the libraries.
func capped(after time.Duration) (time.Duration, bool) {
	if after < 0 {
		return 0, false
	}

	return fetch.CapWait(after, maxRetryWait)
}

// discordWait retries a rate limit as long as it asks, a network error and a
// 5xx.
func discordWait(err error) (time.Duration, bool) {
	if e, ok := errors.AsType[*discordgo.RateLimitError](err); ok {
		return capped(e.RetryAfter)
	}
	if e, ok := errors.AsType[*discordgo.RESTError](err); ok && e.Response.StatusCode < http.StatusInternalServerError {
		return 0, false
	}

	return fetch.RetryDelay, true
}

// SendTelegram delivers text to every chat, carrying on past a failing one.
// The token is cut from errors, since an http error quotes the URL holding it.
func SendTelegram(ctx context.Context, token string, chats []string, text string, opts ...bot.Option) error {
	redact := func(err error) error {
		return errors.New(strings.ReplaceAll(err.Error(), token, "<token>")) //nolint:err113 // a copy of err's text
	}

	// No getMe: its fixed 5 s would lose the night's alert to a slow answer,
	// and a bad token fails the send just the same.
	b, err := bot.New(token, append(opts, bot.WithSkipGetMe())...)
	if err != nil {
		return fmt.Errorf("telegram: %w", redact(err))
	}
	noPreview := true
	send := func(chat string) error {
		return retried(ctx, telegramWait, func() error {
			_, err := b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: chat, Text: text, ParseMode: models.ParseModeHTML,
				LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: &noPreview},
			})

			return err
		})
	}

	return concurrently(chats, func(chat string) error {
		err := send(chat)
		if e, ok := errors.AsType[*bot.MigrateError](err); ok {
			// A group upgraded to a supergroup has a new ID: every night would
			// fail until the setting changes, so deliver and say so.
			moved := strconv.Itoa(e.MigrateToChatID)
			fmt.Fprintf(os.Stderr, "astro-notify: telegram chat %s is now %s: update -chat\n", chat, moved)
			err = send(moved)
		}
		if err != nil {
			return fmt.Errorf("telegram chat %s: %w", chat, redact(err))
		}

		return nil
	})
}

// openGateway connects and identifies once: Discord has asked a bot to before
// its first message, and a connection a night costs nothing.
var openGateway = func(s *discordgo.Session) error {
	if err := s.Open(); err != nil {
		return err
	}

	return s.Close()
}

// identify runs openGateway within gatewayTimeout, since discordgo's Open
// takes no context and can wait on a stalled server for good.
func identify(ctx context.Context, s *discordgo.Session) error {
	done := make(chan error, 1)
	go func() { done <- openGateway(s) }()
	select {
	case err := <-done:
		return err
	case <-time.After(gatewayTimeout):
		return errGatewayTimeout
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendDiscord sends text as an embed, whose description takes twice a plain
// message's 2000 characters, to every user's direct messages. A failed
// Gateway connection is reported only with a failed send: the messages
// themselves go over REST and may well arrive without it.
func SendDiscord(ctx context.Context, token string, users []string, text string, opts ...func(*discordgo.Session)) error {
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return fmt.Errorf("discord: %w", err)
	}
	// discordgo's own wait is a time.Sleep without limit, which neither the
	// deadline nor SIGTERM can stop; retried waits instead.
	s.ShouldRetryOnRateLimit = false
	for _, opt := range opts {
		opt(s)
	}
	gateway := identify(ctx, s)
	err = concurrently(users, func(user string) error {
		// One each: discordgo writes defaults into the embed it is given.
		embed := &discordgo.MessageEmbed{Description: text, Color: embedGreen}
		err := retried(ctx, discordWait, func() error {
			ch, err := s.UserChannelCreate(user, discordgo.WithContext(ctx))
			if err != nil {
				return err
			}
			_, err = s.ChannelMessageSendEmbed(ch.ID, embed, discordgo.WithContext(ctx))

			return err
		})
		if err != nil {
			return fmt.Errorf("discord user %s: %w", user, err)
		}

		return nil
	})
	if err != nil && gateway != nil {
		return errors.Join(err, fmt.Errorf("discord gateway: %w", gateway))
	}

	return err
}

// slackTransient is the errors Slack answers with a 200 that may pass.
var slackTransient = []string{"internal_error", "fatal_error", "service_unavailable", "request_timeout", "ratelimited", "rate_limited"}

// slackWait retries a rate limit as long as it asks, a network error, a 429,
// a 5xx and slackTransient, not Slack's refusals (a bad token or channel).
func slackWait(err error) (time.Duration, bool) {
	if e, ok := errors.AsType[*slack.RateLimitedError](err); ok {
		return capped(e.RetryAfter)
	}
	if e, ok := errors.AsType[slack.SlackErrorResponse](err); ok && !slices.Contains(slackTransient, e.Err) {
		return 0, false
	}
	if e, ok := errors.AsType[slack.StatusCodeError](err); ok && !e.Retryable() {
		return 0, false
	}

	return fetch.RetryDelay, true
}

// SendSlack posts text to every channel, or user by ID, the bot can reach.
func SendSlack(ctx context.Context, token string, chats []string, text string, opts ...slack.Option) error {
	client := slack.New(token, append([]slack.Option{slack.OptionHTTPClient(postClient)}, opts...)...)

	return concurrently(chats, func(chat string) error {
		err := retried(ctx, slackWait, func() error {
			_, posted, err := client.PostMessageContext(ctx, chat,
				slack.MsgOptionText(text, false), // Style.esc has escaped it
				slack.MsgOptionDisableLinkUnfurl(), slack.MsgOptionDisableMediaUnfurl())
			if err == nil && posted == "" { // a proxy's bare 200: the library sees no error
				return errSlackNotPosted
			}

			return err
		})
		if err != nil {
			return fmt.Errorf("slack chat %s: %w", chat, err)
		}

		return nil
	})
}

// postClient reports a redirect instead of following it: a 301 or 302 would
// turn the POST into a GET and lose the message.
var postClient = &http.Client{
	Timeout:       webhookTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// Webhook is a validated address to POST the report to.
type Webhook struct {
	header http.Header
	url    string
	format string
}

// headerName is RFC 9110's token, what net/http would otherwise refuse only
// on the first night worth a message.
var headerName = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

func badHeaderValue(value string) bool {
	return strings.ContainsFunc(value, func(c rune) bool { return (c < ' ' && c != '\t') || c == 0x7f })
}

// CheckFormat rejects a -webhook-format that is neither body format.
func CheckFormat(format string) error {
	if format != FormatText && format != FormatJSON {
		return errWebhookFormat
	}

	return nil
}

// NewWebhook checks the settings; lines are "Name: value" headers.
func NewWebhook(rawURL, format string, lines []string) (*Webhook, error) {
	// A stray space would pass url.Parse and post to another ntfy topic.
	rawURL = strings.TrimSpace(rawURL)
	if u, err := url.Parse(rawURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, ErrWebhookURL // not err, which quotes a URL that may hold a key
	}
	if err := CheckFormat(format); err != nil {
		return nil, err
	}
	w := &Webhook{url: rawURL, format: format, header: http.Header{}}
	for _, line := range lines {
		name, value, ok := strings.Cut(line, ":")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || !headerName.MatchString(name) || badHeaderValue(value) {
			return nil, errWebhookHeader
		}
		w.header.Add(name, value)
	}

	return w, nil
}

// statusError is a webhook's answer other than 2xx.
type statusError struct {
	status, retryAfter string
	code               int
}

func (e *statusError) Error() string { return "the webhook answered " + e.status }

// webhookWait retries a network error, a 429 or a 5xx, as long as it asks.
func webhookWait(err error) (time.Duration, bool) {
	e, ok := errors.AsType[*statusError](err)
	if !ok {
		return fetch.RetryDelay, true
	}
	if e.code != http.StatusTooManyRequests && e.code < http.StatusInternalServerError {
		return 0, false
	}

	return fetch.RetryWait(e.retryAfter, maxRetryWait)
}

// Send posts text. Errors leave the URL out, since it may hold a key.
func (w *Webhook) Send(ctx context.Context, text string) error {
	body, contentType, err := w.body(text)
	if err != nil {
		return fmt.Errorf("webhook: %w", err)
	}

	return retried(ctx, webhookWait, func() error { return w.post(ctx, body, contentType) })
}

func (w *Webhook) post(ctx context.Context, body []byte, contentType string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return ErrWebhookURL
	}
	req.Header = w.header.Clone()
	req.Host = req.Header.Get("Host") // net/http reads only this field; "" is the URL's
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := postClient.Do(req)
	if err != nil {
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}

		return fmt.Errorf("webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &statusError{status: resp.Status, retryAfter: resp.Header.Get("Retry-After"), code: resp.StatusCode}
	}

	return nil
}

// body is the request body and its type. The JSON keys are the ones Gotify
// and Home Assistant's notify services read; the first line is the title.
func (w *Webhook) body(text string) ([]byte, string, error) {
	if w.format == FormatText {
		return []byte(text), "text/plain; charset=utf-8", nil
	}
	title, msg, _ := strings.Cut(text, "\n")
	body, err := json.Marshal(struct {
		Title   string `json:"title"`
		Message string `json:"message"`
	}{title, msg})

	return body, "application/json", err
}
