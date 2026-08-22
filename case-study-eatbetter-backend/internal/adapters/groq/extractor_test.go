package groq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/config"
)

const syntheticAPIKey = "synthetic-groq-test-key"

func TestExtractorSendsBoundedStrictRequest(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", request.Method)
		}
		if request.URL.Path != chatCompletionsPath {
			t.Errorf("path = %s, want %s", request.URL.Path, chatCompletionsPath)
		}
		if request.Header.Get("Authorization") != "Bearer "+syntheticAPIKey {
			t.Errorf("Authorization header missing or incorrect")
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", request.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writeChatResponse(t, response, "stop", `{"items":[]}`)
	}))
	defer server.Close()

	extractor := newTestExtractor(t, server.URL, 2*time.Second)
	if _, err := extractor.Extract(context.Background(), "elma"); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if captured["model"] != "configured-test-model" {
		t.Fatalf("model = %#v", captured["model"])
	}
	if captured["stream"] != false || captured["include_reasoning"] != false {
		t.Fatalf("stream/reasoning flags = %#v/%#v", captured["stream"], captured["include_reasoning"])
	}
	if captured["reasoning_effort"] != "low" {
		t.Fatalf("reasoning_effort = %#v", captured["reasoning_effort"])
	}
	if captured["max_completion_tokens"] != float64(maxCompletionTokens) {
		t.Fatalf("max_completion_tokens = %#v", captured["max_completion_tokens"])
	}
	if _, exists := captured["tools"]; exists {
		t.Fatal("request unexpectedly contains tools")
	}
	messages, ok := captured["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v, want system and user", captured["messages"])
	}
	userMessage := messages[1].(map[string]any)
	if userMessage["role"] != "user" || userMessage["content"] != "elma" {
		t.Fatalf("user message = %#v", userMessage)
	}

	format := captured["response_format"].(map[string]any)
	if format["type"] != "json_schema" {
		t.Fatalf("response format type = %#v", format["type"])
	}
	schemaFormat := format["json_schema"].(map[string]any)
	if schemaFormat["name"] != "food_intent_extraction_v1" || schemaFormat["strict"] != true {
		t.Fatalf("json_schema metadata = %#v", schemaFormat)
	}
	schema := schemaFormat["schema"].(map[string]any)
	if schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Fatalf("root schema = %#v", schema)
	}
	properties := schema["properties"].(map[string]any)
	items := properties["items"].(map[string]any)
	itemSchema := items["items"].(map[string]any)
	intent := itemSchema["properties"].(map[string]any)["intent"].(map[string]any)
	if intent["additionalProperties"] != false || len(intent["required"].([]any)) != 3 {
		t.Fatalf("intent schema = %#v", intent)
	}
}

func TestExtractorDecodesStructuredResponse(t *testing.T) {
	t.Parallel()

	server := responseServer(t, http.StatusOK, "stop", `{"items":[{"mention":"2 yumurta","intent":{"query":"yumurta","quantity":2,"unitHint":"adet"}}]}`)
	defer server.Close()

	result, err := newTestExtractor(t, server.URL, time.Second).Extract(context.Background(), "2 yumurta")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Mention != "2 yumurta" || *result.Items[0].Intent.Quantity != 2 || *result.Items[0].Intent.UnitHint != "adet" {
		t.Fatalf("result = %#v", result)
	}
}

func TestExtractorDecodesEmptyItems(t *testing.T) {
	t.Parallel()

	server := responseServer(t, http.StatusOK, "stop", `{"items":[]}`)
	defer server.Close()

	result, err := newTestExtractor(t, server.URL, time.Second).Extract(context.Background(), "hiçbir şey yemedim")
	if err != nil || result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestExtractorRejectsInvalidProviderResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		status       int
		body         string
		finishReason string
		content      string
		kind         foodextraction.ErrorKind
	}{
		{name: "upstream status", status: http.StatusBadGateway, body: `provider details`, kind: foodextraction.ErrorProviderFailure},
		{name: "rate limit", status: http.StatusTooManyRequests, body: `limited`, kind: foodextraction.ErrorRateLimit},
		{name: "invalid credentials", status: http.StatusUnauthorized, body: `credential details`, kind: foodextraction.ErrorProviderConfiguration},
		{name: "malformed provider JSON", status: http.StatusOK, body: `{`, kind: foodextraction.ErrorInvalidProviderOutput},
		{name: "missing choices", status: http.StatusOK, body: `{"choices":[]}`, kind: foodextraction.ErrorInvalidProviderOutput},
		{name: "missing content", status: http.StatusOK, body: `{"choices":[{"finish_reason":"stop","message":{}}]}`, kind: foodextraction.ErrorInvalidProviderOutput},
		{name: "empty content", status: http.StatusOK, finishReason: "stop", content: "  ", kind: foodextraction.ErrorInvalidProviderOutput},
		{name: "length finish", status: http.StatusOK, finishReason: "length", content: `{"items":[]}`, kind: foodextraction.ErrorInvalidProviderOutput},
		{name: "unexpected finish", status: http.StatusOK, finishReason: "tool_calls", content: `{"items":[]}`, kind: foodextraction.ErrorInvalidProviderOutput},
		{name: "missing finish", status: http.StatusOK, finishReason: "", content: `{"items":[]}`, kind: foodextraction.ErrorInvalidProviderOutput},
		{name: "malformed structured content", status: http.StatusOK, finishReason: "stop", content: `{`, kind: foodextraction.ErrorInvalidProviderOutput},
		{name: "extra contract field", status: http.StatusOK, finishReason: "stop", content: `{"items":[],"extra":true}`, kind: foodextraction.ErrorInvalidProviderOutput},
		{name: "missing nullable field", status: http.StatusOK, finishReason: "stop", content: `{"items":[{"mention":"elma","intent":{"query":"elma","unitHint":null}}]}`, kind: foodextraction.ErrorInvalidProviderOutput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(tt.status)
				if tt.body != "" {
					_, _ = response.Write([]byte(tt.body))
					return
				}
				writeChatResponse(t, response, tt.finishReason, tt.content)
			}))
			defer server.Close()

			_, err := newTestExtractor(t, server.URL, time.Second).Extract(context.Background(), "elma")
			if !foodextraction.IsKind(err, tt.kind) {
				t.Fatalf("error = %v, want %s", err, tt.kind)
			}
			if err != nil && strings.Contains(err.Error(), "provider details") {
				t.Fatalf("error exposed provider response body: %v", err)
			}
		})
	}
}

func TestExtractorTimeout(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	defer server.Close()

	_, err := newTestExtractor(t, server.URL, 20*time.Millisecond).Extract(context.Background(), "elma")
	close(release)
	if !foodextraction.IsKind(err, foodextraction.ErrorTimeout) {
		t.Fatalf("error = %v, want timeout", err)
	}
}

func TestExtractorHonorsCallerCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newTestExtractor(t, server.URL, time.Second).Extract(ctx, "elma")
	if !foodextraction.IsKind(err, foodextraction.ErrorCanceled) {
		t.Fatalf("error = %v, want canceled", err)
	}
}

func TestExtractorRejectsOversizedResponseBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(strings.Repeat("x", maxResponseBodyBytes+1)))
	}))
	defer server.Close()

	_, err := newTestExtractor(t, server.URL, time.Second).Extract(context.Background(), "elma")
	if !foodextraction.IsKind(err, foodextraction.ErrorInvalidProviderOutput) || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("error = %v, want bounded-response rejection", err)
	}
}

func TestNewExtractorRequiresUsableConfigurationWithoutExposingKey(t *testing.T) {
	t.Parallel()

	tests := []config.Groq{
		{Model: "model", Timeout: time.Second},
		{APIKey: syntheticAPIKey, Timeout: time.Second},
		{APIKey: syntheticAPIKey, Model: "model"},
	}
	for _, cfg := range tests {
		_, err := NewExtractor(cfg)
		if !foodextraction.IsKind(err, foodextraction.ErrorProviderConfiguration) {
			t.Fatalf("error = %v, want provider configuration", err)
		}
		if err != nil && strings.Contains(err.Error(), syntheticAPIKey) {
			t.Fatalf("error exposed API key: %v", err)
		}
	}
}

func TestZeroValueExtractorFailsAsProviderConfiguration(t *testing.T) {
	t.Parallel()

	var extractor Extractor
	_, err := extractor.Extract(context.Background(), "elma")
	if !foodextraction.IsKind(err, foodextraction.ErrorProviderConfiguration) {
		t.Fatalf("error = %v, want provider configuration", err)
	}
}

func newTestExtractor(t *testing.T, baseURL string, timeout time.Duration) *Extractor {
	t.Helper()
	extractor, err := NewExtractor(config.Groq{
		APIKey: syntheticAPIKey, Model: "configured-test-model", Timeout: timeout,
	}, WithBaseURL(baseURL))
	if err != nil {
		t.Fatalf("NewExtractor: %v", err)
	}
	return extractor
}

func responseServer(t *testing.T, status int, finishReason, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(status)
		writeChatResponse(t, response, finishReason, content)
	}))
}

func writeChatResponse(t *testing.T, response http.ResponseWriter, finishReason, content string) {
	t.Helper()
	if err := json.NewEncoder(response).Encode(map[string]any{
		"choices": []any{map[string]any{
			"finish_reason": finishReason,
			"message":       map[string]any{"content": content},
		}},
	}); err != nil {
		t.Errorf("encode response: %v", err)
	}
}
