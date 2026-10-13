// Command loadtest runs N compression jobs against the SmallPict API
// (round-robin over the images in a folder) to measure whether SmallPict
// can handle that load.
//
// Every request to the API host (optimize, status polling, and automatic SDK
// retries) is counted and throttled by a global rate limiter (-rpm), so the
// account's "hits per minute" limit (sandbox / subscription) is not exceeded.
//
// Examples:
//
//	go run ./cmd/loadtest -dir ../images -total 10 -rpm 30 -max-file-mb 5	# smoke test (sandbox)
//	go run ./cmd/loadtest -dir ../images -total 1500 -rpm 120 -workers 10 -max-file-mb 5	# full test (subscription)
package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"smallpict-go/config"

	"github.com/smallpict/smallpict-go"
)

// ---------------------------------------------------------------------------
// Rate limiter + instrumented transport
// ---------------------------------------------------------------------------

// spacingLimiter divides time into evenly spaced slots (60s / rpm).
// This spreads requests out evenly, with no burst at the start of a minute.
type spacingLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func newSpacingLimiter(rpm int) *spacingLimiter {
	return &spacingLimiter{interval: time.Minute / time.Duration(rpm)}
}

func (l *spacingLimiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	slot := l.next
	l.next = l.next.Add(l.interval)
	l.mu.Unlock()

	d := time.Until(slot)
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// apiTransport throttles & counts every request to the SmallPict API host.
// Requests to other hosts (e.g. presigned S3 URLs) pass through unthrottled.
type apiTransport struct {
	base    http.RoundTripper
	lim     *spacingLimiter
	apiHost string

	mu       sync.Mutex
	statuses map[int]int64
	calls    atomic.Int64
	netErrs  atomic.Int64
}

func (t *apiTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != t.apiHost {
		return t.base.RoundTrip(req)
	}
	if err := t.lim.Wait(req.Context()); err != nil {
		return nil, err
	}
	t.calls.Add(1)
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		t.netErrs.Add(1)
		return nil, err
	}
	t.mu.Lock()
	t.statuses[resp.StatusCode]++
	t.mu.Unlock()

	// The API returns the `file_too_large` error as HTTP 500, so the SDK would
	// retry in vain (wasting hits/minute). Rewrite it to 413 so it is not
	// retried. The stats above still record the server's original status.
	if resp.StatusCode >= 500 && resp.Body != nil {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(b))
		if bytes.Contains(b, []byte("file_too_large")) {
			resp.StatusCode = http.StatusRequestEntityTooLarge
			resp.Status = "413 Request Entity Too Large"
		}
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// Data
// ---------------------------------------------------------------------------

type sample struct {
	name string
	data []byte
	mime string
}

type result struct {
	seq      int
	file     string
	ok       bool
	errKind  string
	errMsg   string
	latency  time.Duration
	origSize int64
	compSize int64 // -1 if unknown
	format   string
}

// loadSamples reads images from dir. Files larger than maxBytes
// (when maxBytes > 0) are skipped so they don't hit the plan's upload limit.
func loadSamples(dir string, maxBytes int64) ([]sample, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".jpg", ".jpeg", ".png", ".webp", ".avif":
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("no images (jpg/png/webp/avif) found in %s", dir)
	}

	out := make([]sample, 0, len(names))
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		if maxBytes > 0 && int64(len(b)) > maxBytes {
			fmt.Printf("Skipped      : %s (%s > limit %s)\n", n, human(int64(len(b))), human(maxBytes))
			continue
		}
		out = append(out, sample{name: n, data: b, mime: http.DetectContentType(b)})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("all images in %s are larger than the limit %s", dir, human(maxBytes))
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Worker
// ---------------------------------------------------------------------------

type runner struct {
	client       *smallpict.Client
	plain        *http.Client // for S3 PUT & download verification (no limiter)
	cfg          *config.Config
	samples      []sample
	pollInterval time.Duration
	jobTimeout   time.Duration
	verify       bool
	stopOnFatal  bool
	cancel       context.CancelFunc
}

// errJobFailed marks a job that the API accepted but failed to process on the
// server side (e.g. "unsupported image format").
var errJobFailed = errors.New("job failed")

func classify(err error) string {
	var apiErr *smallpict.Error
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errJobFailed):
		return "job_failed"
	case errors.As(err, &apiErr) && (apiErr.Code == "file_too_large" || apiErr.StatusCode == http.StatusRequestEntityTooLarge):
		return "file_too_large"
	case smallpict.IsRateLimit(err):
		return "rate_limit_429"
	case smallpict.IsQuotaExceeded(err):
		return "quota_402"
	case smallpict.IsUnauthorized(err):
		return "unauthorized_401"
	case smallpict.IsPermissionDenied(err):
		return "forbidden_403"
	case smallpict.IsValidationError(err):
		return "validation_400"
	case smallpict.IsServer(err):
		return "server_5xx"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "other"
	}
}

func (r *runner) process(parent context.Context, seq int) result {
	s := r.samples[seq%len(r.samples)]
	res := result{seq: seq, file: s.name, origSize: int64(len(s.data)), compSize: -1}

	ext := filepath.Ext(s.name)
	base := strings.TrimSuffix(s.name, ext)
	filename := fmt.Sprintf("%s_lt%d_%d%s", base, time.Now().UnixNano(), seq, ext)

	ctx, cancel := context.WithTimeout(parent, r.jobTimeout)
	defer cancel()

	start := time.Now()
	fail := func(err error) result {
		res.latency = time.Since(start)
		res.compSize = -1 // compressed size is meaningless for failed requests
		res.errKind = classify(err)
		res.errMsg = err.Error()
		if r.stopOnFatal && (res.errKind == "unauthorized_401" || res.errKind == "quota_402" || res.errKind == "forbidden_403") {
			fmt.Fprintf(os.Stderr, "\n⛔ fatal error (%s): stopping the test. %s\n", res.errKind, res.errMsg)
			r.cancel()
		}
		return res
	}

	opts := &smallpict.OptimizeOptions{
		Filename: filename,
		MIMEType: s.mime,
		Format:   r.cfg.SmallPictFormat,
		Quality:  r.cfg.SmallPictQuality,
		MaxWidth: r.cfg.SmallPictMaxWidth,
	}

	out, err := r.client.Optimize(ctx, bytes.NewReader(s.data), opts)
	if err != nil {
		return fail(err)
	}
	res.format = out.Format

	if out.UploadURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, out.UploadURL, bytes.NewReader(s.data))
		if err != nil {
			return fail(err)
		}
		req.Header.Set("Content-Type", s.mime)
		resp, err := r.plain.Do(req)
		if err != nil {
			return fail(fmt.Errorf("upload s3: %w", err))
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fail(fmt.Errorf("upload s3 status %d", resp.StatusCode))
		}
	}

	url := out.URL
	if out.CompressedSize > 0 {
		res.compSize = out.CompressedSize
	}

	if out.JobID != "" && (out.Status == "pending" || out.Status == "processing" || url == "") {
		done := false
		for !done {
			select {
			case <-ctx.Done():
				return fail(ctx.Err())
			case <-time.After(r.pollInterval):
			}
			st, err := r.client.GetJobStatus(ctx, out.JobID)
			if err != nil {
				if smallpict.IsRateLimit(err) || smallpict.IsServer(err) {
					continue
				}
				return fail(err)
			}
			switch strings.ToLower(st.Status) {
			case "succeeded", "completed", "success", "ready", "done":
				if st.URL != "" {
					url = st.URL
				}
				if st.BytesSaved != nil && *st.BytesSaved > 0 {
					res.compSize = res.origSize - *st.BytesSaved
				}
				if st.Format != "" {
					res.format = st.Format
				}
				done = true
			case "failed", "error", "canceled", "cancelled":
				return fail(fmt.Errorf("%w: job %s status=%s err=%v", errJobFailed, out.JobID, st.Status, st.Error))
			}
		}
	}

	if url == "" {
		return fail(errors.New("no optimized result URL"))
	}

	if r.verify {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fail(err)
		}
		resp, err := r.plain.Do(req)
		if err != nil {
			return fail(fmt.Errorf("download: %w", err))
		}
		n, err := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err != nil {
			return fail(fmt.Errorf("download: %w", err))
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fail(fmt.Errorf("download status %d", resp.StatusCode))
		}
		res.compSize = n
	}

	res.ok = true
	res.latency = time.Since(start)
	return res
}

// ---------------------------------------------------------------------------
// Reporting
// ---------------------------------------------------------------------------

func pct(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

func human(n int64) string {
	const k = 1024.0
	f := float64(n)
	switch {
	case f >= k*k*k:
		return fmt.Sprintf("%.2f GB", f/(k*k*k))
	case f >= k*k:
		return fmt.Sprintf("%.2f MB", f/(k*k))
	case f >= k:
		return fmt.Sprintf("%.1f KB", f/k)
	}
	return fmt.Sprintf("%d B", n)
}

func printSummary(results []result, elapsed time.Duration, tr *apiTransport, planned int, targetBytes int64, rpm int) {
	var okN, failN int
	errKinds := map[string]int{}
	var lats []time.Duration
	var origSum, compSum, compKnown int64
	var underTarget int
	type agg struct {
		n, ok int
		comp  int64
		known int
		max   int64
	}
	perFile := map[string]*agg{}

	for _, r := range results {
		a := perFile[r.file]
		if a == nil {
			a = &agg{}
			perFile[r.file] = a
		}
		a.n++
		if r.ok {
			okN++
			a.ok++
			lats = append(lats, r.latency)
			origSum += r.origSize
			if r.compSize >= 0 {
				compSum += r.compSize
				compKnown++
				a.comp += r.compSize
				a.known++
				if r.compSize > a.max {
					a.max = r.compSize
				}
				if r.compSize <= targetBytes {
					underTarget++
				}
			}
		} else {
			failN++
			errKinds[r.errKind]++
		}
	}
	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })

	fmt.Println("\n================ LOAD TEST RESULTS ================")
	fmt.Printf("Planned        : %d\n", planned)
	fmt.Printf("Completed      : %d  (success %d, failed %d)\n", len(results), okN, failN)
	if len(results) > 0 {
		fmt.Printf("Success rate   : %.2f%%\n", float64(okN)/float64(len(results))*100)
	}
	fmt.Printf("Total duration : %s\n", elapsed.Round(time.Second))
	if elapsed > 0 {
		fmt.Printf("Throughput     : %.1f images/minute\n", float64(len(results))/elapsed.Minutes())
	}

	tr.mu.Lock()
	fmt.Printf("\nAPI calls      : %d total  (limit set to %d/min, actual average %.1f/min)\n",
		tr.calls.Load(), rpm, float64(tr.calls.Load())/maxF(elapsed.Minutes(), 1.0/60))
	var codes []int
	for c := range tr.statuses {
		codes = append(codes, c)
	}
	sort.Ints(codes)
	for _, c := range codes {
		fmt.Printf("  HTTP %d       : %d\n", c, tr.statuses[c])
	}
	tr.mu.Unlock()
	if n := tr.netErrs.Load(); n > 0 {
		fmt.Printf("  network error: %d\n", n)
	}

	if len(lats) > 0 {
		fmt.Printf("\nEnd-to-end latency (success): p50=%s  p95=%s  p99=%s  max=%s\n",
			pct(lats, 0.50).Round(time.Millisecond), pct(lats, 0.95).Round(time.Millisecond),
			pct(lats, 0.99).Round(time.Millisecond), lats[len(lats)-1].Round(time.Millisecond))
	}

	if okN > 0 {
		fmt.Printf("\nOriginal data processed : %s\n", human(origSum))
		if compKnown > 0 {
			fmt.Printf("Compression result      : average %s per image (%d images measured)\n", human(compSum/compKnown), compKnown)
			fmt.Printf("≤ %s            : %d / %d (%.1f%%)\n", human(targetBytes), underTarget, compKnown, float64(underTarget)/float64(compKnown)*100)
		} else {
			fmt.Println("Compressed size is not reported by the API (run with -verify to measure it).")
		}
	}

	if len(errKinds) > 0 {
		fmt.Println("\nErrors:")
		var ks []string
		for k := range errKinds {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			fmt.Printf("  %-18s %d\n", k, errKinds[k])
		}
		shown := 0
		for _, r := range results {
			if !r.ok && shown < 3 {
				fmt.Printf("  example: #%d %s -> %s\n", r.seq, r.file, r.errMsg)
				shown++
			}
		}
	}

	fmt.Println("\nPer file:")
	var files []string
	for f := range perFile {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		a := perFile[f]
		avg := "n/a"
		if a.known > 0 {
			avg = human(a.comp / int64(a.known))
		}
		mx := "n/a"
		if a.known > 0 {
			mx = human(a.max)
		}
		fmt.Printf("  %-16s ok %d/%d  avg %s  max %s\n", f, a.ok, a.n, avg, mx)
	}
	fmt.Println("=================================================")
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func writeCSV(path string, results []result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"seq", "file", "ok", "error_kind", "latency_ms", "orig_bytes", "comp_bytes", "format", "error"})
	sorted := append([]result(nil), results...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].seq < sorted[j].seq })
	for _, r := range sorted {
		_ = w.Write([]string{
			strconv.Itoa(r.seq), r.file, strconv.FormatBool(r.ok), r.errKind,
			strconv.FormatInt(r.latency.Milliseconds(), 10),
			strconv.FormatInt(r.origSize, 10), strconv.FormatInt(r.compSize, 10),
			r.format, r.errMsg,
		})
	}
	return w.Error()
}

// quotaInfo mirrors the fields of GET /v1/quota that matter for this test.
// The SDK's QuotaResponse only reads the legacy `quota_limit` / `bytes_used`
// fields, while this API reports the plan quota in the *_mb fields, so the raw
// response is parsed here instead.
type quotaInfo struct {
	Plan              string  `json:"plan"`
	QuotaLimitMB      float64 `json:"quota_limit_mb"`
	QuotaUsedMB       float64 `json:"quota_used_mb"`
	QuotaRemainingMB  float64 `json:"quota_remaining_mb"`
	BytesLimit        int64   `json:"bytes_limit"`
	RemainingBytes    int64   `json:"remaining_bytes"`
	NextResetDate     string  `json:"next_reset_date"`
	HasPendingLicense bool    `json:"has_pending_license"`
	Capabilities      struct {
		MaxFileMB float64  `json:"max_file_mb"`
		Formats   []string `json:"formats"`
	} `json:"capabilities"`
}

// fetchQuota performs a signed GET /v1/quota through hc, so the call goes
// through the rate-limited transport and is counted like any other API hit.
// It returns the HTTP status code (0 on transport errors) alongside the error.
func fetchQuota(ctx context.Context, hc *http.Client, cfg *config.Config) (*quotaInfo, int, error) {
	const path = "/v1/quota"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.SmallPictBaseURL, "/")+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", cfg.SmallPictAPIKey)
	if cfg.SmallPictSecretKey != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		sig := smallpict.HMACSHA256Hex(cfg.SmallPictSecretKey,
			smallpict.BuildStringToSign(http.MethodGet, path, ts, smallpict.EmptySHA256))
		req.Header.Set("X-Timestamp", ts)
		req.Header.Set("X-Signature", sig)
	} else {
		req.Header.Set("Authorization", "Bearer "+cfg.SmallPictAPIKey)
	}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := string(body)
		if len(msg) > 200 {
			msg = msg[:200] + "..."
		}
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, smallpict.SanitizeMessage(msg))
	}

	var q quotaInfo
	if err := json.Unmarshal(body, &q); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("invalid quota response: %w", err)
	}
	return &q, resp.StatusCode, nil
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	dir := flag.String("dir", "../images", "folder containing the source images")
	maxFileMB := flag.Float64("max-file-mb", 0, "skip files larger than this (MB); 0 = no limit. Set according to your plan's upload limit")
	total := flag.Int("total", 1500, "total number of compression jobs")
	rpm := flag.Int("rpm", 60, "maximum API calls per minute (sandbox: adjust to your limit, subscription: 300)")
	workers := flag.Int("workers", 8, "number of parallel workers")
	targetKB := flag.Int("target-kb", 200, "maximum target size of the compressed result (KB), used for statistics")
	verify := flag.Bool("verify", false, "download the result to measure the real size (uses bandwidth/egress)")
	pollEvery := flag.Duration("poll-interval", 2*time.Second, "job status polling interval")
	jobTimeout := flag.Duration("job-timeout", 90*time.Second, "timeout per image (end-to-end)")
	csvPath := flag.String("csv", "loadtest-results.csv", "detailed results CSV file ('' to disable)")
	stopOnFatal := flag.Bool("stop-on-fatal", true, "stop the test on 401/402/403")
	skipQuota := flag.Bool("skip-quota-check", false, "skip the quota check at startup")
	yes := flag.Bool("yes", false, "run without confirmation")
	flag.Parse()

	if *total <= 0 || *rpm <= 0 || *workers <= 0 {
		fmt.Fprintln(os.Stderr, "-total, -rpm, and -workers must be > 0")
		os.Exit(2)
	}

	cfg := config.LoadConfig()
	if cfg.SmallPictAPIKey == "" {
		fmt.Fprintln(os.Stderr, "SMALLPICT_API_KEY is empty. Set it in .env (run from the smallpict-go folder).")
		os.Exit(2)
	}

	samples, err := loadSamples(*dir, int64(*maxFileMB*1024*1024))
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to load images:", err)
		os.Exit(2)
	}
	var sampleBytes int64
	for _, s := range samples {
		sampleBytes += int64(len(s.data))
	}
	estBytes := sampleBytes / int64(len(samples)) * int64(*total)

	baseURL := strings.TrimRight(cfg.SmallPictBaseURL, "/")
	apiHost := strings.TrimPrefix(strings.TrimPrefix(baseURL, "https://"), "http://")
	if i := strings.Index(apiHost, "/"); i != -1 {
		apiHost = apiHost[:i]
	}

	tr := &apiTransport{
		base:     http.DefaultTransport,
		lim:      newSpacingLimiter(*rpm),
		apiHost:  apiHost,
		statuses: map[int]int64{},
	}
	apiHTTP := &http.Client{Transport: tr, Timeout: 60 * time.Second}

	client, err := smallpict.NewClient(
		smallpict.WithAPIKey(cfg.SmallPictAPIKey),
		smallpict.WithSecretKey(cfg.SmallPictSecretKey),
		smallpict.WithBaseURL(cfg.SmallPictBaseURL),
		smallpict.WithHTTPClient(apiHTTP),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to init client:", err)
		os.Exit(2)
	}

	fmt.Println("---------------- CONFIGURATION ----------------")
	fmt.Printf("Base URL     : %s\n", baseURL)
	fmt.Printf("Images       : %d files from %s (average %s)\n", len(samples), *dir, human(sampleBytes/int64(len(samples))))
	fmt.Printf("Total jobs   : %d  (estimated data %s)\n", *total, human(estBytes))
	fmt.Printf("Options      : format=%s quality=%d max_width=%d\n", cfg.SmallPictFormat, cfg.SmallPictQuality, cfg.SmallPictMaxWidth)
	fmt.Printf("Rate limit   : %d API calls/min (1 image ≈ 2-4 calls: optimize + status polling)\n", *rpm)
	fmt.Printf("Workers      : %d | verify download: %v\n", *workers, *verify)
	fmt.Printf("Est. duration: ~%.0f min (at 3 calls/image)\n", float64(*total)*3/float64(*rpm))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Fprintln(os.Stderr, "\n⏹  stopped, summarizing partial results...")
		cancel()
	}()

	if !*skipQuota {
		q, status, err := fetchQuota(ctx, apiHTTP, cfg)
		if err != nil {
			fmt.Printf("Quota        : failed to check (%v)\n", err)
			if status == http.StatusUnauthorized {
				fmt.Fprintln(os.Stderr, "Invalid API key / secret.")
				os.Exit(1)
			}
		} else {
			fmt.Printf("Quota        : plan=%s used %.0f MB of %.0f MB (remaining %.0f MB, resets %s)\n",
				q.Plan, q.QuotaUsedMB, q.QuotaLimitMB, q.QuotaRemainingMB, q.NextResetDate)
			if q.Capabilities.MaxFileMB > 0 {
				fmt.Printf("Plan limits  : max %.0f MB per file | formats: %s\n",
					q.Capabilities.MaxFileMB, strings.Join(q.Capabilities.Formats, ", "))
			}

			estMB := float64(estBytes) / (1024 * 1024)
			if q.QuotaLimitMB > 0 && q.QuotaRemainingMB < estMB {
				fmt.Printf("⚠️  Remaining quota %.0f MB < estimated need %.0f MB. The test will likely stop (402).\n",
					q.QuotaRemainingMB, estMB)
			}
			if q.QuotaLimitMB > 0 && (q.BytesLimit == 0 || q.RemainingBytes == 0) {
				fmt.Printf("⚠️  quota_limit_mb is %.0f but bytes_limit=%d / remaining_bytes=%d. The server may reject\n"+
					"   requests with QUOTA_EXCEEDED even though quota is available (possible plan provisioning issue).\n",
					q.QuotaLimitMB, q.BytesLimit, q.RemainingBytes)
			}
			if q.HasPendingLicense {
				fmt.Println("⚠️  The account has a pending license; the plan may not be fully active yet.")
			}
			if maxMB := q.Capabilities.MaxFileMB; maxMB > 0 {
				oversize := 0
				for _, s := range samples {
					if float64(len(s.data)) > maxMB*1024*1024 {
						oversize++
					}
				}
				if oversize > 0 {
					fmt.Printf("⚠️  %d image(s) exceed the plan's %.0f MB file limit and will fail (use -max-file-mb %.0f to skip them).\n",
						oversize, maxMB, maxMB)
				}
			}
		}
	}
	fmt.Println("---------------------------------------------")

	if !*yes {
		fmt.Print("Continue? [y/N]: ")
		var ans string
		_, _ = fmt.Scanln(&ans)
		if strings.ToLower(strings.TrimSpace(ans)) != "y" {
			fmt.Println("Cancelled.")
			return
		}
	}

	r := &runner{
		client:       client,
		plain:        &http.Client{Timeout: 60 * time.Second},
		cfg:          cfg,
		samples:      samples,
		pollInterval: *pollEvery,
		jobTimeout:   *jobTimeout,
		verify:       *verify,
		stopOnFatal:  *stopOnFatal,
		cancel:       cancel,
	}

	jobs := make(chan int)
	resCh := make(chan result, *workers*2)

	var wg sync.WaitGroup
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for seq := range jobs {
				resCh <- r.process(ctx, seq)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for i := 0; i < *total; i++ {
			select {
			case <-ctx.Done():
				return
			case jobs <- i:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(resCh)
	}()

	start := time.Now()
	results := make([]result, 0, *total)
	var okN, failN int
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

collect:
	for {
		select {
		case res, open := <-resCh:
			if !open {
				break collect
			}
			// results canceled by Ctrl+C / fatal error are not counted
			if !res.ok && res.errKind == "canceled" {
				continue
			}
			results = append(results, res)
			if res.ok {
				okN++
			} else {
				failN++
			}
		case <-ticker.C:
			el := time.Since(start)
			fmt.Printf("[%s] %d/%d done | ok %d | failed %d | %.1f images/min | API calls %d\n",
				el.Round(time.Second), len(results), *total, okN, failN,
				float64(len(results))/maxF(el.Minutes(), 1.0/60), tr.calls.Load())
		}
	}

	elapsed := time.Since(start)
	printSummary(results, elapsed, tr, *total, int64(*targetKB)*1024, *rpm)

	if *csvPath != "" {
		if err := writeCSV(*csvPath, results); err != nil {
			fmt.Fprintln(os.Stderr, "failed to write CSV:", err)
		} else {
			fmt.Printf("Per-request details: %s\n", *csvPath)
		}
	}
	if failN > 0 {
		os.Exit(1)
	}
}
