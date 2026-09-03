package cli

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/tokenflux/tf-cli/internal/buildinfo"
	"github.com/tokenflux/tf-cli/internal/config"
	"github.com/tokenflux/tf-cli/internal/ui"
)

const (
	webImportProtocol        = 1
	webImportPortFirst       = 43110
	webImportPortLast        = 43119
	webImportMaxBody         = 32 << 10
	webImportSessionBytes    = 16
	webImportChallengeMin    = 16
	webImportChallengeMax    = 128
	webImportProofHeader     = "X-TF-Session-Proof"
	webImportBodyReadTimeout = 10 * time.Second
	webImportTimeout         = 10 * time.Minute
)

type webImportRequest struct {
	Version   int    `json:"version"`
	Key       string `json:"key"`
	Host      string `json:"host"`
	KeyName   string `json:"key_name,omitempty"`
	GroupID   int64  `json:"group_id,omitempty"`
	GroupName string `json:"group_name,omitempty"`
	Origin    string `json:"-"`
	Verified  bool   `json:"-"`
}

type webImportDecision struct {
	Accepted bool
	Error    string
}

type webImportEvent struct {
	Request   webImportRequest
	Reply     chan webImportDecision
	Responded chan struct{}
}

type webImportHandler struct {
	host          string
	origin        string
	port          int
	sessionSecret []byte
	events        chan<- webImportEvent
	busy          atomic.Bool
}

// waitForWebImport opens a short-lived loopback server and waits for one
// terminal-confirmed request. It never writes credentials itself: the caller
// sends the returned key through the normal login validation and save path.
func waitForWebImport(c *Context, host, credentialsPath, targetName string,
	existing *config.Credential, fixedTarget bool) (webImportRequest, error) {
	host = normalizeHost(host)
	origin, err := webOrigin(host)
	if err != nil {
		return webImportRequest{}, ui.Errf(ui.CodeUsage,
			fmt.Sprintf(c.UI.T("网关地址无效：%s", "invalid gateway address: %s"), host)).WithCause(err)
	}

	listener, port, err := listenWebImport()
	if err != nil {
		return webImportRequest{}, ui.Errf(ui.CodeNetwork,
			c.UI.T("本机网页导入端口都被占用", "all local web-import ports are in use")).WithCause(err)
	}

	sessionSecret := make([]byte, webImportSessionBytes)
	if _, err := rand.Read(sessionSecret); err != nil {
		_ = listener.Close()
		return webImportRequest{}, ui.Errf(ui.CodeInternal,
			c.UI.T("无法建立网页导入会话", "cannot create a web-import session")).WithCause(err)
	}
	events := make(chan webImportEvent)
	server := &http.Server{
		Handler: &webImportHandler{
			host: host, origin: origin, port: port, sessionSecret: sessionSecret, events: events,
		},
		// handleImport applies and then clears a deadline around the body read,
		// before the potentially long terminal confirmation.
		ReadHeaderTimeout: 3 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			serveErr <- err
		}
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			_ = server.Close()
		}
	}()

	sessionURL := webImportSessionURL(host, port, sessionSecret)
	c.UI.Logf("%s", c.UI.Bold(c.UI.T("等待网页导入", "Waiting for web import")))
	c.UI.Logf("  %s http://127.0.0.1:%d", ui.Pad(c.UI.T("监听", "listen"), 8), port)
	c.UI.Logf("  %s %s", ui.Pad(c.UI.T("来源", "origin"), 8), origin)
	c.UI.Logf("  %s %s", ui.Pad(c.UI.T("打开", "open"), 8), sessionURL)
	c.UI.Logf("  %s", c.UI.Dim(c.UI.T("10 分钟内没有请求会自动退出", "exits after 10 minutes without a request")))
	if err := openWebBrowser(sessionURL); err != nil {
		c.UI.Warnf("%s", c.UI.T("无法自动打开浏览器；请打开上方链接",
			"could not open a browser automatically; open the URL above"))
	}

	timer := time.NewTimer(webImportTimeout)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			accepted, confirmErr := confirmWebImport(c, event.Request, credentialsPath,
				targetName, existing, fixedTarget)
			decision := makeWebImportDecision(accepted, confirmErr)
			event.Reply <- decision
			<-event.Responded
			if confirmErr != nil {
				return webImportRequest{}, confirmErr
			}
			if accepted {
				return event.Request, nil
			}
			c.UI.Warnf("%s", c.UI.T("已拒绝该请求，继续等待", "request rejected; still waiting"))
		case err := <-serveErr:
			return webImportRequest{}, ui.Errf(ui.CodeNetwork,
				c.UI.T("网页导入监听已中断", "the web-import listener stopped")).WithCause(err)
		case <-timer.C:
			return webImportRequest{}, ui.Errf(ui.CodeCancelled,
				c.UI.T("等待网页导入超时", "timed out waiting for web import")).
				WithHint("echo $KEY | tf login")
		}
	}
}

func makeWebImportDecision(accepted bool, confirmErr error) webImportDecision {
	if confirmErr != nil {
		return webImportDecision{Error: "cancelled"}
	}
	if !accepted {
		return webImportDecision{Error: "rejected"}
	}
	return webImportDecision{Accepted: true}
}

func listenWebImport() (net.Listener, int, error) {
	var last error
	for port := webImportPortFirst; port <= webImportPortLast; port++ {
		listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return listener, port, nil
		}
		last = err
	}
	return nil, 0, last
}

func webImportSessionURL(host string, port int, secret []byte) string {
	page, _ := url.Parse(host) // host was validated by webOrigin before this call.
	page.Path = strings.TrimRight(page.Path, "/") + "/keys"
	page.RawPath = ""
	page.Fragment = fmt.Sprintf("tfcli=%d.%d.%s", webImportProtocol, port,
		base64.RawURLEncoding.EncodeToString(secret))
	return page.String()
}

func openWebBrowser(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func webImportProof(secret []byte, port int, challenge string) string {
	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "tf-web-import-v1\n%d\n%s", port, challenge)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func webImportBodyProof(secret []byte, port int, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "tf-web-import-v1\n%d\nimport\n", port)
	_, _ = mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validWebImportChallenge(challenge string) bool {
	if len(challenge) < webImportChallengeMin || len(challenge) > webImportChallengeMax {
		return false
	}
	for _, r := range challenge {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	_, err := base64.RawURLEncoding.Strict().DecodeString(challenge)
	return err == nil
}

func confirmWebImport(c *Context, req webImportRequest, credentialsPath, targetName string,
	existing *config.Credential, fixedTarget bool) (bool, error) {
	group := req.GroupName
	if req.GroupID != 0 {
		if group != "" {
			group += " "
		}
		group += fmt.Sprintf("#%d", req.GroupID)
	}
	if group == "" {
		group = c.UI.T("未提供", "not provided")
	}
	key := config.Mask(req.Key)
	if req.KeyName != "" {
		key += fmt.Sprintf("  %q", req.KeyName)
	}

	destination := targetName
	if !fixedTarget {
		destination = c.UI.T("校验后选择", "choose after validation")
	} else if existing != nil && existing.Key != "" && existing.Key != req.Key {
		destination += fmt.Sprintf(c.UI.T("（将覆盖 %s）", " (replaces %s)"), config.Mask(existing.Key))
	}

	c.UI.Logf("%s", c.UI.Bold(c.UI.T("收到网页导入请求", "Web import received")))
	if req.Verified {
		c.UI.Logf("  %s %s", ui.Pad(c.UI.T("验证", "verification"), 8),
			c.UI.T("已验证当前 tf 会话", "current tf session verified"))
	} else {
		c.UI.Warnf("%s", c.UI.T("未验证本机 tf 会话；确认后仍可继续",
			"local tf session is unverified; you may still confirm"))
	}
	c.UI.Logf("  %s %s", ui.Pad(c.UI.T("来源", "origin"), 8), req.Origin)
	c.UI.Logf("  %s %s", ui.Pad(c.UI.T("网关", "gateway"), 8), req.Host)
	c.UI.Logf("  %s %s", ui.Pad(c.UI.T("分组", "group"), 8), group)
	c.UI.Logf("  %s %s", ui.Pad(c.UI.T("Key", "key"), 8), key)
	c.UI.Logf("  %s %s", ui.Pad(c.UI.T("名称", "name"), 8), destination)

	idx, err := c.UI.Select(fmt.Sprintf(c.UI.T("写入 %s？", "Write to %s?"), credentialsPath), []ui.Item{
		{Label: c.UI.T("写入", "write")},
		{Label: c.UI.T("拒绝", "reject")},
	})
	return idx == 0, err
}

func webOrigin(host string) (string, error) {
	u, err := url.Parse(host)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("expected an http(s) URL")
	}
	if u.User != nil || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("URL must not contain user info, query, or fragment")
	}
	scheme := strings.ToLower(u.Scheme)
	hostname := u.Hostname()
	if hostname == "" {
		return "", fmt.Errorf("URL host must not be empty")
	}
	originHost := strings.ToLower(hostname)
	if strings.Contains(originHost, ":") {
		originHost = "[" + originHost + "]"
	}
	port := u.Port()
	if port != "" && !((scheme == "https" && port == "443") || (scheme == "http" && port == "80")) {
		originHost += ":" + port
	}
	return scheme + "://" + originHost, nil
}

func (h *webImportHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/ping" && r.URL.Path != "/import" {
		writeWebJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not_found"})
		return
	}
	if !h.setCORS(w, r) {
		writeWebJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "origin_not_allowed"})
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/ping" {
		if r.Method != http.MethodGet {
			writeWebJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
			return
		}
		data := map[string]any{
			"ok": true, "service": "tf", "protocol": webImportProtocol, "version": buildinfo.Version,
		}
		if challenge := r.URL.Query().Get("challenge"); challenge != "" {
			if !validWebImportChallenge(challenge) || len(h.sessionSecret) == 0 || h.port == 0 {
				writeWebJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_challenge"})
				return
			}
			data["proof"] = webImportProof(h.sessionSecret, h.port, challenge)
		}
		writeWebJSON(w, http.StatusOK, data)
		return
	}
	if r.Method != http.MethodPost {
		writeWebJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
		return
	}
	h.handleImport(w, r)
}

func (h *webImportHandler) setCORS(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") != h.origin {
		return false
	}
	head := w.Header()
	head.Set("Access-Control-Allow-Origin", h.origin)
	head.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	head.Set("Access-Control-Allow-Headers", "Content-Type, "+webImportProofHeader)
	head.Set("Access-Control-Allow-Private-Network", "true")
	head.Set("Access-Control-Max-Age", "600")
	head.Set("Cache-Control", "no-store")
	head.Set("Vary", "Origin")
	return true
}

func (h *webImportHandler) handleImport(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeWebJSON(w, http.StatusUnsupportedMediaType, map[string]any{"ok": false, "error": "content_type"})
		return
	}
	// Claim the one available import transaction before reading the body.
	// Otherwise many slow local clients can all occupy handler goroutines
	// before any of them reaches the confirmation-stage busy check.
	if !h.busy.CompareAndSwap(false, true) {
		writeWebJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "busy"})
		return
	}
	defer h.busy.Store(false)

	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(webImportBodyReadTimeout))
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, webImportMaxBody))
	_ = controller.SetReadDeadline(time.Time{})
	if err != nil {
		writeWebJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_json"})
		return
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var req webImportRequest
	if err := dec.Decode(&req); err != nil {
		writeWebJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_json"})
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeWebJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_json"})
		return
	}
	if err := h.validate(&req); err != nil {
		writeWebJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	expectedProof := webImportBodyProof(h.sessionSecret, h.port, body)
	providedProof := r.Header.Get(webImportProofHeader)
	req.Verified = len(h.sessionSecret) > 0 && h.port != 0 &&
		hmac.Equal([]byte(providedProof), []byte(expectedProof))

	event := webImportEvent{
		Request: req, Reply: make(chan webImportDecision, 1), Responded: make(chan struct{}),
	}
	select {
	case h.events <- event:
	case <-r.Context().Done():
		return
	}
	defer close(event.Responded)

	select {
	case decision := <-event.Reply:
		if !decision.Accepted {
			writeWebJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": decision.Error})
			return
		}
		writeWebJSON(w, http.StatusAccepted, map[string]any{"ok": true, "status": "accepted"})
	case <-r.Context().Done():
	}
}

func (h *webImportHandler) validate(req *webImportRequest) error {
	req.Key = strings.TrimSpace(req.Key)
	req.Host = normalizeHost(req.Host)
	if req.Version != webImportProtocol {
		return fmt.Errorf("unsupported_protocol")
	}
	if req.Key == "" || len(req.Key) > 16<<10 || strings.IndexFunc(req.Key, func(r rune) bool {
		return r < 0x21 || r > 0x7e
	}) >= 0 {
		return fmt.Errorf("invalid_key")
	}
	origin, err := webOrigin(req.Host)
	if err != nil || origin != h.origin {
		return fmt.Errorf("host_mismatch")
	}
	if req.GroupID < 0 || !safeImportText(req.KeyName, 256) || !safeImportText(req.GroupName, 256) {
		return fmt.Errorf("invalid_metadata")
	}
	req.Host = h.host
	req.Origin = h.origin
	return nil
}

func safeImportText(s string, max int) bool {
	if len(s) > max {
		return false
	}
	// Format controls include bidi overrides and zero-width characters;
	// line and paragraph separators can also restructure the confirmation.
	return strings.IndexFunc(s, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Cs, unicode.Zl, unicode.Zp)
	}) < 0
}

func writeWebJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
