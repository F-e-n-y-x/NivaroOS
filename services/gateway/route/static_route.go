package route

import (
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/gateway/service"
	"github.com/labstack/echo/v5"
	echo_middleware "github.com/labstack/echo/v5/middleware"
)

type StaticRoute struct {
	state *service.State
}

func NewStaticRoute(state *service.State) *StaticRoute {
	return &StaticRoute{
		state: state,
	}
}

type CustomFS struct {
	base fs.FS
}

func NewCustomFS(prefix string) *CustomFS {
	return &CustomFS{
		base: fs.FS(os.DirFS(prefix)),
	}
}

func (c *CustomFS) Open(name string) (fs.File, error) {
	file, err := c.base.Open(name)
	if err != nil {
		return nil, err
	}
	return &CustomFile{
		File: file,
	}, nil
}

func (c *CustomFS) Stat(name string) (fs.FileInfo, error) {
	file, err := c.base.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	return &CustomFileInfo{
		FileInfo: info,
	}, nil
}

type CustomFile struct {
	fs.File
}

func (c *CustomFile) Stat() (fs.FileInfo, error) {
	info, err := c.File.Stat()
	if err != nil {
		return nil, err
	}
	return &CustomFileInfo{
		FileInfo: info,
	}, nil
}

func (c *CustomFile) Read(p []byte) (int, error) {
	if seeker, ok := c.File.(io.Reader); ok {
		return seeker.Read(p)
	}
	return 0, fmt.Errorf("file does not implement io.Reader")
}

func (c *CustomFile) Seek(offset int64, whence int) (int64, error) {
	if seeker, ok := c.File.(io.Seeker); ok {
		return seeker.Seek(offset, whence)
	}
	return 0, fmt.Errorf("file does not implement io.Seeker")
}

type CustomFileInfo struct {
	fs.FileInfo
}

// hashedName: a build output with a content hash in its name
// (app.87b43194.js, js/x.ec5a2b53.js, img/logo.569eeabe.png) - its content
// never changes under that name.
var hashedName = regexp.MustCompile(`\.[0-9a-f]{8}\.[A-Za-z0-9]+$`)

func cacheControl(urlPath string) string {
	if hashedName.MatchString(urlPath) {
		return "public, max-age=31536000, immutable"
	}
	if urlPath == "" || urlPath == "/" || strings.HasSuffix(urlPath, ".html") {
		return "no-cache, no-store, must-revalidate"
	}
	return "no-cache"
}

// precompressed serves <file>.br / <file>.gz written at build time (see
// ui/vite.config.mjs) to clients that accept them, so the gzip middleware
// doesn't recompress the same bytes on every request.
func precompressed(root fs.FS) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			r := c.Request()
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				return next(c)
			}
			name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
			if name == "" {
				name = "index.html"
			}
			accept := r.Header.Get("Accept-Encoding")
			for _, enc := range []struct{ token, ext string }{{"br", ".br"}, {"gzip", ".gz"}} {
				if !strings.Contains(accept, enc.token) {
					continue
				}
				f, err := root.Open(name + enc.ext)
				if err != nil {
					continue
				}
				defer f.Close()
				info, err := f.Stat()
				rs, ok := f.(io.ReadSeeker)
				if err != nil || info.IsDir() || !ok {
					continue
				}
				h := c.Response().Header()
				h.Set("Content-Encoding", enc.token)
				h.Add("Vary", "Accept-Encoding")
				if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
					h.Set("Content-Type", ct)
				}
				http.ServeContent(c.Response(), r, name, info.ModTime(), rs)
				return nil
			}
			return next(c)
		}
	}
}

func (s *StaticRoute) GetRoute() http.Handler {
	e := echo.New()

	// Hashed build files are cached for good; index.html is never cached
	// (utils/updateWatcher.js reads it to spot a new build); everything
	// else revalidates. A failed request (404) gets no cache header.
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			h := c.Response().Header()
			h.Set("Cache-Control", cacheControl(c.Request().URL.Path))
			if strings.HasPrefix(h.Get("Cache-Control"), "no-cache, no-store") {
				h.Set("Pragma", "no-cache")
				h.Set("Expires", "0")
			}
			err := next(c)
			if err != nil {
				h.Del("Cache-Control")
			}
			return err
		}
	})
	e.Use(precompressed(os.DirFS(s.state.GetWWWPath())))
	e.Use(echo_middleware.Gzip())

	e.StaticFS("/", NewCustomFS(s.state.GetWWWPath()))
	return e
}
