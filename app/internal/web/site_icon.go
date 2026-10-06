// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package web

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	siteIconMaxBytes     = 8 << 20
	siteIconMaxPixels    = 16 << 20
	siteIconCacheEntries = 64
	siteIconCacheControl = "private, no-store, max-age=0"
)

type siteIconKey struct {
	name     string
	modified int64
	bytes    int64
	size     int
}

type siteIconImage struct {
	source fs.FileInfo
	png    []byte
}

type siteIcons struct {
	uploadDir string
	mu        sync.Mutex
	cache     map[siteIconKey]siteIconImage
}

// RegisterSiteIcons serves browser icons using the brand resolved for each request.
// Only configured upload paths are read locally; external URLs are never fetched.
func RegisterSiteIcons(r *gin.Engine, resolve func(*gin.Context) (string, error), uploadDir string) error {
	if r == nil || resolve == nil || strings.TrimSpace(uploadDir) == "" {
		return errors.New("site icons require an engine, resolver and upload directory")
	}
	dir, err := filepath.Abs(uploadDir)
	if err != nil {
		return errors.New("invalid site icon upload directory")
	}
	icons := &siteIcons{uploadDir: dir, cache: make(map[siteIconKey]siteIconImage)}
	handler := func(c *gin.Context) {
		c.Header("Cache-Control", siteIconCacheControl)
		c.Header("X-Content-Type-Options", "nosniff")
		size := 32
		switch c.Request.URL.Path {
		case "/apple-touch-icon.png", "/apple-touch-icon-precomposed.png":
			size = 180
		case "/api/v1/public/site-icon":
			if values, exists := c.Request.URL.Query()["size"]; exists {
				if len(values) != 1 || (values[0] != "32" && values[0] != "180") {
					siteIconError(c, http.StatusBadRequest)
					return
				}
				size, _ = strconv.Atoi(values[0])
			}
		}
		configured, err := resolve(c)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, fs.ErrNotExist) {
				status = http.StatusNotFound
			}
			siteIconError(c, status)
			return
		}
		name, redirect, err := siteIconSource(configured)
		if err != nil {
			siteIconError(c, http.StatusBadRequest)
			return
		}
		if redirect != "" {
			c.Header("Location", redirect)
			siteIconResponse(c, http.StatusFound, "text/plain; charset=utf-8", nil)
			return
		}
		data, status := icons.convert(name, size)
		if status != http.StatusOK {
			siteIconError(c, status)
			return
		}
		mime := "image/png"
		if c.Request.URL.Path == "/favicon.ico" {
			data = siteIconICO(data, size)
			mime = "image/x-icon"
		}
		siteIconResponse(c, http.StatusOK, mime, data)
	}
	for _, route := range []string{"/favicon.ico", "/apple-touch-icon.png", "/apple-touch-icon-precomposed.png", "/api/v1/public/site-icon"} {
		r.GET(route, handler)
		r.HEAD(route, handler)
	}
	return nil
}

func siteIconSource(configured string) (name, redirect string, err error) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return "", "/dj.svg", nil
	}
	invalid := errors.New("invalid site icon source")
	if strings.ContainsAny(configured, "\\\r\n\t") {
		return "", "", invalid
	}
	u, err := url.Parse(configured)
	if err != nil || u.User != nil || u.Opaque != "" || u.Fragment != "" {
		return "", "", invalid
	}
	if u.IsAbs() {
		if (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
			return "", "", invalid
		}
		return "", u.String(), nil
	}
	// Validate the decoded path before either opening or redirecting it.
	if u.Host != "" || !strings.HasPrefix(u.Path, "/") || strings.ContainsAny(u.Path, "\\%\x00\r\n\t") || !fs.ValidPath(strings.TrimPrefix(u.Path, "/")) {
		return "", "", invalid
	}
	ext := strings.ToLower(path.Ext(u.Path))
	if ext == ".svg" || ext == ".ico" {
		if u.Path == "/favicon.ico" { // Avoid redirecting the endpoint to itself.
			return "", "", invalid
		}
		return "", u.String(), nil
	}
	if !strings.HasPrefix(u.Path, "/uploads/") {
		return "", "", invalid
	}
	return strings.TrimPrefix(u.Path, "/uploads/"), "", nil
}

func (icons *siteIcons) convert(name string, size int) ([]byte, int) {
	root, err := os.OpenRoot(icons.uploadDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, http.StatusNotFound
		}
		return nil, http.StatusInternalServerError
	}
	defer root.Close()
	f, err := root.Open(name) // OpenRoot prevents traversal and escaping symlinks, including races.
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, http.StatusNotFound
		}
		return nil, http.StatusBadRequest
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, http.StatusInternalServerError
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return nil, http.StatusBadRequest
	}
	if info.Size() > siteIconMaxBytes {
		return nil, http.StatusRequestEntityTooLarge
	}
	key := siteIconKey{name: name, modified: info.ModTime().UnixNano(), bytes: info.Size(), size: size}
	// Serialize misses too, so concurrent requests cannot multiply decode memory use.
	icons.mu.Lock()
	defer icons.mu.Unlock()
	if cached, ok := icons.cache[key]; ok && os.SameFile(info, cached.source) {
		return cached.png, http.StatusOK
	}
	raw, err := io.ReadAll(io.LimitReader(f, siteIconMaxBytes+1))
	if err != nil {
		return nil, http.StatusBadRequest
	}
	if len(raw) > siteIconMaxBytes {
		return nil, http.StatusRequestEntityTooLarge
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "png" && format != "jpeg" && format != "gif" && format != "webp") || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, http.StatusBadRequest
	}
	if uint64(cfg.Width) > siteIconMaxPixels/uint64(cfg.Height) {
		return nil, http.StatusRequestEntityTooLarge
	}
	source, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil || source.Bounds().Dx() != cfg.Width || source.Bounds().Dy() != cfg.Height {
		return nil, http.StatusBadRequest
	}
	w, h := size, size
	if cfg.Width > cfg.Height {
		h = max(1, size*cfg.Height/cfg.Width)
	} else {
		w = max(1, size*cfg.Width/cfg.Height)
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, size, size))
	box := image.Rect((size-w)/2, (size-h)/2, (size-w)/2+w, (size-h)/2+h)
	draw.CatmullRom.Scale(canvas, box, source, source.Bounds(), draw.Src, nil)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		return nil, http.StatusInternalServerError
	}
	// A concurrently modified file may be served once, but must not poison the cache.
	if after, err := f.Stat(); err == nil && after.Size() == info.Size() && after.ModTime().Equal(info.ModTime()) {
		for old := range icons.cache {
			if old.name == name && old.size == size {
				delete(icons.cache, old)
			}
		}
		if len(icons.cache) >= siteIconCacheEntries {
			for old := range icons.cache {
				delete(icons.cache, old)
				break
			}
		}
		icons.cache[key] = siteIconImage{source: info, png: encoded.Bytes()}
	}
	return encoded.Bytes(), http.StatusOK
}

func siteIconICO(png []byte, size int) []byte {
	data := make([]byte, 22+len(png))
	binary.LittleEndian.PutUint16(data[2:4], 1) // ICO image type.
	binary.LittleEndian.PutUint16(data[4:6], 1) // One PNG image.
	data[6], data[7] = byte(size), byte(size)
	binary.LittleEndian.PutUint16(data[10:12], 1)
	binary.LittleEndian.PutUint16(data[12:14], 32)
	binary.LittleEndian.PutUint32(data[14:18], uint32(len(png)))
	binary.LittleEndian.PutUint32(data[18:22], 22)
	copy(data[22:], png)
	return data
}

func siteIconError(c *gin.Context, status int) {
	siteIconResponse(c, status, "text/plain; charset=utf-8", []byte(http.StatusText(status)+"\n"))
}

func siteIconResponse(c *gin.Context, status int, mime string, data []byte) {
	c.Header("Content-Type", mime)
	c.Header("Content-Length", strconv.Itoa(len(data)))
	c.Status(status)
	if c.Request.Method != http.MethodHead {
		_, _ = c.Writer.Write(data)
	}
}
