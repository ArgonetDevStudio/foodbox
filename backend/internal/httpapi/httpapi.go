package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
	"github.com/LooLookProject/foodbox/backend/internal/web"
)

const (
	defaultMaxUploadBytes  = int64(10 << 20)
	defaultRequestTimeout  = 30 * time.Second
	multipartOverheadLimit = int64(1 << 20)
)

// MenuService is the application boundary used by the HTTP transport. It is
// deliberately limited to operations exposed over HTTP so transport tests do
// not require crawler, OCR, or persistence implementations.
type MenuService interface {
	Ready(context.Context) error
	FindAll(context.Context) ([]domain.Menu, error)
	Today(context.Context, domain.LocalDate) (domain.Menu, error)
	Crawl(context.Context) error
	ParseAndSave(context.Context, string) ([]domain.Menu, error)
}

type NotificationService interface {
	NotifyToday(context.Context) error
}

type Config struct {
	AdminToken     string
	StaticDir      string
	TempDir        string
	Location       *time.Location
	MaxUploadSize  int64
	RequestTimeout time.Duration
	Now            func() time.Time
	Logger         *slog.Logger
}

type handler struct {
	menus         MenuService
	notifications NotificationService
	adminToken    string
	tempDir       string
	location      *time.Location
	maxUploadSize int64
	now           func() time.Time
	logger        *slog.Logger
}

// NewHandler builds the complete application handler. API and readiness paths
// are always dispatched before the optional Svelte SPA fallback.
func NewHandler(config Config, menus MenuService, notifications NotificationService) (http.Handler, error) {
	if menus == nil {
		return nil, errors.New("menu service is required")
	}
	if notifications == nil {
		return nil, errors.New("notification service is required")
	}

	if config.Location == nil {
		location, err := time.LoadLocation("Asia/Seoul")
		if err != nil {
			return nil, fmt.Errorf("load Asia/Seoul timezone: %w", err)
		}
		config.Location = location
	}
	if config.MaxUploadSize <= 0 {
		config.MaxUploadSize = defaultMaxUploadBytes
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = defaultRequestTimeout
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	h := &handler{
		menus:         menus,
		notifications: notifications,
		adminToken:    config.AdminToken,
		tempDir:       config.TempDir,
		location:      config.Location,
		maxUploadSize: config.MaxUploadSize,
		now:           config.Now,
		logger:        config.Logger,
	}

	api := http.NewServeMux()
	api.HandleFunc("/healthz", allowMethods([]string{http.MethodGet}, h.health))
	api.HandleFunc("/api/menu", allowMethods([]string{http.MethodGet}, h.findAll))
	api.HandleFunc("/api/menu/today", allowMethods([]string{http.MethodGet}, h.today))
	api.HandleFunc("/api/upload", allowMethods([]string{http.MethodPost}, h.authorize(h.upload)))
	api.HandleFunc("/api/crawl", allowMethods([]string{http.MethodPost}, h.authorize(h.crawl)))
	api.HandleFunc("/api/slack/notify", allowMethods([]string{http.MethodPost}, h.authorize(h.notify)))
	api.HandleFunc("/", h.notFound)

	var static http.Handler = http.NotFoundHandler()
	if config.StaticDir != "" {
		static = web.New(config.StaticDir)
	}

	router := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if isReservedPath(request.URL.Path) {
			api.ServeHTTP(response, request)
			return
		}
		static.ServeHTTP(response, request)
	})

	timeoutBody, _ := json.Marshal(envelope{
		Status: http.StatusServiceUnavailable,
		Error: &errorResponse{
			ErrorCode: "REQUEST_TIMEOUT",
			Message:   "request timed out",
		},
	})
	recovered := h.recoverPanics(router)
	timed := http.TimeoutHandler(recovered, config.RequestTimeout, string(timeoutBody))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		// TimeoutHandler writes its own response. Set the API content type before
		// entering it so timeout responses retain the JSON contract too.
		if isReservedPath(request.URL.Path) {
			response.Header().Set("Content-Type", "application/json; charset=utf-8")
		}
		timed.ServeHTTP(response, request)
	}), nil
}

func (h *handler) health(response http.ResponseWriter, request *http.Request) {
	if err := h.menus.Ready(request.Context()); err != nil {
		h.writeError(response, http.StatusServiceUnavailable, "NOT_READY", "database is not ready")
		return
	}
	h.writeSuccess(response, http.StatusOK, map[string]bool{"ready": true})
}

func (h *handler) findAll(response http.ResponseWriter, request *http.Request) {
	menus, err := h.menus.FindAll(request.Context())
	if err != nil {
		h.writeServiceError(response, err)
		return
	}
	h.writeSuccess(response, http.StatusOK, menuResponses(menus))
}

func (h *handler) today(response http.ResponseWriter, request *http.Request) {
	today := domain.FromTime(h.now().In(h.location))
	menu, err := h.menus.Today(request.Context(), today)
	if err != nil {
		h.writeServiceError(response, err)
		return
	}
	h.writeSuccess(response, http.StatusOK, newMenuResponse(menu))
}

func (h *handler) crawl(response http.ResponseWriter, request *http.Request) {
	if err := h.menus.Crawl(request.Context()); err != nil {
		h.writeServiceError(response, err)
		return
	}
	h.writeSuccess(response, http.StatusOK, "ok")
}

func (h *handler) notify(response http.ResponseWriter, request *http.Request) {
	if err := h.notifications.NotifyToday(request.Context()); err != nil {
		h.writeServiceError(response, err)
		return
	}
	h.writeSuccess(response, http.StatusOK, true)
}

func (h *handler) upload(response http.ResponseWriter, request *http.Request) {
	path, uploadError := h.saveUpload(response, request)
	if uploadError != nil {
		h.writeError(response, uploadError.status, uploadError.code, uploadError.message)
		return
	}
	defer func() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			h.logger.Warn("remove upload temporary file", "error", err)
		}
	}()

	menus, err := h.menus.ParseAndSave(request.Context(), path)
	if err != nil {
		h.writeServiceError(response, err)
		return
	}
	h.writeSuccess(response, http.StatusOK, menuResponses(menus))
}

func (h *handler) saveUpload(response http.ResponseWriter, request *http.Request) (string, *requestError) {
	request.Body = http.MaxBytesReader(response, request.Body, h.maxUploadSize+multipartOverheadLimit)
	reader, err := request.MultipartReader()
	if err != nil {
		return "", &requestError{
			status:  http.StatusUnsupportedMediaType,
			code:    "UNSUPPORTED_MEDIA_TYPE",
			message: "content type must be multipart/form-data",
		}
	}

	var uploadPath string
	cleanup := func() {
		if uploadPath != "" {
			_ = os.Remove(uploadPath)
		}
	}
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			cleanup()
			return "", multipartReadError(nextErr)
		}

		if part.FormName() != "file" || part.FileName() == "" {
			_, copyErr := io.Copy(io.Discard, part)
			_ = part.Close()
			if copyErr != nil {
				cleanup()
				return "", multipartReadError(copyErr)
			}
			continue
		}
		if uploadPath != "" {
			_ = part.Close()
			cleanup()
			return "", &requestError{
				status:  http.StatusBadRequest,
				code:    "INVALID_UPLOAD",
				message: "multipart request must contain exactly one file field",
			}
		}

		temporary, createErr := os.CreateTemp(h.tempDir, "foodbox-upload-*")
		if createErr != nil {
			_ = part.Close()
			return "", &requestError{
				status:  http.StatusInternalServerError,
				code:    "UPLOAD_FAILED",
				message: "could not create upload temporary file",
			}
		}
		uploadPath = temporary.Name()
		written, copyErr := io.Copy(temporary, io.LimitReader(part, h.maxUploadSize+1))
		closeErr := temporary.Close()
		_ = part.Close()
		if written > h.maxUploadSize {
			cleanup()
			return "", uploadTooLarge(h.maxUploadSize)
		}
		if copyErr != nil {
			cleanup()
			return "", multipartReadError(copyErr)
		}
		if closeErr != nil {
			cleanup()
			return "", &requestError{
				status:  http.StatusInternalServerError,
				code:    "UPLOAD_FAILED",
				message: "could not save uploaded file",
			}
		}
	}

	if uploadPath == "" {
		return "", &requestError{
			status:  http.StatusBadRequest,
			code:    "MISSING_FILE",
			message: "multipart field file is required",
		}
	}
	return uploadPath, nil
}

func multipartReadError(err error) *requestError {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return uploadTooLarge(tooLarge.Limit)
	}
	return &requestError{
		status:  http.StatusBadRequest,
		code:    "INVALID_MULTIPART",
		message: "could not read multipart request",
	}
}

func uploadTooLarge(limit int64) *requestError {
	return &requestError{
		status:  http.StatusRequestEntityTooLarge,
		code:    "UPLOAD_TOO_LARGE",
		message: fmt.Sprintf("uploaded file exceeds %d bytes", limit),
	}
}

func (h *handler) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if h.adminToken == "" {
			h.writeError(response, http.StatusServiceUnavailable, "ADMIN_DISABLED", "admin endpoints are disabled")
			return
		}

		bearer := bearerToken(request.Header.Get("Authorization"))
		headerToken := request.Header.Get("X-Admin-Token")
		bearerMatches := secureTokenEqual(bearer, h.adminToken)
		headerMatches := secureTokenEqual(headerToken, h.adminToken)
		if !bearerMatches && !headerMatches {
			response.Header().Set("WWW-Authenticate", "Bearer")
			h.writeError(response, http.StatusUnauthorized, "UNAUTHORIZED", "valid admin token is required")
			return
		}
		next(response, request)
	}
}

func bearerToken(value string) string {
	space := strings.IndexByte(value, ' ')
	if space < 0 || !strings.EqualFold(value[:space], "Bearer") {
		return ""
	}
	return strings.TrimSpace(value[space+1:])
}

func secureTokenEqual(candidate, expected string) bool {
	candidateHash := sha256.Sum256([]byte(candidate))
	expectedHash := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(candidateHash[:], expectedHash[:]) == 1
}

func (h *handler) writeServiceError(response http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "INTERNAL_SERVER_ERROR"
	message := "internal server error"

	var publicError interface {
		HTTPStatus() int
		ErrorCode() string
	}
	if errors.As(err, &publicError) &&
		publicError.HTTPStatus() >= 400 && publicError.HTTPStatus() <= 599 &&
		publicError.ErrorCode() != "" {
		status = publicError.HTTPStatus()
		code = publicError.ErrorCode()
		message = err.Error()
	}
	if errors.Is(err, context.Canceled) {
		status = http.StatusRequestTimeout
		code = "REQUEST_CANCELED"
		message = "request canceled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		status = http.StatusGatewayTimeout
		code = "REQUEST_TIMEOUT"
		message = "request timed out"
	}

	if status >= http.StatusInternalServerError {
		h.logger.Error("HTTP service request failed", "error", err, "status", status)
	}
	h.writeError(response, status, code, message)
}

func (h *handler) notFound(response http.ResponseWriter, _ *http.Request) {
	h.writeError(response, http.StatusNotFound, "NOT_FOUND", "resource not found")
}

func (h *handler) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				h.logger.Error("panic in HTTP handler", "panic", recovered)
				h.writeError(response, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error")
			}
		}()
		next.ServeHTTP(response, request)
	})
}

func (h *handler) writeSuccess(response http.ResponseWriter, status int, data any) {
	writeJSON(response, status, envelope{Status: status, Data: data})
}

func (h *handler) writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, envelope{
		Status: status,
		Error: &errorResponse{
			ErrorCode: code,
			Message:   message,
		},
	})
}

func writeJSON(response http.ResponseWriter, status int, value envelope) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func allowMethods(methods []string, next http.HandlerFunc) http.HandlerFunc {
	allowed := strings.Join(methods, ", ")
	return func(response http.ResponseWriter, request *http.Request) {
		for _, method := range methods {
			if request.Method == method {
				next(response, request)
				return
			}
		}
		response.Header().Set("Allow", allowed)
		writeJSON(response, http.StatusMethodNotAllowed, envelope{
			Status: http.StatusMethodNotAllowed,
			Error: &errorResponse{
				ErrorCode: "HttpRequestMethodNotSupportedException",
				Message:   fmt.Sprintf("Request method '%s' is not supported", request.Method),
			},
		})
	}
}

func isReservedPath(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/") ||
		path == "/healthz" || strings.HasPrefix(path, "/healthz/")
}

func menuResponses(menus []domain.Menu) []menuResponse {
	responses := make([]menuResponse, len(menus))
	for index, menu := range menus {
		responses[index] = newMenuResponse(menu)
	}
	return responses
}

func newMenuResponse(menu domain.Menu) menuResponse {
	items := append([]string(nil), menu.Menus...)
	if items == nil {
		items = []string{}
	}
	return menuResponse{
		Date:    menu.Date.String(),
		Menus:   items,
		IsValid: menu.Valid,
	}
}

type envelope struct {
	Status int            `json:"status"`
	Error  *errorResponse `json:"error"`
	Data   any            `json:"data"`
}

type errorResponse struct {
	ErrorCode string `json:"errorCode"`
	Message   string `json:"message"`
}

type menuResponse struct {
	Date    string   `json:"date"`
	Menus   []string `json:"menus"`
	IsValid bool     `json:"isValid"`
}

type requestError struct {
	status  int
	code    string
	message string
}
