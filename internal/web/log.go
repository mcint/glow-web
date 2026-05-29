package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// LogLevel controls per-request access logging.
type LogLevel int

const (
	LogOff  LogLevel = iota // no per-request output
	LogDot                  // one char per request: "|" for a primary page view, "." otherwise — a heartbeat that shows page loads punctuating their sub-requests, e.g. "|.....|..|....."
	LogInfo                 // one line per request: client method path status bytes duration
)

// isPrimaryRequest reports whether a request is a top-level page view (a
// navigation the user initiated) rather than an internal helper fetch. The
// internal endpoints live under the "/_/" namespace (e.g. /_/files, /_/render);
// everything else — index, rendered/text file views, raw/download — is primary.
// The URL prefix (when mounted behind a proxy) is trimmed first so the "/_/"
// test sees the internal path.
func isPrimaryRequest(r *http.Request, urlPrefix string) bool {
	path := r.URL.Path
	if urlPrefix != "" {
		path = strings.TrimPrefix(path, urlPrefix)
	}
	return !strings.HasPrefix(path, "/_/")
}

// ParseLogLevel maps the --log-level CLI value to a LogLevel. Empty / "off" /
// "none" / "silent" all map to LogOff so the default and explicit-disable
// invocations both produce the same silent server we shipped before this knob.
func ParseLogLevel(s string) (LogLevel, error) {
	switch s {
	case "", "off", "none", "silent":
		return LogOff, nil
	case "dot":
		return LogDot, nil
	case "info":
		return LogInfo, nil
	default:
		return LogOff, fmt.Errorf("unknown log level %q (try off|dot|info)", s)
	}
}

// respRecorder wraps an http.ResponseWriter to capture the status code and
// bytes written so the info-format line can report them.
type respRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *respRecorder) WriteHeader(c int) {
	if r.status == 0 {
		r.status = c
	}
	r.ResponseWriter.WriteHeader(c)
}

func (r *respRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// logMiddleware wraps next with per-request logging at the configured level.
// LogOff returns next unchanged so there's zero overhead when logging is
// disabled (the default).
func logMiddleware(level LogLevel, out io.Writer, urlPrefix string, next http.Handler) http.Handler {
	if level == LogOff {
		return next
	}
	if out == nil {
		out = os.Stderr
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch level {
		case LogDot:
			next.ServeHTTP(w, r)
			mark := "."
			if isPrimaryRequest(r, urlPrefix) {
				mark = "|"
			}
			_, _ = fmt.Fprint(out, mark)
		case LogInfo:
			start := time.Now()
			rec := &respRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			client := r.RemoteAddr
			if client == "" {
				client = "-"
			}
			_, _ = fmt.Fprintf(out, "%s %s %s %d %d %s\n",
				client, r.Method, r.URL.RequestURI(), status, rec.bytes,
				time.Since(start).Round(time.Microsecond))
		}
	})
}
