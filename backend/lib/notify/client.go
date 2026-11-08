package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the Telegram Bot API endpoint.
const DefaultBaseURL = "https://api.telegram.org"

const (
	requestTimeout = 15 * time.Second
	maxReplyBytes  = 1 << 20
	// MaxMessageRunes is Telegram's message length limit.
	MaxMessageRunes = 4096
)

// APIError is a Telegram API failure. It never contains the bot token.
type APIError struct {
	Code        int
	Description string
	// RetryAfter is set on HTTP 429.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram api error %d: %s", e.Code, e.Description)
}

// Forbidden reports that the user blocked the bot or removed the chat.
func (e *APIError) Forbidden() bool { return e.Code == http.StatusForbidden }

// Client is a minimal Telegram Bot API client over plain net/http.
type Client struct {
	token string
	base  string
	hc    *http.Client
}

// NewClient builds a client. baseURL "" means the real Telegram API; tests
// point it at a fake server. Redirects are never followed, so the token cannot
// be forwarded to another host.
func NewClient(token, baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		token: token,
		base:  strings.TrimRight(baseURL, "/"),
		hc: &http.Client{
			Timeout:       requestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// scrub removes the token from an error text (net/http errors embed the URL).
func (c *Client) scrub(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(strings.ReplaceAll(err.Error(), c.token, "[REDACTED]"))
}

func (c *Client) call(ctx context.Context, method string, payload, out any, timeout time.Duration) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/bot"+c.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, c.scrub(err))
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, c.scrub(err))
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReplyBytes))
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, c.scrub(err))
	}
	var ar apiResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return &APIError{Code: resp.StatusCode, Description: "unreadable response"}
	}
	if !ar.OK {
		code := ar.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		e := &APIError{Code: code, Description: strings.ReplaceAll(ar.Description, c.token, "[REDACTED]")}
		if ar.Parameters.RetryAfter > 0 {
			e.RetryAfter = time.Duration(ar.Parameters.RetryAfter) * time.Second
		}
		return e
	}
	if out != nil {
		if err := json.Unmarshal(ar.Result, out); err != nil {
			return fmt.Errorf("telegram %s: bad result", method)
		}
	}
	return nil
}

// SendMessage sends plain text (no parse mode, so item names cannot inject markup).
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	r := []rune(text)
	if len(r) > MaxMessageRunes {
		text = string(r[:MaxMessageRunes-1]) + "…"
	}
	return c.call(ctx, "sendMessage", map[string]any{
		"chat_id": chatID, "text": text, "disable_web_page_preview": true,
	}, nil, 0)
}

// Update is the subset of a Telegram update the bot reads.
type Update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
	} `json:"message"`
}

// GetUpdates long-polls for new messages; timeout is the server-side wait.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	var ups []Update
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": int(timeout / time.Second), "allowed_updates": []string{"message"},
	}, &ups, timeout+requestTimeout)
	return ups, err
}

// BotUsername returns the bot's @username (getMe).
func (c *Client) BotUsername(ctx context.Context) (string, error) {
	var me struct {
		Username string `json:"username"`
	}
	if err := c.call(ctx, "getMe", map[string]any{}, &me, 0); err != nil {
		return "", err
	}
	return me.Username, nil
}
