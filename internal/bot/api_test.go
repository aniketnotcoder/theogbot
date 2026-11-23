package bot

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAPIFloodWaitUsesRetryAfter(t *testing.T) {
	var response apiResponse[json.RawMessage]
	if err := json.Unmarshal([]byte(`{
		"ok": false,
		"error_code": 429,
		"description": "Too Many Requests",
		"parameters": {"retry_after": 7}
	}`), &response); err != nil {
		t.Fatal(err)
	}
	if got, want := response.floodWait(), 7*time.Second; got != want {
		t.Fatalf("floodWait() = %s, want %s", got, want)
	}
}

func TestAPIFloodWaitRejectsNonRetryableResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "ordinary API error",
			body: `{"ok":false,"error_code":400,"description":"Bad Request","parameters":{"retry_after":7}}`,
		},
		{
			name: "missing retry_after",
			body: `{"ok":false,"error_code":429,"description":"Too Many Requests"}`,
		},
		{
			name: "successful response",
			body: `{"ok":true,"error_code":429,"parameters":{"retry_after":7},"result":true}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var response apiResponse[json.RawMessage]
			if err := json.Unmarshal([]byte(tt.body), &response); err != nil {
				t.Fatal(err)
			}
			if got := response.floodWait(); got != 0 {
				t.Fatalf("floodWait() = %s, want 0", got)
			}
		})
	}
}

func TestTelegramAPIErrorIncludesRetryDelay(t *testing.T) {
	err := (&telegramAPIError{
		Method: "sendPhoto", Description: "Too Many Requests",
		HTTPStatus: 429, Code: 429, RetryAfter: 5,
	}).Error()
	want := "telegram API sendPhoto: Too Many Requests (HTTP 429, error 429); retry after 5s"
	if err != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}
