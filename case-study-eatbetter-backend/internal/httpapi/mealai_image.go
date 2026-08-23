package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodimageextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/mealai"
)

const (
	mealImageRequestBodyLimit = foodimageextraction.MaxImageBytes + 64*1024
	mealImageLocaleByteLimit  = 128
)

type imageUploadErrorKind uint8

const (
	imageUploadInvalidRequest imageUploadErrorKind = iota + 1
	imageUploadUnsupportedMediaType
	imageUploadPayloadTooLarge
	imageUploadRequestCanceled
)

type imageUploadError struct{ kind imageUploadErrorKind }

func (e *imageUploadError) Error() string {
	switch e.kind {
	case imageUploadUnsupportedMediaType:
		return "unsupported media type"
	case imageUploadPayloadTooLarge:
		return "payload too large"
	case imageUploadRequestCanceled:
		return "request canceled"
	default:
		return "invalid multipart request"
	}
}

func mealImageInterpretHandler(logger *slog.Logger, interpreter MealAIService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request, err := parseMealImageRequest(w, r)
		if err != nil {
			statusCode, status := imageUploadErrorResponse(err)
			writeStatus(w, statusCode, status)
			return
		}
		if interpreter == nil {
			logger.ErrorContext(r.Context(), "image meal interpretation dependency unavailable",
				"request_id", requestIDFromContext(r.Context()))
			writeStatus(w, http.StatusInternalServerError, "internal_error")
			return
		}

		result, err := interpreter.InterpretImage(r.Context(), request)
		if err != nil {
			statusCode, status, kind := mealInterpretErrorResponse(err)
			logger.ErrorContext(r.Context(), "image meal interpretation failed",
				"request_id", requestIDFromContext(r.Context()), "error_kind", kind)
			writeStatus(w, statusCode, status)
			return
		}
		response, err := mapMealImageResult(result)
		if err != nil {
			logger.ErrorContext(r.Context(), "image meal interpretation result was invalid",
				"request_id", requestIDFromContext(r.Context()))
			writeStatus(w, http.StatusInternalServerError, "internal_error")
			return
		}
		readyCount, clarificationCount := mealImageItemCounts(result.Items)
		logger.InfoContext(r.Context(), "image meal interpretation completed",
			"request_id", requestIDFromContext(r.Context()),
			"state", result.State,
			"item_count", len(result.Items),
			"ready_count", readyCount,
			"clarification_count", clarificationCount,
		)
		writeJSON(w, http.StatusOK, response)
	})
}

func parseMealImageRequest(w http.ResponseWriter, r *http.Request) (mealai.ImageRequest, error) {
	contentType := r.Header.Get("Content-Type")
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		return mealai.ImageRequest{}, &imageUploadError{kind: imageUploadUnsupportedMediaType}
	}
	if strings.TrimSpace(parameters["boundary"]) == "" {
		return mealai.ImageRequest{}, &imageUploadError{kind: imageUploadInvalidRequest}
	}

	requestBody := &maxBytesTrackingReadCloser{ReadCloser: http.MaxBytesReader(w, r.Body, mealImageRequestBodyLimit)}
	r.Body = requestBody
	if r.ContentLength > mealImageRequestBodyLimit {
		return mealai.ImageRequest{}, &imageUploadError{kind: imageUploadPayloadTooLarge}
	}
	reader, err := r.MultipartReader()
	if err != nil {
		return mealai.ImageRequest{}, classifyMultipartError(r.Context(), requestBody, imageUploadInvalidRequest, err)
	}

	var imageData []byte
	var imageMIME, locale string
	imageSeen, localeSeen := false, false
	for {
		part, err := reader.NextRawPart()
		if requestBody.exceeded {
			return mealai.ImageRequest{}, &imageUploadError{kind: imageUploadPayloadTooLarge}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return mealai.ImageRequest{}, classifyMultipartError(r.Context(), requestBody, imageUploadInvalidRequest, err)
		}

		switch part.FormName() {
		case "image":
			if imageSeen {
				return mealai.ImageRequest{}, closePartWithError(r.Context(), part, requestBody, imageUploadInvalidRequest)
			}
			imageSeen = true
			imageMIME, err = parseImagePartMIME(part.Header.Get("Content-Type"))
			if err != nil {
				return mealai.ImageRequest{}, closePartWithError(r.Context(), part, requestBody, imageUploadUnsupportedMediaType)
			}
			imageData, err = readBoundedMultipartPart(r.Context(), part, requestBody, foodimageextraction.MaxImageBytes, imageUploadPayloadTooLarge)
			if err != nil {
				return mealai.ImageRequest{}, err
			}
			if !matchesImageSignature(imageData, imageMIME) {
				return mealai.ImageRequest{}, &imageUploadError{kind: imageUploadUnsupportedMediaType}
			}
		case "locale":
			if localeSeen {
				return mealai.ImageRequest{}, closePartWithError(r.Context(), part, requestBody, imageUploadInvalidRequest)
			}
			localeSeen = true
			localeBytes, readErr := readBoundedMultipartPart(r.Context(), part, requestBody, mealImageLocaleByteLimit, imageUploadInvalidRequest)
			if readErr != nil {
				return mealai.ImageRequest{}, readErr
			}
			locale = string(localeBytes)
		default:
			return mealai.ImageRequest{}, closePartWithError(r.Context(), part, requestBody, imageUploadInvalidRequest)
		}
	}
	if !imageSeen {
		return mealai.ImageRequest{}, &imageUploadError{kind: imageUploadInvalidRequest}
	}
	return mealai.ImageRequest{
		Image:  foodimageextraction.ImageInput{Data: imageData, MIMEType: imageMIME},
		Locale: locale,
	}, nil
}

func parseImagePartMIME(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("image Content-Type is required")
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return "", err
	}
	mediaType = strings.ToLower(mediaType)
	switch mediaType {
	case "image/jpeg", "image/png", "image/webp":
		return mediaType, nil
	default:
		return "", fmt.Errorf("unsupported image media type")
	}
}

func readBoundedMultipartPart(ctx context.Context, part *multipart.Part, requestBody *maxBytesTrackingReadCloser, maximum int, overflowKind imageUploadErrorKind) ([]byte, error) {
	data, readErr := io.ReadAll(io.LimitReader(part, int64(maximum)+1))
	closeErr := part.Close()
	if requestBody.exceeded || isMaxBytesError(readErr) || isMaxBytesError(closeErr) ||
		(len(data) > maximum && overflowKind == imageUploadPayloadTooLarge) {
		return nil, &imageUploadError{kind: imageUploadPayloadTooLarge}
	}
	if isCancellationError(requestBody.terminalErr) {
		return nil, &imageUploadError{kind: imageUploadRequestCanceled}
	}
	if readErr != nil || closeErr != nil {
		return nil, classifyMultipartError(ctx, requestBody, imageUploadInvalidRequest, readErr, closeErr)
	}
	if len(data) > maximum {
		if isRequestCancellation(ctx, requestBody) {
			return nil, &imageUploadError{kind: imageUploadRequestCanceled}
		}
		return nil, &imageUploadError{kind: overflowKind}
	}
	return data, nil
}

func closePartWithError(ctx context.Context, part *multipart.Part, requestBody *maxBytesTrackingReadCloser, fallback imageUploadErrorKind) error {
	return classifyMultipartError(ctx, requestBody, fallback, part.Close())
}

type maxBytesTrackingReadCloser struct {
	io.ReadCloser
	exceeded    bool
	terminalErr error
}

func (reader *maxBytesTrackingReadCloser) Read(buffer []byte) (int, error) {
	read, err := reader.ReadCloser.Read(buffer)
	if isMaxBytesError(err) {
		reader.exceeded = true
	}
	if err != nil {
		reader.terminalErr = err
	}
	return read, err
}

func classifyMultipartError(ctx context.Context, requestBody *maxBytesTrackingReadCloser, fallback imageUploadErrorKind, observedErrors ...error) error {
	if requestBody.exceeded || isMaxBytesError(requestBody.terminalErr) || anyErrorMatches(observedErrors, isMaxBytesError) {
		return &imageUploadError{kind: imageUploadPayloadTooLarge}
	}
	if isRequestCancellation(ctx, requestBody) || anyErrorMatches(observedErrors, isCancellationError) {
		return &imageUploadError{kind: imageUploadRequestCanceled}
	}
	return &imageUploadError{kind: fallback}
}

func isRequestCancellation(ctx context.Context, requestBody *maxBytesTrackingReadCloser) bool {
	return isCancellationError(requestBody.terminalErr) || isCancellationError(ctx.Err())
}

func isCancellationError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func anyErrorMatches(errs []error, matches func(error) bool) bool {
	for _, err := range errs {
		if matches(err) {
			return true
		}
	}
	return false
}

func isMaxBytesError(err error) bool {
	if err == nil {
		return false
	}
	var maximumError *http.MaxBytesError
	return errors.As(err, &maximumError)
}

func matchesImageSignature(data []byte, mediaType string) bool {
	switch mediaType {
	case "image/jpeg":
		return len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff
	case "image/png":
		return len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	case "image/webp":
		return len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP"))
	default:
		return false
	}
}

func imageUploadErrorResponse(err error) (int, string) {
	var uploadError *imageUploadError
	if !errors.As(err, &uploadError) {
		return http.StatusBadRequest, "invalid_request"
	}
	switch uploadError.kind {
	case imageUploadUnsupportedMediaType:
		return http.StatusUnsupportedMediaType, "unsupported_media_type"
	case imageUploadPayloadTooLarge:
		return http.StatusRequestEntityTooLarge, "payload_too_large"
	case imageUploadRequestCanceled:
		return http.StatusRequestTimeout, "request_canceled"
	default:
		return http.StatusBadRequest, "invalid_request"
	}
}

func mapMealImageResult(result mealai.ImageResult) (mealImageInterpretResponse, error) {
	if result.Items == nil {
		return mealImageInterpretResponse{}, fmt.Errorf("nil image meal items")
	}
	items := make([]mealImageInterpretItemResponse, 0, len(result.Items))
	clarifications := 0
	for _, item := range result.Items {
		mapped, err := mapMealImageItem(item)
		if err != nil {
			return mealImageInterpretResponse{}, err
		}
		if item.State == mealai.ItemClarificationRequired {
			clarifications++
		}
		items = append(items, mapped)
	}
	switch result.State {
	case mealai.StateEmpty:
		if len(items) != 0 {
			return mealImageInterpretResponse{}, fmt.Errorf("empty image result has items")
		}
	case mealai.StateReady:
		if len(items) == 0 || clarifications != 0 {
			return mealImageInterpretResponse{}, fmt.Errorf("malformed ready image result")
		}
	case mealai.StateClarificationRequired:
		if len(items) == 0 || clarifications == 0 {
			return mealImageInterpretResponse{}, fmt.Errorf("malformed image clarification result")
		}
	default:
		return mealImageInterpretResponse{}, fmt.Errorf("unknown image meal result state")
	}
	return mealImageInterpretResponse{State: string(result.State), Items: items}, nil
}

func mapMealImageItem(item mealai.ImageItem) (mealImageInterpretItemResponse, error) {
	if !normalizedRuneLength(item.Observation, 1, foodimageextraction.MaxObservationRunes) ||
		!normalizedRuneLength(item.Intent.Query, 2, foodimageextraction.MaxQueryRunes) ||
		item.Intent.Quantity != nil || item.Intent.UnitHint != nil {
		return mealImageInterpretItemResponse{}, fmt.Errorf("malformed image evidence or intent")
	}
	mapped, err := mapMealInterpretItem(mealai.Item{
		Intent: item.Intent, State: item.State, Food: item.Food,
		Selection: item.Selection, Preview: item.Preview, Clarification: item.Clarification,
	})
	if err != nil {
		return mealImageInterpretItemResponse{}, err
	}
	return mealImageInterpretItemResponse{
		Observation: item.Observation, Intent: mapped.Intent, State: mapped.State,
		Food: mapped.Food, Selection: mapped.Selection, Preview: mapped.Preview, Clarification: mapped.Clarification,
	}, nil
}

func normalizedRuneLength(value string, minimum, maximum int) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) == value &&
		utf8.RuneCountInString(value) >= minimum && utf8.RuneCountInString(value) <= maximum
}

func mealImageItemCounts(items []mealai.ImageItem) (ready, clarification int) {
	for _, item := range items {
		switch item.State {
		case mealai.ItemReady:
			ready++
		case mealai.ItemClarificationRequired:
			clarification++
		}
	}
	return ready, clarification
}

type mealImageInterpretResponse struct {
	State string                           `json:"state"`
	Items []mealImageInterpretItemResponse `json:"items"`
}

type mealImageInterpretItemResponse struct {
	Observation   string                     `json:"observation"`
	Intent        mealIntentResponse         `json:"intent"`
	State         string                     `json:"state"`
	Food          *resolvedFoodResponse      `json:"food"`
	Selection     *amountSelectionResponse   `json:"selection"`
	Preview       *nutritionPreviewResponse  `json:"preview"`
	Clarification *mealClarificationResponse `json:"clarification"`
}
