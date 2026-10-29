package updater

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func notify(ctx context.Context, tg telegramConfig, message string, logger *log.Logger) {
	if !tg.Enabled {
		return
	}
	token, chat := tg.BotToken, tg.ChatID
	if token == "" || chat == "" {
		logger.Print("WARNING: Telegram credentials missing")
		return
	}
	body := url.Values{"chat_id": {chat}, "text": {message}, "disable_web_page_preview": {"true"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+token+"/sendMessage", strings.NewReader(body.Encode()))
	if err != nil {
		logger.Print("WARNING: invalid Telegram configuration")
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		logger.Print("WARNING: Telegram request failed")
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		logger.Printf("WARNING: Telegram HTTP %d", resp.StatusCode)
		return
	}
	var response struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&response); err != nil || !response.OK {
		logger.Print("WARNING: Telegram rejected report")
	}
}
