package claude

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"ds2api/internal/auth"
)

type streamStatusClaudeAuthStub struct{}

func (streamStatusClaudeAuthStub) Determine(_ *http.Request) (*auth.RequestAuth, error) {
	return &auth.RequestAuth{
		UseConfigToken: false,
		DeepSeekToken:  "direct-token",
		CallerID:       "caller:test",
		TriedAccounts:  map[string]bool{},
	}, nil
}

func (streamStatusClaudeAuthStub) Release(_ *auth.RequestAuth) {}

type streamStatusClaudeDSStub struct{}

func (streamStatusClaudeDSStub) CreateSession(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "session-id", nil
}

func (streamStatusClaudeDSStub) GetPow(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "pow", nil
}

func (streamStatusClaudeDSStub) CallCompletion(_ context.Context, _ *auth.RequestAuth, _ map[string]any, _ string, _ int) (*http.Response, error) {
	body := "data: {\"p\":\"response/content\",\"v\":\"hello\"}\n" + "data: [DONE]\n"
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       ioNopCloser{strings.NewReader(body)},
	}, nil
}

type ioNopCloser struct {
	*strings.Reader
}

func (ioNopCloser) Close() error { return nil }

type streamStatusClaudeStoreStub struct{}

func (streamStatusClaudeStoreStub) ClaudeMapping() map[string]string {
	return map[string]string{
		"fast": "deepseek-chat",
		"slow": "deepseek-reasoner",
	}
}

func captureClaudeStatusMiddleware(statuses *[]int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			*statuses = append(*statuses, ww.Status())
		})
	}
}

func TestClaudeMessagesStreamStatusCapturedAs200(t *testing.T) {
	statuses := make([]int, 0, 1)
	h := &Handler{
		Store: streamStatusClaudeStoreStub{},
		Auth:  streamStatusClaudeAuthStub{},
		DS:    streamStatusClaudeDSStub{},
	}
	r := chi.NewRouter()
	r.Use(captureClaudeStatusMiddleware(&statuses))
	RegisterRoutes(r, h)

	reqBody := `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(statuses) != 1 {
		t.Fatalf("expected one captured status, got %d", len(statuses))
	}
	if statuses[0] != http.StatusOK {
		t.Fatalf("expected captured status 200 (not 000), got %d", statuses[0])
	}
}
