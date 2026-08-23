package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodimageextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/config"
)

const syntheticAPIKey = "synthetic-gemini-test-key"

func TestExtractorSendsBoundedStrictRequest(t *testing.T) {
	t.Parallel()

	imageBytes := []byte{0x01, 0x02, 0xfe}
	var calls atomic.Int32
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", request.Method)
		}
		if request.URL.Path != "/v1beta/models/configured-test-model:generateContent" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.URL.RawQuery != "" || strings.Contains(request.URL.String(), syntheticAPIKey) {
			t.Errorf("request URL exposes API key or has query parameters")
		}
		if request.Header.Get("x-goog-api-key") != syntheticAPIKey {
			t.Errorf("x-goog-api-key header missing or incorrect")
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", request.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writeGeminiResponse(t, response, "STOP", `{"items":[]}`)
	}))
	defer server.Close()

	extractor := newTestExtractor(t, server.URL, 2*time.Second)
	result, err := extractor.Extract(context.Background(), foodimageextraction.ImageInput{Data: imageBytes, MIMEType: "image/webp"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Items == nil || calls.Load() != 1 {
		t.Fatalf("result/calls = %#v/%d", result, calls.Load())
	}

	systemInstruction := captured["systemInstruction"].(map[string]any)
	systemParts := systemInstruction["parts"].([]any)
	systemText := systemParts[0].(map[string]any)["text"].(string)
	for _, required := range []string{
		"only foods visibly present", "hidden ingredients", "recipes from appearance alone", "invent brands",
		"Prefer uncertainty", "concise English", "no more than 12", "empty items list",
		"calories, macros, nutrition, grams, milliliters, portion weights, serving sizes, quantity estimates, or database IDs",
		"untrusted image content", "never override", "only the structured result",
	} {
		if !strings.Contains(systemText, required) {
			t.Errorf("system instruction missing %q", required)
		}
	}

	contents := captured["contents"].([]any)
	if len(contents) != 1 || contents[0].(map[string]any)["role"] != "user" {
		t.Fatalf("contents = %#v", contents)
	}
	parts := contents[0].(map[string]any)["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("parts = %#v", parts)
	}
	inline := parts[0].(map[string]any)["inlineData"].(map[string]any)
	if inline["mimeType"] != "image/webp" || inline["data"] != base64.StdEncoding.EncodeToString(imageBytes) {
		t.Fatalf("inlineData = %#v", inline)
	}
	if parts[1].(map[string]any)["text"] != userInstruction {
		t.Fatalf("user instruction = %#v", parts[1])
	}

	generation := captured["generationConfig"].(map[string]any)
	if generation["responseMimeType"] != "application/json" || generation["maxOutputTokens"] != float64(maxOutputTokens) {
		t.Fatalf("generation config = %#v", generation)
	}
	thinking := generation["thinkingConfig"].(map[string]any)
	if thinking["thinkingBudget"] != float64(0) {
		t.Fatalf("thinking config = %#v", thinking)
	}
	schema := generation["responseJsonSchema"].(map[string]any)
	if schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Fatalf("root schema = %#v", schema)
	}
	items := schema["properties"].(map[string]any)["items"].(map[string]any)
	if items["maxItems"] != float64(foodimageextraction.MaxItems) {
		t.Fatalf("items schema = %#v", items)
	}
	itemSchema := items["items"].(map[string]any)
	if itemSchema["additionalProperties"] != false || len(itemSchema["required"].([]any)) != 2 {
		t.Fatalf("item schema = %#v", itemSchema)
	}
	if _, exists := captured["tools"]; exists {
		t.Fatal("request unexpectedly contains tools")
	}
}

func TestExtractorDecodesSuccessfulResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{name: "one item", content: `{"items":[{"observation":"a red apple","query":"red apple"}]}`, want: []string{"red apple"}},
		{name: "ordered items", content: `{"items":[{"observation":"rice","query":"white rice"},{"observation":"grilled chicken","query":"grilled chicken"}]}`, want: []string{"white rice", "grilled chicken"}},
		{name: "empty", content: `{"items":[]}`, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := responseServer(t, http.StatusOK, "STOP", tt.content)
			defer server.Close()
			result, err := newTestExtractor(t, server.URL, time.Second).Extract(context.Background(), testInput())
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			if result.Items == nil || len(result.Items) != len(tt.want) {
				t.Fatalf("Items = %#v, want %d items", result.Items, len(tt.want))
			}
			for index, query := range tt.want {
				if result.Items[index].Intent.Query != query {
					t.Fatalf("item %d query = %q, want %q", index, result.Items[index].Intent.Query, query)
				}
				if result.Items[index].Intent.Quantity != nil || result.Items[index].Intent.UnitHint != nil {
					t.Fatalf("item %d contains amount evidence: %#v", index, result.Items[index].Intent)
				}
			}
		})
	}
}

func TestExtractorRejectsInvalidStructuredResponsesWithoutRetry(t *testing.T) {
	t.Parallel()

	items := make([]string, foodimageextraction.MaxItems+1)
	for index := range items {
		items[index] = `{"observation":"apple","query":"apple"}`
	}
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed outer JSON", body: `{`},
		{name: "zero candidates", body: `{"candidates":[]}`},
		{name: "missing content", body: `{"candidates":[{"finishReason":"STOP"}]}`},
		{name: "missing parts", body: `{"candidates":[{"finishReason":"STOP","content":{}}]}`},
		{name: "missing text", body: `{"candidates":[{"finishReason":"STOP","content":{"parts":[{}]}}]}`},
		{name: "blocked", body: `{"promptFeedback":{"blockReason":"SAFETY"}}`},
		{name: "non STOP", body: candidateBody("MAX_TOKENS", `{"items":[]}`)},
		{name: "malformed structured JSON", body: candidateBody("STOP", `{`)},
		{name: "missing items", body: candidateBody("STOP", `{}`)},
		{name: "null items", body: candidateBody("STOP", `{"items":null}`)},
		{name: "unexpected root field", body: candidateBody("STOP", `{"items":[],"extra":true}`)},
		{name: "unexpected item field", body: candidateBody("STOP", `{"items":[{"observation":"apple","query":"apple","extra":true}]}`)},
		{name: "blank observation", body: candidateBody("STOP", `{"items":[{"observation":"  ","query":"apple"}]}`)},
		{name: "blank query", body: candidateBody("STOP", `{"items":[{"observation":"apple","query":"  "}]}`)},
		{name: "too many items", body: candidateBody("STOP", `{"items":[`+strings.Join(items, ",")+`]}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				_, _ = response.Write([]byte(tt.body))
			}))
			defer server.Close()
			_, err := newTestExtractor(t, server.URL, time.Second).Extract(context.Background(), testInput())
			if !foodimageextraction.IsKind(err, foodimageextraction.ErrorInvalidProviderOutput) {
				t.Fatalf("error = %v, want invalid_provider_output", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("requests = %d, want exactly 1", calls.Load())
			}
		})
	}
}

func TestExtractorRejectsOversizedResponseWithoutRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = response.Write([]byte(strings.Repeat("x", maxResponseBodyBytes+1)))
	}))
	defer server.Close()

	_, err := newTestExtractor(t, server.URL, time.Second).Extract(context.Background(), testInput())
	if !foodimageextraction.IsKind(err, foodimageextraction.ErrorInvalidProviderOutput) || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("error = %v, want bounded invalid_provider_output", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("requests = %d, want exactly 1", calls.Load())
	}
}

func TestExtractorMapsHTTPResponsesWithoutRetryOrBodyDisclosure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status int
		kind   foodimageextraction.ErrorKind
	}{
		{status: http.StatusUnauthorized, kind: foodimageextraction.ErrorProviderConfiguration},
		{status: http.StatusForbidden, kind: foodimageextraction.ErrorProviderConfiguration},
		{status: http.StatusRequestTimeout, kind: foodimageextraction.ErrorTimeout},
		{status: http.StatusTooManyRequests, kind: foodimageextraction.ErrorRateLimit},
		{status: http.StatusGatewayTimeout, kind: foodimageextraction.ErrorTimeout},
		{status: http.StatusInternalServerError, kind: foodimageextraction.ErrorProviderFailure},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.status), func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				response.WriteHeader(tt.status)
				_, _ = response.Write([]byte("sensitive provider response body"))
			}))
			defer server.Close()
			_, err := newTestExtractor(t, server.URL, time.Second).Extract(context.Background(), testInput())
			if !foodimageextraction.IsKind(err, tt.kind) {
				t.Fatalf("error = %v, want %s", err, tt.kind)
			}
			if strings.Contains(err.Error(), "sensitive provider response body") {
				t.Fatalf("error exposed provider body: %v", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("requests = %d, want exactly 1", calls.Load())
			}
		})
	}
}

func TestExtractorDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	var redirectedCalls atomic.Int32
	redirected := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectedCalls.Add(1)
	}))
	defer redirected.Close()

	var initialCalls atomic.Int32
	initial := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		initialCalls.Add(1)
		response.Header().Set("Location", redirected.URL)
		response.WriteHeader(http.StatusFound)
	}))
	defer initial.Close()

	_, err := newTestExtractor(t, initial.URL, time.Second).Extract(context.Background(), testInput())
	if !foodimageextraction.IsKind(err, foodimageextraction.ErrorProviderFailure) {
		t.Fatalf("error = %v, want provider_failure", err)
	}
	if initialCalls.Load() != 1 || redirectedCalls.Load() != 0 {
		t.Fatalf("initial/redirected requests = %d/%d, want 1/0", initialCalls.Load(), redirectedCalls.Load())
	}
}

func TestExtractorMapsTransportFailureWithoutRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("synthetic transport detail")
	})}
	extractor, err := NewExtractor(config.Gemini{APIKey: syntheticAPIKey, Model: "model", Timeout: time.Second}, WithHTTPClient(client))
	if err != nil {
		t.Fatalf("NewExtractor: %v", err)
	}
	_, err = extractor.Extract(context.Background(), testInput())
	if !foodimageextraction.IsKind(err, foodimageextraction.ErrorProviderFailure) {
		t.Fatalf("error = %v, want provider_failure", err)
	}
	if strings.Contains(err.Error(), "synthetic transport detail") {
		t.Fatalf("error exposed transport detail: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("transport calls = %d, want exactly 1", calls.Load())
	}
}

func TestExtractorConfiguredTimeout(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		<-release
	}))
	defer server.Close()

	_, err := newTestExtractor(t, server.URL, 20*time.Millisecond).Extract(context.Background(), testInput())
	close(release)
	if !foodimageextraction.IsKind(err, foodimageextraction.ErrorTimeout) {
		t.Fatalf("error = %v, want timeout", err)
	}
	if calls.Load() > 1 {
		t.Fatalf("requests = %d, want at most 1", calls.Load())
	}
}

func TestExtractorHonorsCallerCancellation(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		<-request.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newTestExtractor(t, server.URL, time.Second).Extract(ctx, testInput())
	if !foodimageextraction.IsKind(err, foodimageextraction.ErrorCanceled) {
		t.Fatalf("error = %v, want canceled", err)
	}
	if calls.Load() > 1 {
		t.Fatalf("requests = %d, want at most 1", calls.Load())
	}
}

func TestNewExtractorRequiresUsableConfigurationWithoutExposingKey(t *testing.T) {
	t.Parallel()

	tests := []config.Gemini{
		{Model: "model", Timeout: time.Second},
		{APIKey: syntheticAPIKey, Timeout: time.Second},
		{APIKey: syntheticAPIKey, Model: "model"},
		{APIKey: syntheticAPIKey, Model: "model", Timeout: -time.Second},
	}
	for _, cfg := range tests {
		_, err := NewExtractor(cfg)
		if !foodimageextraction.IsKind(err, foodimageextraction.ErrorProviderConfiguration) {
			t.Fatalf("error = %v, want provider_configuration", err)
		}
		if strings.Contains(err.Error(), syntheticAPIKey) {
			t.Fatalf("error exposed API key: %v", err)
		}
	}
}

func TestZeroValueExtractorFailsAsProviderConfiguration(t *testing.T) {
	t.Parallel()

	var extractor Extractor
	_, err := extractor.Extract(context.Background(), testInput())
	if !foodimageextraction.IsKind(err, foodimageextraction.ErrorProviderConfiguration) {
		t.Fatalf("error = %v, want provider_configuration", err)
	}
}

func newTestExtractor(t *testing.T, baseURL string, timeout time.Duration) *Extractor {
	t.Helper()
	extractor, err := NewExtractor(config.Gemini{
		APIKey: syntheticAPIKey, Model: "configured-test-model", Timeout: timeout,
	}, WithBaseURL(baseURL))
	if err != nil {
		t.Fatalf("NewExtractor: %v", err)
	}
	return extractor
}

func testInput() foodimageextraction.ImageInput {
	return foodimageextraction.ImageInput{Data: []byte{1, 2, 3}, MIMEType: "image/jpeg"}
}

func responseServer(t *testing.T, status int, finishReason, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(status)
		writeGeminiResponse(t, response, finishReason, content)
	}))
}

func writeGeminiResponse(t *testing.T, response http.ResponseWriter, finishReason, content string) {
	t.Helper()
	if _, err := response.Write([]byte(candidateBody(finishReason, content))); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func candidateBody(finishReason, content string) string {
	body, _ := json.Marshal(map[string]any{
		"candidates": []any{map[string]any{
			"finishReason": finishReason,
			"content":      map[string]any{"parts": []any{map[string]any{"text": content}}},
		}},
	})
	return string(body)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
