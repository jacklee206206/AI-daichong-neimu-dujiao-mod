// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package web

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func iconTestRouter(t *testing.T, dir string, resolve func(*gin.Context) (string, error)) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := RegisterSiteIcons(r, resolve, dir); err != nil {
		t.Fatal(err)
	}
	return r
}

func iconTestPNG(t *testing.T, filename string, c color.NRGBA) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func iconTestRequest(t *testing.T, r *gin.Engine, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, target, nil))
	if got := w.Header().Get("Cache-Control"); got != "private, no-store, max-age=0" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	return w
}

func TestSiteIconFormatsAndHEAD(t *testing.T) {
	dir := t.TempDir()
	iconTestPNG(t, filepath.Join(dir, "brand.png"), color.NRGBA{R: 255, A: 255})
	r := iconTestRouter(t, dir, func(*gin.Context) (string, error) { return "/uploads/brand.png", nil })
	for _, tc := range []struct {
		url  string
		size int
		ico  bool
	}{
		{"/favicon.ico", 32, true},
		{"/apple-touch-icon.png", 180, false},
		{"/apple-touch-icon-precomposed.png", 180, false},
		{"/api/v1/public/site-icon", 32, false},
		{"/api/v1/public/site-icon?size=32&file=/etc/passwd", 32, false},
		{"/api/v1/public/site-icon?size=180", 180, false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			w := iconTestRequest(t, r, http.MethodGet, tc.url)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d; body = %q", w.Code, w.Body.String())
			}
			data := w.Body.Bytes()
			if tc.ico {
				if w.Header().Get("Content-Type") != "image/x-icon" || len(data) < 22 || !bytes.Equal(data[:6], []byte{0, 0, 1, 0, 1, 0}) {
					t.Fatal("invalid ICO header or MIME type")
				}
				if data[6] != 32 || data[7] != 32 || binary.LittleEndian.Uint32(data[14:18]) != uint32(len(data)-22) || binary.LittleEndian.Uint32(data[18:22]) != 22 {
					t.Fatal("invalid ICO image directory")
				}
				data = data[22:]
			} else if w.Header().Get("Content-Type") != "image/png" {
				t.Fatal("invalid PNG MIME type")
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil || img.Bounds() != image.Rect(0, 0, tc.size, tc.size) {
				t.Fatalf("invalid PNG size or encoding: %v", err)
			}
			if _, _, _, a := img.At(tc.size/2, 0).RGBA(); a != 0 {
				t.Fatal("aspect ratio lost: expected transparent top margin")
			}
			if red, _, _, a := img.At(tc.size/2, tc.size/2).RGBA(); red != 65535 || a != 65535 {
				t.Fatal("source image missing in center")
			}
			head := iconTestRequest(t, r, http.MethodHead, tc.url)
			if head.Code != w.Code || head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) || head.Header().Get("Content-Type") != w.Header().Get("Content-Type") {
				t.Fatal("HEAD must return GET headers without a body")
			}
		})
	}
}

func TestSiteIconRasterDecoders(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 10, 20))
	for _, format := range []string{"jpeg", "gif", "webp"} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			var err error
			if format == "jpeg" {
				err = jpeg.Encode(&buf, img, nil)
			} else if format == "gif" {
				err = gif.Encode(&buf, img, nil)
			} else {
				// A 2x2 opaque red lossless WebP generated for this test.
				var data []byte
				data, err = base64.StdEncoding.DecodeString("UklGRhwAAABXRUJQVlA4TA8AAAAvAUAAAAcQ/Y/+ByKi/wEA")
				buf.Write(data)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "brand."+format), buf.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			r := iconTestRouter(t, dir, func(*gin.Context) (string, error) { return "/uploads/brand." + format, nil })
			if w := iconTestRequest(t, r, http.MethodGet, "/apple-touch-icon.png"); w.Code != http.StatusOK {
				t.Fatalf("%s status = %d", format, w.Code)
			}
		})
	}
}

func TestSiteIconRejectsInvalidSources(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "invalid.png"), []byte("<html>not an image</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "directory.png"), 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	iconTestPNG(t, outside, color.NRGBA{A: 255})
	if err := os.Symlink(outside, filepath.Join(dir, "escape.png")); err != nil {
		t.Fatal(err)
	}
	large, err := os.Create(filepath.Join(dir, "large.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := large.Truncate(siteIconMaxBytes + 1); err != nil {
		t.Fatal(err)
	}
	large.Close()
	// A valid PNG header advertises huge dimensions without allocating the image.
	iconTestPNG(t, filepath.Join(dir, "pixels.png"), color.NRGBA{A: 255})
	pixels, err := os.ReadFile(filepath.Join(dir, "pixels.png"))
	if err != nil {
		t.Fatal(err)
	}
	binary.BigEndian.PutUint32(pixels[16:20], 1<<20)
	binary.BigEndian.PutUint32(pixels[20:24], 1<<20)
	binary.BigEndian.PutUint32(pixels[29:33], crc32.ChecksumIEEE(pixels[12:29]))
	if err := os.WriteFile(filepath.Join(dir, "pixels.png"), pixels, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		source string
		status int
	}{
		{"/uploads/missing.png", 404},
		{"/uploads/invalid.png", 400},
		{"/uploads/directory.png", 400},
		{"/uploads/escape.png", 400},
		{"/uploads/large.png", 413},
		{"/uploads/pixels.png", 413},
		{"/uploads/../outside.png", 400},
		{"/uploads/%2e%2e/outside.png", 400},
		{"/uploads/%252e%252e/outside.svg", 400},
		{"/uploads/a\\b.png", 400},
		{"/etc/passwd", 400},
		{"file:///etc/passwd", 400},
		{"javascript:alert(1)", 400},
		{"https://user:secret@example.com/icon.png", 400},
		{"https:///missing-host.png", 400},
		{"//example.com/icon.png", 400},
		{"/favicon.ico", 400},
	} {
		t.Run(tc.source, func(t *testing.T) {
			r := iconTestRouter(t, dir, func(*gin.Context) (string, error) { return tc.source, nil })
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				w := iconTestRequest(t, r, method, "/favicon.ico")
				if w.Code != tc.status || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") || strings.Contains(w.Body.String(), dir) || (method == http.MethodHead && w.Body.Len() != 0) {
					t.Fatalf("%s: status=%d body=%q", method, w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestSiteIconRedirectsAndResolverErrors(t *testing.T) {
	for _, source := range []string{"", "/dj.svg", "/uploads/icon.svg?v=2", "/uploads/icon.ico", "https://example.com/icon.png?v=2", "http://example.com/icon.png"} {
		t.Run(source, func(t *testing.T) {
			r := iconTestRouter(t, t.TempDir(), func(*gin.Context) (string, error) { return source, nil })
			w := iconTestRequest(t, r, http.MethodGet, "/favicon.ico")
			want := source
			if want == "" {
				want = "/dj.svg"
			}
			if w.Code != http.StatusFound || w.Header().Get("Location") != want || w.Body.Len() != 0 {
				t.Fatalf("redirect = %d %q", w.Code, w.Header().Get("Location"))
			}
		})
	}
	r := iconTestRouter(t, t.TempDir(), func(*gin.Context) (string, error) { return "", errors.New("private database details") })
	if w := iconTestRequest(t, r, http.MethodGet, "/favicon.ico"); w.Code != 500 || strings.Contains(w.Body.String(), "database") {
		t.Fatalf("resolver error = %d %q", w.Code, w.Body.String())
	}
	missing := iconTestRouter(t, t.TempDir(), func(*gin.Context) (string, error) { return "", os.ErrNotExist })
	if w := iconTestRequest(t, missing, http.MethodGet, "/favicon.ico"); w.Code != http.StatusNotFound {
		t.Fatalf("missing tenant status = %d", w.Code)
	}
	for _, size := range []string{"", "0", "64", "-32", "180&size=32", "../x"} {
		if w := iconTestRequest(t, r, http.MethodGet, "/api/v1/public/site-icon?size="+size); w.Code != 400 {
			t.Fatalf("size %q status = %d", size, w.Code)
		}
	}
}

func TestSiteIconTenantAndFileChanges(t *testing.T) {
	dir := t.TempDir()
	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	iconTestPNG(t, filepath.Join(dir, "one.png"), red)
	iconTestPNG(t, filepath.Join(dir, "two.png"), blue)
	calls := 0
	r := iconTestRouter(t, dir, func(c *gin.Context) (string, error) {
		calls++
		if c.Request.Host == "two.test" {
			return "/uploads/two.png", nil
		}
		return "/uploads/one.png", nil
	})
	check := func(host string, want color.NRGBA) {
		t.Helper()
		w := iconTestRequest(t, r, http.MethodGet, "https://"+host+"/api/v1/public/site-icon")
		img, err := png.Decode(w.Body)
		if err != nil {
			t.Fatal(err)
		}
		if got := color.NRGBAModel.Convert(img.At(16, 16)); got != want {
			t.Fatalf("%s center = %v, want %v", host, got, want)
		}
	}
	check("one.test", red)
	check("two.test", blue)
	check("one.test", red)
	iconTestPNG(t, filepath.Join(dir, "one.png"), blue)
	now := time.Now().Add(time.Second)
	if err := os.Chtimes(filepath.Join(dir, "one.png"), now, now); err != nil {
		t.Fatal(err)
	}
	check("one.test", blue)
	if calls != 4 {
		t.Fatalf("resolver called %d times, want 4", calls)
	}
	if err := os.Remove(filepath.Join(dir, "one.png")); err != nil {
		t.Fatal(err)
	}
	if w := iconTestRequest(t, r, http.MethodGet, "https://one.test/favicon.ico"); w.Code != 404 {
		t.Fatalf("cached deleted file status = %d", w.Code)
	}
}

func TestSiteIconCacheBound(t *testing.T) {
	dir := t.TempDir()
	icons := &siteIcons{uploadDir: dir, cache: make(map[siteIconKey]siteIconImage)}
	for i := 0; i < siteIconCacheEntries+2; i++ {
		name := strconv.Itoa(i) + ".png"
		iconTestPNG(t, filepath.Join(dir, name), color.NRGBA{A: 255})
		if _, status := icons.convert(name, 32); status != http.StatusOK {
			t.Fatalf("convert status = %d", status)
		}
	}
	if len(icons.cache) != siteIconCacheEntries {
		t.Fatalf("cache size = %d", len(icons.cache))
	}
}
