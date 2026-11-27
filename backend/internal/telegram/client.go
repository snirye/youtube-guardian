package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type client struct {
	baseURL string
	token   string
	http    *http.Client
}

func newClient(token string, transport http.RoundTripper) client {
	return client{
		token:   token,
		baseURL: "https://api.telegram.org",
		http: &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
}

func (c client) call(ctx context.Context, method string, payload any, output any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/bot"+c.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build Telegram %s request", method)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		// Transport errors can include the bot token in the request URL.
		return fmt.Errorf("Telegram %s connection failed", method)
	}
	defer response.Body.Close()
	var result struct {
		OK        bool
		Result    json.RawMessage
		ErrorCode int `json:"error_code"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&result); err != nil {
		return fmt.Errorf("Telegram %s returned an invalid response (HTTP %d)", method, response.StatusCode)
	}
	if response.StatusCode != http.StatusOK || !result.OK {
		return fmt.Errorf("Telegram %s failed (HTTP %d, API %d)", method, response.StatusCode, result.ErrorCode)
	}
	if output != nil {
		if err := json.Unmarshal(result.Result, output); err != nil {
			return fmt.Errorf("Telegram %s returned invalid data", method)
		}
	}
	return nil
}
