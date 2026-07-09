package obs

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	reg           *prometheus.Registry
	requests      *prometheus.CounterVec
	duration      *prometheus.HistogramVec
	CacheHit      prometheus.Counter
	CacheMiss     prometheus.Counter
	UploadBytes   prometheus.Counter
	DownloadBytes prometheus.Counter
}

func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		reg: reg,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total", Help: "HTTP requests by method/route/status",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "request_duration_seconds", Help: "HTTP request duration",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		CacheHit:      prometheus.NewCounter(prometheus.CounterOpts{Name: "cache_hits_total"}),
		CacheMiss:     prometheus.NewCounter(prometheus.CounterOpts{Name: "cache_misses_total"}),
		UploadBytes:   prometheus.NewCounter(prometheus.CounterOpts{Name: "cache_upload_bytes_total"}),
		DownloadBytes: prometheus.NewCounter(prometheus.CounterOpts{Name: "cache_download_bytes_total"}),
	}
	reg.MustRegister(m.requests, m.duration, m.CacheHit, m.CacheMiss, m.UploadBytes, m.DownloadBytes)
	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// Middleware records count + duration. Uses chi RoutePattern to avoid label cardinality blowup.
func (m *Metrics) Middleware(routePattern func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(sw, r)
			route := routePattern(r)
			m.requests.WithLabelValues(r.Method, route, strconv.Itoa(sw.status)).Inc()
			m.duration.WithLabelValues(route).Observe(time.Since(start).Seconds())
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// ReadFrom forwards to the wrapped ResponseWriter's ReadFrom when it has one
// (http.response does, enabling the kernel sendfile/splice fast path for
// *os.File sources). Embedding a bare http.ResponseWriter interface value
// does NOT promote this method — promotion is based on the field's static
// (interface) type, not whatever concrete writer sits behind it at runtime —
// so this passthrough has to be explicit.
func (s *statusWriter) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := s.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(writerOnly{s.ResponseWriter}, r)
}

// writerOnly strips every interface but io.Writer so the fallback io.Copy
// above can't recurse back into statusWriter.ReadFrom.
type writerOnly struct{ io.Writer }
