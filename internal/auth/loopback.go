package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxCallbackBodyBytes = 64 << 10

type loginGrantPayload struct {
	Type            string `json:"type"`
	Grant           string `json:"grant"`
	RandomSecretKey string `json:"random_secret_key"`
	ExpireAt        int64  `json:"expire_at,omitempty"`
	Source          string `json:"source"`
	CallbackURL     string `json:"callback_url"`
}

type browserFlow struct {
	loginURL    string
	callbackURL string
	secret      string
	state       string
	source      string
	origin      string
	listener    net.Listener
	server      *http.Server
	payload     chan loginGrantPayload
	serveErr    chan error
	closeOnce   sync.Once
}

func startBrowserFlow(authBaseURL *url.URL, randomReader io.Reader) (*browserFlow, error) {
	secret, err := randomEncoded(randomReader, randomBindingBytes)
	if err != nil {
		return nil, errors.New("生成网页授权绑定信息失败")
	}
	state, err := randomEncoded(randomReader, randomBindingBytes)
	if err != nil {
		return nil, errors.New("生成网页授权状态失败")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("启动本机网页授权回调失败")
	}

	callback := &url.URL{
		Scheme: "http",
		Host:   listener.Addr().String(),
		Path:   callbackPath,
	}
	callbackQuery := callback.Query()
	callbackQuery.Set("state", state)
	callback.RawQuery = callbackQuery.Encode()

	loginURL := *authBaseURL
	loginURL.Path = loginExportPath
	loginURL.RawPath = ""
	loginURL.RawQuery = ""
	loginURL.Fragment = ""
	query := loginURL.Query()
	query.Set("callback", callback.String())
	query.Set("random_secret_key", secret)
	query.Set("source", loginSource)
	loginURL.RawQuery = query.Encode()

	flow := &browserFlow{
		loginURL:    loginURL.String(),
		callbackURL: callback.String(),
		secret:      secret,
		state:       state,
		source:      loginSource,
		origin:      originOf(authBaseURL),
		listener:    listener,
		payload:     make(chan loginGrantPayload, 1),
		serveErr:    make(chan error, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, flow.handleCallback)
	flow.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       15 * time.Second,
	}
	go func() {
		err := flow.server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			flow.serveErr <- err
		}
		close(flow.serveErr)
	}()
	return flow, nil
}

func (f *browserFlow) wait(ctx context.Context) (loginGrantPayload, error) {
	select {
	case payload := <-f.payload:
		return payload, nil
	case err, open := <-f.serveErr:
		if open && err != nil {
			return loginGrantPayload{}, errors.New("本机网页授权回调异常退出")
		}
		return loginGrantPayload{}, errors.New("本机网页授权回调已关闭")
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return loginGrantPayload{}, errors.New("等待网页授权超时，请重新登录")
		}
		return loginGrantPayload{}, ctx.Err()
	}
}

func (f *browserFlow) close() {
	f.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = f.server.Shutdown(ctx)
		_ = f.listener.Close()
	})
}

func (f *browserFlow) handleCallback(writer http.ResponseWriter, request *http.Request) {
	if !f.validRequestTarget(request) {
		http.Error(writer, "invalid callback target", http.StatusBadRequest)
		return
	}
	if !constantTimeEqual(request.Header.Get("Origin"), f.origin) {
		http.Error(writer, "origin not allowed", http.StatusForbidden)
		return
	}
	f.setCORSHeaders(writer.Header())

	if request.Method == http.MethodOptions {
		if !strings.EqualFold(strings.TrimSpace(request.Header.Get("Access-Control-Request-Method")), http.MethodPost) ||
			!allowsContentTypeHeader(request.Header.Get("Access-Control-Request-Headers")) {
			http.Error(writer, "invalid preflight", http.StatusBadRequest)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", "OPTIONS, POST")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		http.Error(writer, "content type must be application/json", http.StatusUnsupportedMediaType)
		return
	}

	reader := http.MaxBytesReader(writer, request.Body, maxCallbackBodyBytes)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	payload := loginGrantPayload{}
	if err := decoder.Decode(&payload); err != nil {
		http.Error(writer, "invalid callback payload", http.StatusBadRequest)
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		http.Error(writer, "invalid callback payload", http.StatusBadRequest)
		return
	}
	if payload.Type != "login_grant" || strings.TrimSpace(payload.Grant) == "" ||
		!constantTimeEqual(payload.RandomSecretKey, f.secret) ||
		!constantTimeEqual(payload.Source, f.source) ||
		!constantTimeEqual(payload.CallbackURL, f.callbackURL) {
		http.Error(writer, "callback binding mismatch", http.StatusBadRequest)
		return
	}
	select {
	case f.payload <- payload:
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"ok":true}`))
	default:
		http.Error(writer, "callback already received", http.StatusConflict)
	}
}

func (f *browserFlow) validRequestTarget(request *http.Request) bool {
	if request.URL.Path != callbackPath || !constantTimeEqual(request.Host, strings.TrimPrefix(f.callbackURLHost(), "//")) {
		return false
	}
	query := request.URL.Query()
	states, ok := query["state"]
	return ok && len(query) == 1 && len(states) == 1 && constantTimeEqual(states[0], f.state)
}

func (f *browserFlow) callbackURLHost() string {
	parsed, err := url.Parse(f.callbackURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

func (f *browserFlow) setCORSHeaders(header http.Header) {
	header.Set("Access-Control-Allow-Origin", f.origin)
	header.Set("Access-Control-Allow-Methods", "POST")
	header.Set("Access-Control-Allow-Headers", "Content-Type")
	header.Set("Access-Control-Allow-Private-Network", "true")
	header.Add("Vary", "Origin")
	header.Add("Vary", "Access-Control-Request-Method")
	header.Add("Vary", "Access-Control-Request-Headers")
}

func allowsContentTypeHeader(value string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), "content-type") {
			return true
		}
	}
	return false
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("unexpected trailing JSON")
	}
	return err
}

func originOf(value *url.URL) string {
	return fmt.Sprintf("%s://%s", strings.ToLower(value.Scheme), value.Host)
}
