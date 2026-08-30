package route

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"

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

// CustomFileInfo used to freeze ModTime() at the moment the gateway
// process started (a package-level `startTime`, captured once) instead
// of the file's real mtime - worked around some deployments producing
// static files with a broken/epoch timestamp (extracted from an
// archive with no preserved mtimes), which made an old but non-zero
// `If-Modified-Since` from the browser incorrectly satisfy `mtime <=
// If-Modified-Since` and 304 on a page that had never actually been
// served before.
//
// That "fix" only ever invalidated caches once per gateway *process*
// lifetime (on restart) - fine for an update that also restarts the
// gateway, but wrong for any deployment that only replaces the static
// files without restarting it: every file kept reporting the exact
// same Last-Modified forever, so a browser that had already cached the
// dashboard once would get 304'd for every later deploy, seeing none
// of them. Report the real underlying mtime instead - a normal build
// (this fork's `pnpm build`, or any sane packaging step) already
// stamps output files with a correct, current timestamp, so this
// reflects real changes correctly without reintroducing the original
// epoch-timestamp problem.
type CustomFileInfo struct {
	fs.FileInfo
}

func (s *StaticRoute) GetRoute() http.Handler {
	e := echo.New()

	e.Use(echo_middleware.Gzip())

	e.StaticFS("/", NewCustomFS(s.state.GetWWWPath()))
	return e
}
