package route

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/gateway/service"
	"github.com/labstack/echo/v4"
	echo_middleware "github.com/labstack/echo/v4/middleware"
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

func (s *StaticRoute) GetRoute() http.Handler {
	e := echo.New()

	e.Use(echo_middleware.Gzip())

	// Prevent browser from permanently caching index.html so changes are immediately visible
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Request().URL.Path
			if path == "" || path == "/" || path == "/index.html" || strings.HasSuffix(path, ".html") {
				c.Response().Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
				c.Response().Header().Set("Pragma", "no-cache")
				c.Response().Header().Set("Expires", "0")
			}
			return next(c)
		}
	})

	e.StaticFS("/", NewCustomFS(s.state.GetWWWPath()))
	return e
}
