package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodamount"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodimageextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/mealai"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

func (stub *stubMealTextInterpreter) InterpretImage(ctx context.Context, request mealai.ImageRequest) (mealai.ImageResult, error) {
	if stub.panicValue != nil {
		panic(stub.panicValue)
	}
	stub.imageCalls++
	stub.ctx = ctx
	stub.imageRequest = request
	return stub.imageResult, stub.imageErr
}

func TestMealImageInterpretAcceptsSupportedImagesAndForwardsRawInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mimeType string
		data     []byte
	}{
		{name: "JPEG", mimeType: "image/jpeg", data: jpegBytes()},
		{name: "PNG", mimeType: "image/png", data: pngBytes()},
		{name: "WebP", mimeType: "image/webp", data: webpBytes()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubMealTextInterpreter{imageResult: mealai.ImageResult{State: mealai.StateEmpty, Items: []mealai.ImageItem{}}}
			request := newMultipartImageRequest(t, []multipartTestField{
				{name: "locale", data: []byte("TR-tr")},
				{name: "image", contentType: tt.mimeType, data: tt.data},
			})
			response := performPreparedRequest(mealRouter(stub), request)
			if response.Code != http.StatusOK || response.Body.String() != "{\"state\":\"empty\",\"items\":[]}\n" {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
			if stub.imageCalls != 1 || stub.imageRequest.Locale != "TR-tr" || stub.imageRequest.Image.MIMEType != tt.mimeType || !bytes.Equal(stub.imageRequest.Image.Data, tt.data) {
				t.Fatalf("application request = %#v calls=%d", stub.imageRequest, stub.imageCalls)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestMealImageInterpretAcceptsOmittedLocaleAndImageFirst(t *testing.T) {
	t.Parallel()

	stub := &stubMealTextInterpreter{imageResult: mealai.ImageResult{State: mealai.StateEmpty, Items: []mealai.ImageItem{}}}
	request := newMultipartImageRequest(t, []multipartTestField{{name: "image", contentType: "image/jpeg", data: jpegBytes()}})
	response := performPreparedRequest(mealRouter(stub), request)
	if response.Code != http.StatusOK || stub.imageRequest.Locale != "" || !bytes.Equal(stub.imageRequest.Image.Data, jpegBytes()) {
		t.Fatalf("response/request = %d %q/%#v", response.Code, response.Body.String(), stub.imageRequest)
	}
}

func TestMealImageInterpretAcceptsImageBeforeLocale(t *testing.T) {
	t.Parallel()

	stub := &stubMealTextInterpreter{imageResult: mealai.ImageResult{State: mealai.StateEmpty, Items: []mealai.ImageItem{}}}
	request := newMultipartImageRequest(t, []multipartTestField{
		{name: "image", contentType: "image/png", data: pngBytes()},
		{name: "locale", data: []byte("en-US")},
	})
	response := performPreparedRequest(mealRouter(stub), request)
	if response.Code != http.StatusOK || stub.imageRequest.Locale != "en-US" || stub.imageRequest.Image.MIMEType != "image/png" {
		t.Fatalf("response/request = %d %q/%#v", response.Code, response.Body.String(), stub.imageRequest)
	}
}

func TestMealImageInterpretContentTypeContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
		wantBody    string
	}{
		{name: "missing", wantStatus: 415, wantBody: "unsupported_media_type"},
		{name: "unparseable", contentType: `multipart/form-data; boundary="unterminated`, wantStatus: 415, wantBody: "unsupported_media_type"},
		{name: "JSON", contentType: "application/json", body: `{}`, wantStatus: 415, wantBody: "unsupported_media_type"},
		{name: "missing boundary", contentType: "multipart/form-data", wantStatus: 400, wantBody: "invalid_request"},
		{name: "malformed body", contentType: "multipart/form-data; boundary=boundary", body: "not multipart", wantStatus: 400, wantBody: "invalid_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubMealTextInterpreter{}
			request := httptest.NewRequest(http.MethodPost, "/ai/meals/interpret-image", strings.NewReader(tt.body))
			if tt.contentType != "" {
				request.Header.Set("Content-Type", tt.contentType)
			}
			response := performPreparedRequest(mealRouter(stub), request)
			assertStatusResponse(t, response, tt.wantStatus, tt.wantBody)
			if stub.imageCalls != 0 {
				t.Fatalf("application calls = %d, want 0", stub.imageCalls)
			}
		})
	}
}

func TestMealImageInterpretRejectsInvalidMultipartFields(t *testing.T) {
	t.Parallel()

	validImage := multipartTestField{name: "image", contentType: "image/jpeg", data: jpegBytes()}
	tests := []struct {
		name   string
		fields []multipartTestField
	}{
		{name: "missing image", fields: []multipartTestField{{name: "locale", data: []byte("tr")}}},
		{name: "duplicate image", fields: []multipartTestField{validImage, validImage}},
		{name: "duplicate locale", fields: []multipartTestField{validImage, {name: "locale", data: []byte("tr")}, {name: "locale", data: []byte("en")}}},
		{name: "unknown field", fields: []multipartTestField{validImage, {name: "caption", data: []byte("private")}}},
		{name: "locale too large", fields: []multipartTestField{validImage, {name: "locale", data: bytes.Repeat([]byte("x"), mealImageLocaleByteLimit+1)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubMealTextInterpreter{}
			response := performPreparedRequest(mealRouter(stub), newMultipartImageRequest(t, tt.fields))
			assertStatusResponse(t, response, http.StatusBadRequest, "invalid_request")
			if stub.imageCalls != 0 {
				t.Fatalf("application calls = %d, want 0", stub.imageCalls)
			}
		})
	}
}

func TestMealImageInterpretRejectsUnsupportedMIMEAndSignature(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		data        []byte
	}{
		{name: "unsupported MIME", contentType: "image/gif", data: []byte("GIF89a")},
		{name: "missing MIME", data: jpegBytes()},
		{name: "invalid JPEG", contentType: "image/jpeg", data: []byte("not jpeg")},
		{name: "invalid PNG", contentType: "image/png", data: []byte("not png")},
		{name: "invalid WebP", contentType: "image/webp", data: []byte("RIFFxxxxNOPE")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubMealTextInterpreter{}
			response := performPreparedRequest(mealRouter(stub), newMultipartImageRequest(t, []multipartTestField{{
				name: "image", contentType: tt.contentType, data: tt.data,
			}}))
			assertStatusResponse(t, response, http.StatusUnsupportedMediaType, "unsupported_media_type")
			if stub.imageCalls != 0 {
				t.Fatalf("application calls = %d, want 0", stub.imageCalls)
			}
		})
	}
}

func TestMealImageInterpretSizeLimits(t *testing.T) {
	validPrefix := jpegBytes()

	t.Run("image part", func(t *testing.T) {
		data := make([]byte, foodimageextraction.MaxImageBytes+1)
		copy(data, validPrefix)
		stub := &stubMealTextInterpreter{}
		response := performPreparedRequest(mealRouter(stub), newMultipartImageRequest(t, []multipartTestField{{
			name: "image", contentType: "image/jpeg", data: data,
		}}))
		assertStatusResponse(t, response, http.StatusRequestEntityTooLarge, "payload_too_large")
		if stub.imageCalls != 0 {
			t.Fatalf("application calls = %d, want 0", stub.imageCalls)
		}
	})

	t.Run("whole request during multipart read", func(t *testing.T) {
		data := make([]byte, foodimageextraction.MaxImageBytes)
		copy(data, validPrefix)
		request := newMultipartImageRequest(t, []multipartTestField{
			{name: "image", contentType: "image/jpeg", data: data},
			{name: "padding", data: bytes.Repeat([]byte("x"), 70*1024)},
		})
		request.ContentLength = -1
		stub := &stubMealTextInterpreter{}
		response := performPreparedRequest(mealRouter(stub), request)
		assertStatusResponse(t, response, http.StatusRequestEntityTooLarge, "payload_too_large")
		if stub.imageCalls != 0 {
			t.Fatalf("application calls = %d, want 0", stub.imageCalls)
		}
	})
}

func TestMealImageInterpretMapsApplicationErrorsWithoutDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{name: "AI unavailable", err: &mealai.Error{Kind: mealai.ErrorAIUnavailable}, wantStatus: 503, wantBody: "ai_unavailable"},
		{name: "AI rate limited", err: &mealai.Error{Kind: mealai.ErrorAIRateLimited}, wantStatus: 429, wantBody: "ai_rate_limited"},
		{name: "AI timeout", err: &mealai.Error{Kind: mealai.ErrorAITimeout}, wantStatus: 504, wantBody: "ai_timeout"},
		{name: "AI invalid", err: &mealai.Error{Kind: mealai.ErrorAIInvalidResponse}, wantStatus: 502, wantBody: "ai_invalid_response"},
		{name: "AI failure", err: errors.Join(&mealai.Error{Kind: mealai.ErrorAIFailure}, errors.New("private provider detail")), wantStatus: 502, wantBody: "ai_provider_error"},
		{name: "dependency timeout", err: &mealai.Error{Kind: mealai.ErrorTimeout}, wantStatus: 504, wantBody: "dependency_timeout"},
		{name: "canceled", err: &mealai.Error{Kind: mealai.ErrorCanceled}, wantStatus: 408, wantBody: "request_canceled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubMealTextInterpreter{imageErr: tt.err}
			response := performPreparedRequest(mealRouter(stub), validImageRequest(t))
			assertStatusResponse(t, response, tt.wantStatus, tt.wantBody)
			if strings.Contains(response.Body.String(), "private provider detail") {
				t.Fatalf("response leaked provider details: %q", response.Body.String())
			}
		})
	}
}

func TestMealImageInterpretWithoutGeminiConfigurationReturnsAIUnavailable(t *testing.T) {
	t.Parallel()

	service := mealai.NewService(
		foodextraction.NewService(nil), foodimageextraction.NewService(nil),
		nil, nil, nil, nil,
	)
	response := performPreparedRequest(mealRouter(service), validImageRequest(t))
	assertStatusResponse(t, response, http.StatusServiceUnavailable, "ai_unavailable")
}

func TestMealImageInterpretResponseUsesObservationAndPreservesOrder(t *testing.T) {
	t.Parallel()

	result := mealai.ImageResult{State: mealai.StateClarificationRequired, Items: []mealai.ImageItem{
		{
			Observation: "a red apple", Intent: foodintent.FoodIntent{Query: "red apple"}, State: mealai.ItemReady,
			Food:      &mealai.ResolvedFood{FoodID: 1, DisplayName: "Elma", CanonicalName: "Apple"},
			Selection: &foodamount.Selection{Kind: foodamount.SelectionGrams, FoodID: 1, Grams: &foodamount.GramsSelection{Grams: 100}},
			Preview:   &mealai.NutritionPreview{ResolvedGrams: 100},
		},
		{
			Observation: "plain white rice", Intent: foodintent.FoodIntent{Query: "white rice"}, State: mealai.ItemClarificationRequired,
			Food: &mealai.ResolvedFood{FoodID: 2, DisplayName: "Pirinç", CanonicalName: "Rice"},
			Clarification: &mealai.Clarification{
				Kind: mealai.ClarificationAmount, Reason: "quantity_required", Candidates: []mealai.FoodOption{},
				Portions: []food.Portion{}, AllowDirectGrams: true,
			},
		},
	}}
	stub := &stubMealTextInterpreter{imageResult: result}
	response := performPreparedRequest(mealRouter(stub), validImageRequest(t))
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, `"mention"`) || !strings.Contains(body, `"observation":"a red apple"`) ||
		!strings.Contains(body, `"quantity":null`) || !strings.Contains(body, `"unit_hint":null`) ||
		!strings.Contains(body, `"kind":"amount"`) {
		t.Fatalf("unexpected response contract: %s", body)
	}
	if strings.Index(body, "a red apple") > strings.Index(body, "plain white rice") {
		t.Fatalf("item order changed: %s", body)
	}
}

func TestMealImageInterpretLogsOnlyControlledSummary(t *testing.T) {
	t.Parallel()

	const (
		observation = "PRIVATE_IMAGE_OBSERVATION_SENTINEL"
		query       = "PRIVATE_IMAGE_QUERY_SENTINEL"
		imageText   = "PRIVATE_IMAGE_BYTES_SENTINEL"
	)
	item := readyImageItemHTTP()
	item.Observation = observation
	item.Intent.Query = query
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	stub := &stubMealTextInterpreter{imageResult: mealai.ImageResult{State: mealai.StateReady, Items: []mealai.ImageItem{item}}}
	image := append(jpegBytes(), []byte(imageText)...)
	request := newMultipartImageRequest(t, []multipartTestField{{name: "image", contentType: "image/jpeg", data: image}})
	response := performPreparedRequest(NewRouter(
		logger, time.Second, func(context.Context) error { return nil },
		&stubFoodSearcher{}, nil, nil, stub,
	), request)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	logText := logs.String()
	for _, expected := range []string{`"state":"ready"`, `"item_count":1`, `"ready_count":1`, `"clarification_count":0`, `"request_id":`} {
		if !strings.Contains(logText, expected) {
			t.Fatalf("logs %q missing %q", logText, expected)
		}
	}
	for _, sensitive := range []string{observation, query, imageText} {
		if strings.Contains(logText, sensitive) {
			t.Fatalf("logs exposed %q", sensitive)
		}
	}
}

func TestTextMealInterpretContractStillUsesMentionOnly(t *testing.T) {
	t.Parallel()

	imageItem := readyImageItemHTTP()
	result := mealai.Result{State: mealai.StateReady, Items: []mealai.Item{{
		Mention: "an apple", Intent: imageItem.Intent, State: imageItem.State,
		Food: imageItem.Food, Selection: imageItem.Selection, Preview: imageItem.Preview,
	}}}
	response := performRequestWithBody(
		mealRouter(&stubMealTextInterpreter{result: result}), http.MethodPost,
		"/ai/meals/interpret", `{"text":"an apple","locale":"en"}`,
	)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"mention":"an apple"`) || strings.Contains(response.Body.String(), `"observation"`) {
		t.Fatalf("text response contract changed: %d %s", response.Code, response.Body.String())
	}
}

func TestMealImageInterpretRejectsMalformedApplicationResults(t *testing.T) {
	t.Parallel()

	quantity := 1.0
	unit := "piece"
	validReady := readyImageItemHTTP()
	clarification := mealai.ImageItem{
		Observation: "an apple", Intent: foodintent.FoodIntent{Query: "apple"}, State: mealai.ItemClarificationRequired,
		Clarification: &mealai.Clarification{
			Kind: mealai.ClarificationFoodIdentity, Reason: "ambiguous", Candidates: []mealai.FoodOption{{FoodID: 1}}, Portions: []food.Portion{},
		},
	}
	tests := []mealai.ImageResult{
		{State: mealai.StateEmpty, Items: nil},
		{State: mealai.StateReady, Items: []mealai.ImageItem{{Observation: " bad ", Intent: foodintent.FoodIntent{Query: "apple"}, State: mealai.ItemReady}}},
		{State: mealai.StateReady, Items: []mealai.ImageItem{{Observation: "apple", Intent: foodintent.FoodIntent{Query: "x"}, State: mealai.ItemReady}}},
		{State: mealai.StateReady, Items: []mealai.ImageItem{{Observation: "apple", Intent: foodintent.FoodIntent{Query: "apple", Quantity: &quantity}, State: mealai.ItemReady}}},
		{State: mealai.StateReady, Items: []mealai.ImageItem{{Observation: "apple", Intent: foodintent.FoodIntent{Query: "apple", UnitHint: &unit}, State: mealai.ItemReady}}},
		{State: mealai.StateReady, Items: []mealai.ImageItem{clarification}},
		{State: mealai.StateClarificationRequired, Items: []mealai.ImageItem{validReady}},
	}
	for _, result := range tests {
		stub := &stubMealTextInterpreter{imageResult: result}
		response := performPreparedRequest(mealRouter(stub), validImageRequest(t))
		assertStatusResponse(t, response, http.StatusInternalServerError, "internal_error")
	}
}

func TestMealImageInterpretIsPostOnly(t *testing.T) {
	t.Parallel()

	response := performRequest(mealRouter(&stubMealTextInterpreter{}), http.MethodGet, "/ai/meals/interpret-image")
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodPost || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response = %d Allow=%q Cache-Control=%q", response.Code, response.Header().Get("Allow"), response.Header().Get("Cache-Control"))
	}
}

type multipartTestField struct {
	name        string
	contentType string
	data        []byte
}

func newMultipartImageRequest(t *testing.T, fields []multipartTestField) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, field := range fields {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", `form-data; name="`+field.name+`"`)
		if field.contentType != "" {
			header.Set("Content-Type", field.contentType)
		}
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatalf("create multipart part: %v", err)
		}
		if _, err := part.Write(field.data); err != nil {
			t.Fatalf("write multipart part: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/ai/meals/interpret-image", bytes.NewReader(body.Bytes()))
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func validImageRequest(t *testing.T) *http.Request {
	t.Helper()
	return newMultipartImageRequest(t, []multipartTestField{{name: "image", contentType: "image/jpeg", data: jpegBytes()}})
}

func performPreparedRequest(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertStatusResponse(t *testing.T, response *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if response.Code != status || response.Body.String() != "{\"status\":\""+body+"\"}\n" {
		t.Fatalf("response = %d %q, want %d %q", response.Code, response.Body.String(), status, body)
	}
}

func readyImageItemHTTP() mealai.ImageItem {
	return mealai.ImageItem{
		Observation: "an apple", Intent: foodintent.FoodIntent{Query: "apple"}, State: mealai.ItemReady,
		Food:      &mealai.ResolvedFood{FoodID: 1, DisplayName: "Elma", CanonicalName: "Apple"},
		Selection: &foodamount.Selection{Kind: foodamount.SelectionGrams, FoodID: 1, Grams: &foodamount.GramsSelection{Grams: 100}},
		Preview:   &mealai.NutritionPreview{ResolvedGrams: 100},
	}
}

func jpegBytes() []byte { return []byte{0xff, 0xd8, 0xff, 0x01, 0x02} }

func pngBytes() []byte { return []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x01} }

func webpBytes() []byte { return []byte("RIFFxxxxWEBPdata") }
