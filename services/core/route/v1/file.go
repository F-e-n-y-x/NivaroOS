package v1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	url2 "net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/mholt/archiver/v3"
	"github.com/robfig/cron/v3"
	"github.com/tidwall/gjson"

	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/core/service/model"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/h2non/filetype"
)

type ListReq struct {
	model.PageReq
	Path string `json:"path" form:"path"`
	// Refresh bool   `json:"refresh"`
}

type ObjResp struct {
	Name       string                 `json:"name"`
	Size       int64                  `json:"size"`
	IsDir      bool                   `json:"is_dir"`
	Modified   time.Time              `json:"modified"`
	Sign       string                 `json:"sign"`
	Thumb      string                 `json:"thumb"`
	Type       int                    `json:"type"`
	Path       string                 `json:"path"`
	Date       time.Time              `json:"date"`
	Extensions map[string]interface{} `json:"extensions"`
}
type FsListResp struct {
	Content  []ObjResp `json:"content"`
	Total    int64     `json:"total"`
	Readme   string    `json:"readme,omitempty"`
	Write    bool      `json:"write,omitempty"`
	Provider string    `json:"provider,omitempty"`
	Index    int       `json:"index"`
	Size     int       `json:"size"`
}

var (
	// 升级成 WebSocket 协议
	upgraderFile = websocket.Upgrader{
		// 允许CORS跨域请求
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	conn *websocket.Conn
	err  error
)

// @Summary 读取文件
// @Produce  application/json
// @Accept application/json
// @Tags file
// @Security ApiKeyAuth
// @Param path query string true "路径"
// @Success 200 {string} string "ok"
// @Router /file/read [get]
func GetFilerContent(ctx echo.Context) error {
	filePath := ctx.QueryParam("path")
	if len(filePath) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{
			Success: common_err.INVALID_PARAMS,
			Message: common_err.GetMsg(common_err.INVALID_PARAMS),
		})
	}
	if !file.Exists(filePath) {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{
			Success: common_err.FILE_DOES_NOT_EXIST,
			Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST),
		})
	}
	// 文件读取任务是将文件内容读取到内存中。
	info, err := ioutil.ReadFile(filePath)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{
			Success: common_err.FILE_READ_ERROR,
			Message: common_err.GetMsg(common_err.FILE_READ_ERROR),
			Data:    err.Error(),
		})
	}
	result := string(info)

	return ctx.JSON(common_err.SUCCESS, model.Result{
		Success: common_err.SUCCESS,
		Message: common_err.GetMsg(common_err.SUCCESS),
		Data:    result,
	})
}

func GetLocalFile(ctx echo.Context) error {
	path := ctx.QueryParam("path")
	if len(path) == 0 {
		return ctx.JSON(http.StatusOK, model.Result{
			Success: common_err.INVALID_PARAMS,
			Message: common_err.GetMsg(common_err.INVALID_PARAMS),
		})
	}
	if !file.Exists(path) {
		return ctx.JSON(http.StatusOK, model.Result{
			Success: common_err.FILE_DOES_NOT_EXIST,
			Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST),
		})
	}
	return ctx.File(path)
}

// @Summary download
// @Produce  application/json
// @Accept application/json
// @Tags file
// @Security ApiKeyAuth
// @Param format query string false "Compression format" Enums(zip,tar,targz)
// @Param files query string true "file list eg: filename1,filename2,filename3 "
// @Success 200 {string} string "ok"
// @Router /file/download [get]
func GetDownloadFile(ctx echo.Context) error {
	t := ctx.QueryParam("format")

	files := ctx.QueryParam("files")

	if len(files) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{
			Success: common_err.INVALID_PARAMS,
			Message: common_err.GetMsg(common_err.INVALID_PARAMS),
		})
	}
	list := strings.Split(files, ",")
	for _, v := range list {
		if !file.Exists(v) {
			if dev, phonePath := GetCompanionDeviceByStoragePath(v); dev != nil {
				if len(list) == 1 {
					if err := ProxyCompanionFileDownload(dev, phonePath, ctx); err == nil {
						return nil
					}
				}
			}
			return ctx.JSON(common_err.SERVICE_ERROR, model.Result{
				Success: common_err.FILE_DOES_NOT_EXIST,
				Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST),
			})
		}
	}
	ctx.Request().Header.Add("Content-Type", "application/octet-stream")
	ctx.Request().Header.Add("Content-Transfer-Encoding", "binary")
	ctx.Request().Header.Add("Cache-Control", "no-cache")
	// handles only single files not folders and multiple files
	if len(list) == 1 {

		filePath := list[0]
		info, err := os.Stat(filePath)
		if err != nil {
			return ctx.JSON(http.StatusOK, model.Result{
				Success: common_err.FILE_DOES_NOT_EXIST,
				Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST),
			})
		}
		if !info.IsDir() {

			// 打开文件
			fileTmp, _ := os.Open(filePath)
			defer fileTmp.Close()

			// 获取文件的名称
			fileName := path.Base(filePath)
			ctx.Response().Header().Add("Content-Disposition", "attachment; filename*=utf-8''"+url2.PathEscape(fileName))
			ctx.File(filePath)
		}
	}

	extension, ar, err := file.GetCompressionAlgorithm(t)
	if err != nil {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{
			Success: common_err.INVALID_PARAMS,
			Message: common_err.GetMsg(common_err.INVALID_PARAMS),
		})
	}

	err = ar.Create(ctx.Response().Writer)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: common_err.GetMsg(common_err.SERVICE_ERROR),
			Data:    err.Error(),
		})
	}
	defer ar.Close()
	commonDir := file.CommonPrefix(filepath.Separator, list...)

	currentPath := filepath.Base(commonDir)

	name := "_" + currentPath
	name += extension
	ctx.Request().Header.Add("Content-Disposition", "attachment; filename*=utf-8''"+url.PathEscape(name))
	for _, fname := range list {
		err = file.AddFile(ar, fname, commonDir)
		if err != nil {
			log.Printf("Failed to archive %s: %v", fname, err)
		}
	}
	return nil
}

func GetDownloadSingleFile(ctx echo.Context) error {
	filePath := ctx.QueryParam("path")
	if len(filePath) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{
			Success: common_err.INVALID_PARAMS,
			Message: common_err.GetMsg(common_err.INVALID_PARAMS),
		})
	}
	fileName := path.Base(filePath)

	if dev, phonePath := GetCompanionDeviceByStoragePath(filePath); dev != nil {
		if !file.Exists(filePath) {
			if err := ProxyCompanionFileDownload(dev, phonePath, ctx); err == nil {
				return nil
			}
		}
	}

	// node.ModTime()/.Size() below used to run on whatever os.Stat returned
	// with its error silently ignored - a nil node (Stat failing after Open
	// somehow succeeded - not impossible on a FUSE/cloud mount) would have
	// panicked on the very next line.
	node, err := os.Stat(filePath)
	if err != nil {
		return ctx.JSON(http.StatusNotFound, model.Result{
			Success: common_err.FILE_DOES_NOT_EXIST,
			Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST),
		})
	}

	fi, err := os.Open(filePath)
	if err != nil {
		return ctx.JSON(http.StatusNotFound, model.Result{
			Success: common_err.FILE_DOES_NOT_EXIST,
			Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST),
		})
	}
	defer fi.Close()

	// Sniff the file's real type from its first 261 bytes via a SEPARATE
	// handle that's opened, read, and closed here, never touching `fi` -
	// this used to read those bytes directly from `fi` and hand that same,
	// now-261-bytes-advanced handle to http.ServeContent below. For a local
	// disk file that happened to still work (os.File's Seek is unaffected by
	// a prior Read), but for a FUSE/cloud mount (e.g. an rclone drive) that
	// read-then-reuse pattern is exactly what broke it: ServeContent's own
	// internal Seek-based size/Range handling on that already-disturbed
	// handle came back with Content-Length: 0 and no actual body on every
	// request - confirmed live (curl -r 0-1000 got "200 OK, Content-Length:
	// 0" for an rclone-mounted video, vs. a correct "206 Partial Content"
	// for the exact same request against a local-disk file) - which is why
	// video (and every other file type) only ever failed for cloud-mounted
	// files, never local ones.
	buffer := make([]byte, 261)
	if sniffFile, sniffErr := os.Open(filePath); sniffErr == nil {
		_, _ = sniffFile.Read(buffer)
		sniffFile.Close()
		if kind, _ := filetype.Match(buffer); kind != filetype.Unknown {
			// Response header, not Request - every one of these used to be
			// ctx.Request().Header.Add(...), which mutates the *incoming*
			// request Echo already finished routing, never reaching the
			// client. Harmless for Content-Type specifically (ServeContent
			// falls back to sniffing the extension itself when the response
			// Content-Type isn't already set), but real dead code, and the
			// Content-Disposition case below was silently never taking
			// effect at all.
			ctx.Response().Header().Set("Content-Type", kind.MIME.Value)
		}
	}
	ctx.Response().Header().Set("Content-Disposition", "attachment; filename*=utf-8''"+url2.PathEscape(fileName))

	http.ServeContent(ctx.Response().Writer, ctx.Request(), fileName, node.ModTime(), fi)
	return nil
}

// remuxCacheDir holds completed remuxes of GetStreamRemuxVideo, keyed by a
// hash of the source path + mtime + size (so an edited/replaced source file
// naturally gets a fresh cache entry instead of serving stale remuxed
// bytes). A package-level sync.Map (remuxInFlight) tracks jobs currently
// being produced, so concurrent requests for the same not-yet-cached video
// (opening it in two tabs, or a retry) wait on the one ffmpeg run already
// in progress instead of each starting their own.
const remuxCacheDir = "/var/lib/nivaroos/remux-cache"

// remuxCacheMaxBytes bounds the cache directory's total size - each entry
// is a full remuxed copy of a source video, so without a cap this would
// grow forever. Trimmed by deleting the least-recently-used entries
// (mtime) down to this size whenever a new one is added.
const remuxCacheMaxBytes int64 = 20 * 1024 * 1024 * 1024 // 20GB

var remuxInFlight sync.Map // cache key (string) -> *sync.WaitGroup

func remuxCacheKey(filePath string, info os.FileInfo) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", filePath, info.ModTime().UnixNano(), info.Size())))
	return hex.EncodeToString(sum[:])
}

// GetStreamRemuxVideo serves a video file's container remuxed into MP4, for
// a source container no browser can play natively at all regardless of the
// codec inside it (Chrome/Firefox have no built-in Matroska/.mkv support,
// nor QuickTime/.mov, etc.). `-c copy` only rewraps the existing
// audio/video streams into an MP4 container - it never re-encodes, so this
// stays fast and cheap (bounded by disk I/O, not real transcode CPU cost),
// but it only actually fixes playback when the codecs already inside the
// source are ones the browser can decode (H.264 video + AAC audio is the
// common case for most ripped/downloaded content). A file using
// H.265/AC3/etc. would still fail in the browser after this - remuxing the
// container can't fix an incompatible codec, only a full transcode
// (heavier, real CPU cost, a separate feature) could.
//
// Remuxes to a real, complete, +faststart MP4 file on disk first (cached
// under remuxCacheDir), THEN serves that file the exact same
// http.ServeContent way GetDownloadSingleFile above does - not streamed
// live from ffmpeg's stdout as the first version of this handler did. That
// live-pipe approach produced a fragmented MP4 (frag_keyframe+empty_moov),
// which is built for playback via Media Source Extensions (a JS-driven
// SourceBuffer), not naive `<video src>` loading - browser support for a
// fragmented MP4 assigned directly to `src` is genuinely inconsistent, and
// is what caused "This video couldn't be played" even though the piped
// bytes were themselves a valid, well-formed MP4 (confirmed: `file`
// correctly identified the captured output as MP4). +faststart on a
// complete, ordinary (non-fragmented) file is the universally-supported
// case, and reusing ServeContent for it also means seeking/scrubbing works
// on a remuxed video now, which the old live-pipe version never supported
// at all.
func GetStreamRemuxVideo(ctx echo.Context) error {
	filePath := ctx.QueryParam("path")
	if len(filePath) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{
			Success: common_err.INVALID_PARAMS,
			Message: common_err.GetMsg(common_err.INVALID_PARAMS),
		})
	}

	if err := os.MkdirAll(remuxCacheDir, 0755); err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
	}

	// A local file's cache key includes its mtime/size (remuxCacheKey), so
	// an edited/replaced file naturally gets a fresh entry - a companion
	// device path, not yet locally cached, has no local stat to build that
	// from at all (that's the same reason GetDownloadSingleFile above needs
	// its own companion branch: os.Stat on it just fails). Keying on the
	// logical path alone here means the same not-yet-downloaded companion
	// video only ever gets fetched-and-remuxed once - checked BEFORE
	// resolveRemuxSource below ever downloads anything, so a cache hit for
	// a companion video (the slowest, most bandwidth-limited source this
	// endpoint serves, and exactly where repeat downloads would hurt most -
	// e.g. Artplayer's own multiple Range requests during one playback)
	// never re-fetches it from the phone just to compute a cache key.
	info, statErr := os.Stat(filePath)
	isLocal := statErr == nil
	var cacheKey string
	if isLocal {
		cacheKey = remuxCacheKey(filePath, info)
	} else {
		sum := sha256.Sum256([]byte(filePath))
		cacheKey = hex.EncodeToString(sum[:])
	}
	cachePath := filepath.Join(remuxCacheDir, cacheKey+".mp4")

	if _, err := os.Stat(cachePath); err != nil {
		// A companion device path (e.g.
		// "/DATA/Companion/<phone>/Download/x.mkv") not yet locally cached
		// isn't a real file at all until fetched from the phone.
		sourcePath, cleanupSource, err := resolveRemuxSource(ctx, filePath)
		if err != nil {
			return ctx.JSON(http.StatusNotFound, model.Result{
				Success: common_err.FILE_DOES_NOT_EXIST,
				Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST),
			})
		}
		remuxErr := produceRemux(ctx, sourcePath, cachePath)
		cleanupSource()
		if remuxErr != nil {
			return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: remuxErr.Error()})
		}
	} else {
		// Already cached - still worth a fresh mtime so the LRU trim below
		// doesn't evict something actively being (re)watched.
		now := time.Now()
		_ = os.Chtimes(cachePath, now, now)
	}

	cacheFile, err := os.Open(cachePath)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
	}
	defer cacheFile.Close()
	cacheInfo, err := cacheFile.Stat()
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
	}

	ctx.Response().Header().Set("Content-Type", "video/mp4")
	http.ServeContent(ctx.Response().Writer, ctx.Request(), filepath.Base(filePath)+".mp4", cacheInfo.ModTime(), cacheFile)
	return nil
}

// resolveRemuxSource returns a real local path ffmpeg can read filePath
// from - filePath itself, unchanged, when it's already a real local file
// (including an already-locally-synced companion file), or a freshly
// downloaded temp copy when it's a companion device path not yet cached
// locally (GetDownloadSingleFile's own companion branch, above, handles the
// exact same case for plain downloads - ffmpeg needs an actual file it can
// open and read, not a remote phone path string, so a live HTTP-proxy
// stream the way ProxyCompanionStream does for GetDownloadSingleFile isn't
// an option here). The returned cleanup func removes that temp download
// once the remux is done reading it - the actual cache entry this exists to
// produce (produceRemux's cachePath) is what persists, not this.
func resolveRemuxSource(ctx echo.Context, filePath string) (string, func(), error) {
	noop := func() {}
	if _, err := os.Stat(filePath); err == nil {
		return filePath, noop, nil
	}

	dev, phonePath := GetCompanionDeviceByStoragePath(filePath)
	if dev == nil {
		return "", noop, fmt.Errorf("file does not exist: %s", filePath)
	}
	devIP := dev.IP
	if devIP == "" || devIP == "Local Device" || devIP == "Local" || strings.HasPrefix(devIP, "127.") {
		return "", noop, fmt.Errorf("companion device has no direct LAN IP")
	}
	port := dev.Port
	if port <= 0 {
		port = 8765
	}
	urlStr := fmt.Sprintf("http://%s:%d/download?path=%s", devIP, port, url.QueryEscape(phonePath))
	// Not ctx.Request().Context() - this download can significantly outlive
	// the original request once produceRemux's own remuxInFlight dedup lets
	// other concurrent requests for the same file wait on it rather than
	// each downloading their own copy; tying it to whichever one of those
	// requests happened to be first would let anyone else's disconnect kill
	// the fetch out from under all of them.
	req, err := http.NewRequestWithContext(context.Background(), "GET", urlStr, nil)
	if err != nil {
		return "", noop, err
	}
	req.Header.Set("X-Companion-Secret", dev.Secret)
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", noop, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", noop, fmt.Errorf("companion device returned status %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "nivaroos-companion-remux-src-*"+filepath.Ext(filePath))
	if err != nil {
		return "", noop, err
	}
	cleanup := func() { os.Remove(tmp.Name()) }
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		cleanup()
		return "", noop, err
	}
	tmp.Close()

	dev.LastSeen = time.Now()
	dev.IsOnline = true

	return tmp.Name(), cleanup, nil
}

// produceRemux runs the actual ffmpeg remux into cachePath, or - if another
// request for the same source is already doing so - waits for that one
// instead of starting a duplicate. Writes to a .tmp sibling and renames
// into place atomically, so a request that reads the cache directory never
// sees (or serves) a partially-written file.
func produceRemux(ctx echo.Context, sourcePath, cachePath string) error {
	cacheKey := filepath.Base(cachePath)

	wgIface, alreadyRunning := remuxInFlight.LoadOrStore(cacheKey, &sync.WaitGroup{})
	wg := wgIface.(*sync.WaitGroup)
	if alreadyRunning {
		wg.Wait()
		if _, err := os.Stat(cachePath); err == nil {
			return nil
		}
		// The in-flight run that finished first failed (no cache file
		// resulted) - fall through and try once more ourselves rather than
		// permanently failing every request that happened to arrive while
		// it was running.
	}
	wg.Add(1)
	defer func() {
		wg.Done()
		remuxInFlight.Delete(cacheKey)
	}()

	tmpPath := cachePath + ".tmp"
	// Not ctx.Request().Context() - that's canceled the moment THIS request
	// disconnects, which would kill the remux out from under every other
	// concurrent request waiting on wg above too. A background context
	// with its own generous timeout means one impatient client closing its
	// tab doesn't cost every other viewer their in-progress remux.
	runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "ffmpeg",
		"-y",
		"-i", sourcePath,
		"-c", "copy",
		"-movflags", "+faststart",
		"-f", "mp4",
		"-loglevel", "error",
		tmpPath,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("ffmpeg remux failed: %w: %s", err, string(output))
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		os.Remove(tmpPath)
		return err
	}

	go trimRemuxCache()
	return nil
}

// trimRemuxCache deletes the least-recently-used cache entries until the
// directory's total size is back under remuxCacheMaxBytes. Runs in its own
// goroutine after each new remux completes, off the request path.
func trimRemuxCache() {
	entries, err := os.ReadDir(remuxCacheDir)
	if err != nil {
		return
	}
	type fileWithInfo struct {
		path    string
		modTime time.Time
		size    int64
	}
	files := make([]fileWithInfo, 0, len(entries))
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		p := filepath.Join(remuxCacheDir, e.Name())
		files = append(files, fileWithInfo{path: p, modTime: info.ModTime(), size: info.Size()})
		total += info.Size()
	}
	if total <= remuxCacheMaxBytes {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })
	for _, f := range files {
		if total <= remuxCacheMaxBytes {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}

// @Summary 获取目录列表
// @Produce  application/json
// @Accept application/json
// @Tags file
// @Security ApiKeyAuth
// @Param path query string false "路径"
// @Success 200 {string} string "ok"
// @Router /file/dirpath [get]
func DirPath(ctx echo.Context) error {
	var req ListReq
	path := ctx.QueryParam("path")
	req.Path = path
	req.Validate()

	// Live companion device file proxy
	if dev, phonePath := GetCompanionDeviceByStoragePath(req.Path); dev != nil {
		phoneFiles, err := FetchCompanionFilesFromDevice(dev, phonePath)
		if err == nil {
			pathList := make([]ObjResp, 0, len(phoneFiles))
			for _, item := range phoneFiles {
				t := ObjResp{
					IsDir:    item.IsDir,
					Name:     item.Name,
					Modified: item.Modified,
					Date:     item.Modified,
					Size:     item.Size,
					Path:     filepath.Join(req.Path, item.Name),
				}
				pathList = append(pathList, t)
			}
			flist := FsListResp{
				Content: pathList,
				Total:   int64(len(pathList)),
				Index:   req.Index,
				Size:    req.Size,
			}
			return ctx.JSON(common_err.SUCCESS, model.Result{
				Success: common_err.SUCCESS,
				Message: common_err.GetMsg(common_err.SUCCESS),
				Data:    flist,
			})
		}
	}

	info, err := service.MyService.System().GetDirPath(req.Path)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}
	// Best-effort: makes this directory eligible for live-update broadcasts
	// (see service/file_watch.go) while it's actually being viewed. Never
	// blocks or fails this listing - only reduces to "no live updates for
	// this folder" if the path can't be watched.
	service.EnsureDirWatch(req.Path)
	shares := service.MyService.Shares().GetSharesList()
	sharesMap := make(map[string]string)
	for _, v := range shares {
		sharesMap[v.Path] = fmt.Sprint(v.ID)
	}
	// if len(info) <= (req.Page-1)*req.Size {
	// 	return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.INVALID_PARAMS), Data: "page out of range"})
	// 	return
	// }
	forEnd := req.Index * req.Size
	if forEnd > len(info) {
		forEnd = len(info)
	}
	for i := (req.Index - 1) * req.Size; i < forEnd; i++ {
		if v, ok := sharesMap[info[i].Path]; ok {
			ex := make(map[string]interface{})
			shareEx := make(map[string]string)
			shareEx["shared"] = "true"
			shareEx["id"] = v
			ex["share"] = shareEx
			ex["mounted"] = false
			info[i].Extensions = ex
		}
	}
	if strings.HasPrefix(req.Path, "/mnt") || strings.HasPrefix(req.Path, "/media") {
		for i := (req.Index - 1) * req.Size; i < forEnd; i++ {
			ex := info[i].Extensions
			if ex == nil {
				ex = make(map[string]interface{})
			}
			mounted := service.IsMounted(info[i].Path)
			ex["mounted"] = mounted
			info[i].Extensions = ex
		}
	}
	// Hide the files or folders in operation
	fileQueue := make(map[string]string)
	opStrArrSnapshot := service.OpStrArrSnapshot()
	if len(opStrArrSnapshot) > 0 {
		for _, v := range opStrArrSnapshot {
			v, ok := service.FileQueue.Load(v)
			if !ok {
				continue
			}
			vt := v.(model.FileOperate)
			for _, i := range vt.Item {
				lastPath := i.From[strings.LastIndex(i.From, "/")+1:]
				fileQueue[vt.To+"/"+lastPath] = i.From
			}
		}
	}

	pathList := []ObjResp{}
	for i := (req.Index - 1) * req.Size; i < forEnd; i++ {
		if info[i].Name == ".temp" && info[i].IsDir {
			continue
		}
		if _, ok := fileQueue[info[i].Path]; !ok {
			t := ObjResp{}
			t.IsDir = info[i].IsDir
			t.Name = info[i].Name
			t.Modified = info[i].Date
			t.Date = info[i].Date
			t.Size = info[i].Size
			t.Path = info[i].Path
			t.Extensions = info[i].Extensions
			pathList = append(pathList, t)

		}
	}
	flist := FsListResp{
		Content: pathList,
		Total:   int64(len(info)),
		// Readme:   "",
		// Write:    true,
		// Provider: "local",
		Index: req.Index,
		Size:  req.Size,
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: flist})
}

// @Summary rename file or dir
// @Produce  application/json
// @Accept application/json
// @Tags file
// @Security ApiKeyAuth
// @Param oldpath body string true "path of old"
// @Param newpath body string true "path of new"
// @Success 200 {string} string "ok"
// @Router /file/rename [put]
func RenamePath(ctx echo.Context) error {
	json := make(map[string]string)
	ctx.Bind(&json)
	op := json["old_path"]
	np := json["new_path"]
	if len(op) == 0 || len(np) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	}
	mounted := service.IsMounted(op)
	if mounted {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.MOUNTED_DIRECTIORIES, Message: common_err.GetMsg(common_err.MOUNTED_DIRECTIORIES), Data: common_err.GetMsg(common_err.MOUNTED_DIRECTIORIES)})
	}

	success, err := service.MyService.System().RenameFile(op, np)
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: success, Message: common_err.GetMsg(success), Data: err})
}

// @Summary create folder
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param path body string true "path of folder"
// @Success 200 {string} string "ok"
// @Router /file/mkdir [post]
func MkdirAll(ctx echo.Context) error {
	json := make(map[string]string)
	ctx.Bind(&json)
	path := json["path"]
	var code int
	if len(path) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	}
	// decodedPath, err := url.QueryUnescape(path)
	// if err != nil {
	// 	return ctx.JSON(http.StatusOK, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	// 	return
	// }
	code, _ = service.MyService.System().MkdirAll(path)
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: code, Message: common_err.GetMsg(code)})
}

// @Summary create file
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param path body string true "path of folder (path need to url encode)"
// @Success 200 {string} string "ok"
// @Router /file/create [post]
func PostCreateFile(ctx echo.Context) error {
	json := make(map[string]string)
	ctx.Bind(&json)
	path := json["path"]
	var code int
	if len(path) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	}
	// decodedPath, err := url.QueryUnescape(path)
	// if err != nil {
	// 	return ctx.JSON(http.StatusOK, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	// 	return
	// }
	code, _ = service.MyService.System().CreateFile(path)
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: code, Message: common_err.GetMsg(code)})
}

// @Summary archive files/folders into a single zip
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param files body []string true "absolute paths to archive"
// @Param destination body string true "destination .zip path - must not already exist"
// @Success 200 {string} string "ok"
// @Router /file/archive [post]
func PostArchiveFiles(ctx echo.Context) error {
	req := struct {
		Files       []string `json:"files"`
		Destination string   `json:"destination"`
	}{}
	if err := ctx.Bind(&req); err != nil || len(req.Files) == 0 || len(req.Destination) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	}
	for _, f := range req.Files {
		if !file.Exists(f) {
			return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.FILE_DOES_NOT_EXIST, Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST)})
		}
	}
	if file.Exists(req.Destination) {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.FILE_ALREADY_EXISTS, Message: common_err.GetMsg(common_err.FILE_ALREADY_EXISTS)})
	}
	if err := archiver.DefaultZip.Archive(req.Files, req.Destination); err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// @Summary extract an archive (zip/tar/tar.gz/tar.bz2/...) into a destination folder
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param path body string true "path of the archive file"
// @Param destination body string true "destination folder - must not already exist"
// @Success 200 {string} string "ok"
// @Router /file/unarchive [post]
func PostUnarchiveFile(ctx echo.Context) error {
	req := struct {
		Path        string `json:"path"`
		Destination string `json:"destination"`
	}{}
	if err := ctx.Bind(&req); err != nil || len(req.Path) == 0 || len(req.Destination) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	}
	if !file.Exists(req.Path) {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.FILE_DOES_NOT_EXIST, Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST)})
	}
	if file.Exists(req.Destination) {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.FILE_ALREADY_EXISTS, Message: common_err.GetMsg(common_err.FILE_ALREADY_EXISTS)})
	}
	if err := os.MkdirAll(req.Destination, 0o755); err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}
	if err := archiver.Unarchive(req.Path, req.Destination); err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// @Summary upload file
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param path formData string false "file path"
// @Param file formData file true "file"
// @Success 200 {string} string "ok"
// @Router /file/upload [get]
func GetFileUpload(ctx echo.Context) error {
	relative := ctx.QueryParam("relativePath")
	fileName := ctx.QueryParam("filename")
	chunkNumber := ctx.QueryParam("chunkNumber")
	totalChunks, _ := strconv.Atoi(utils.DefaultQuery(ctx, "totalChunks", "0"))
	path := ctx.QueryParam("path")
	dirPath := ""
	hash := file.GetHashByContent([]byte(fileName))
	if file.Exists(path + "/" + relative) {
		return ctx.JSON(http.StatusConflict, model.Result{Success: http.StatusConflict, Message: common_err.GetMsg(common_err.FILE_ALREADY_EXISTS)})
	}
	tempDir := filepath.Join(path, ".temp", hash+strconv.Itoa(totalChunks)) + "/"
	if fileName != relative {
		dirPath = strings.TrimSuffix(relative, fileName)
		tempDir += dirPath
		file.MkDir(path + "/" + dirPath)
	}
	tempDir += chunkNumber
	if !file.CheckNotExist(tempDir) {
		return ctx.JSON(200, model.Result{Success: 200, Message: common_err.GetMsg(common_err.FILE_ALREADY_EXISTS)})
	}

	return ctx.JSON(204, model.Result{Success: 204, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// @Summary upload file
// @Produce  application/json
// @Accept  multipart/form-data
// @Tags file
// @Security ApiKeyAuth
// @Param path formData string false "file path"
// @Param file formData file true "file"
// @Success 200 {string} string "ok"
// @Router /file/upload [post]
func PostFileUpload(ctx echo.Context) error {
	f, _, _ := ctx.Request().FormFile("file")
	relative := ctx.FormValue("relativePath")
	fileName := ctx.FormValue("filename")
	totalChunks, _ := strconv.Atoi(utils.DefaultPostForm(ctx, "totalChunks", "0"))
	chunkNumber := ctx.FormValue("chunkNumber")
	dirPath := ""
	path := ctx.FormValue("path")

	hash := file.GetHashByContent([]byte(fileName))

	if len(path) == 0 {
		logger.Error("path should not be empty")
		return ctx.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	}
	tempDir := filepath.Join(path, ".temp", hash+strconv.Itoa(totalChunks)) + "/"

	if fileName != relative {
		dirPath = strings.TrimSuffix(relative, fileName)
		tempDir += dirPath
		if err := file.MkDir(path + "/" + dirPath); err != nil {
			logger.Error("error when trying to create `"+path+"/"+dirPath+"`", zap.Error(err))
			return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		}
	}

	path += "/" + relative

	if !file.CheckNotExist(tempDir + chunkNumber) {
		if err := file.RMDir(tempDir + chunkNumber); err != nil {
			logger.Error("error when trying to remove existing `"+tempDir+chunkNumber+"`", zap.Error(err))
			return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		}
	}

	if totalChunks > 1 {
		if err := file.IsNotExistMkDir(tempDir); err != nil {
			logger.Error("error when trying to create `"+tempDir+"`", zap.Error(err))
			return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		}

		out, err := os.OpenFile(tempDir+chunkNumber, os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			logger.Error("error when trying to open `"+tempDir+chunkNumber+"` for creation", zap.Error(err))
			return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		}

		defer out.Close()

		if _, err := io.Copy(out, f); err != nil { // recommend to use https://github.com/iceber/iouring-go for faster copy
			logger.Error("error when trying to write to `"+tempDir+chunkNumber+"`", zap.Error(err))
			return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		}

		fileNum, err := ioutil.ReadDir(tempDir)
		if err != nil {
			logger.Error("error when trying to read number of files under `"+tempDir+"`", zap.Error(err))
			return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		}

		if totalChunks == len(fileNum) {
			if err := file.SpliceFiles(tempDir, path, totalChunks, 1); err != nil {
				logger.Error("error when trying to splice files under `"+tempDir+"`", zap.Error(err))
				return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
			}
			go func() {
				time.Sleep(11 * time.Second)
				if err := file.RMDir(tempDir); err != nil {
					logger.Error("error when trying to remove `"+tempDir+"`", zap.Error(err))
				}
			}()
		}
	} else {
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			logger.Error("error when trying to open `"+path+"` for creation", zap.Error(err))
			return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		}

		defer out.Close()

		if _, err := io.Copy(out, f); err != nil { // recommend to use https://github.com/iceber/iouring-go for faster copy
			logger.Error("error when trying to write to `"+path+"`", zap.Error(err))
			return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
		}
	}
	return ctx.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

func PostFileOctet(ctx echo.Context) error {
	content_length := ctx.Request().ContentLength
	if content_length <= 0 || content_length > 1024*1024*1024*2*1024 {
		log.Printf("content_length error\n")
		return ctx.JSON(http.StatusBadRequest, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: "content_length error"})
	}
	content_type_, has_key := ctx.Request().Header["Content-Type"]
	if !has_key {
		log.Printf("Content-Type error\n")
		return ctx.JSON(http.StatusBadRequest, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: "Content-Type error"})
	}
	if len(content_type_) != 1 {
		log.Printf("Content-Type count error\n")
		return ctx.JSON(http.StatusBadRequest, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: "Content-Type count error"})
	}
	content_type := content_type_[0]
	const BOUNDARY string = "; boundary="
	loc := strings.Index(content_type, BOUNDARY)
	if loc == -1 {
		log.Printf("Content-Type error, no boundary\n")
		return ctx.JSON(http.StatusBadRequest, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: "Content-Type error, no boundary"})
	}
	boundary := []byte(content_type[(loc + len(BOUNDARY)):])
	log.Printf("[%s]\n\n", boundary)
	read_data := make([]byte, 1024*24)
	var read_total int = 0
	for {
		file_header, file_data, err := file.ParseFromHead(read_data, read_total, append(boundary, []byte("\r\n")...), ctx.Request().Body)
		if err != nil {
			log.Printf("%v", err)
		}
		log.Printf("file :%s\n", file_header)
		//
		//os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o644)
		f, err := os.OpenFile(file_header["path"]+"/"+file_header["filename"], os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			log.Printf("create file fail:%v\n", err)
		}
		f.Write(file_data)
		file_data = nil

		temp_data, reach_end, err := file.ReadToBoundary(boundary, ctx.Request().Body, f)
		f.Close()
		if err != nil {
			log.Printf("%v\n", err)
		}
		if reach_end {
			break
		} else {
			copy(read_data[0:], temp_data)
			read_total = len(temp_data)
			continue
		}
	}
	return ctx.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// @Summary copy or move file
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param body body model.FileOperate true "type:move,copy"
// @Success 200 {string} string "ok"
// @Router /file/operate [post]
func PostOperateFileOrDir(ctx echo.Context) error {
	list := model.FileOperate{}
	ctx.Bind(&list)

	if len(list.Item) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	}
	if list.To == list.Item[0].From[:strings.LastIndex(list.Item[0].From, "/")] {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SOURCE_DES_SAME, Message: common_err.GetMsg(common_err.SOURCE_DES_SAME)})
	}

	// Source-size lookup used to run right here, synchronously, before this
	// handler could respond at all - for a large folder that's a real
	// multi-second stall (a full recursive filepath.Walk per item) with zero
	// UI feedback the whole time, not perceived lag. The mount check for
	// "move" still runs up front (it's a fast lookup, not a tree walk, and
	// this request must still be able to reject a move off a mounted path
	// before queuing anything); actual sizes are now computed by
	// ComputeOperateSizes after the task is already queued and copying has
	// already started, since copying itself never needed them - only the
	// percentage shown to the user did. -1 marks "not computed yet" (0 would
	// be indistinguishable from "processed >= total", which is checked
	// elsewhere as "already finished").
	for i := 0; i < len(list.Item); i++ {
		if list.Type == "move" {
			mounted := service.IsMounted(list.Item[i].From)
			if mounted {
				return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.MOUNTED_DIRECTIORIES, Message: common_err.GetMsg(common_err.MOUNTED_DIRECTIORIES), Data: common_err.GetMsg(common_err.MOUNTED_DIRECTIORIES)})
			}
		}
		list.Item[i].Size = -1
	}

	list.TotalSize = -1
	list.ProcessedSize = 0

	uid := uuid.NewString()
	service.FileQueue.Store(uid, list)
	if service.OpStrArrPush(uid) {
		go service.ExecOpFile()
		go service.CheckFileStatus()

		go service.MyService.Notify().SendFileOperateNotify(false)

	}
	go service.ComputeOperateSizes(uid)

	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// @Summary delete file
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param body body string true "paths eg ["/a/b/c","/d/e/f"]"
// @Success 200 {string} string "ok"
// @Router /file/delete [delete]
func DeleteFile(ctx echo.Context) error {
	paths := []string{}
	body, err := io.ReadAll(ctx.Request().Body)
	if err == nil && len(body) > 0 {
		// Try parsing as plain []string first (filter out empty strings that
		// appear when the JSON actually contains objects instead of strings)
		var rawPaths []string
		if json.Unmarshal(body, &rawPaths) == nil {
			for _, p := range rawPaths {
				if p != "" {
					paths = append(paths, p)
				}
			}
		}
		if len(paths) == 0 {
			var objList []map[string]interface{}
			if err := json.Unmarshal(body, &objList); err == nil {
				for _, o := range objList {
					if p, ok := o["path"].(string); ok && p != "" {
						paths = append(paths, p)
					}
				}
			}
		}
		if len(paths) == 0 {
			var singleObj map[string]interface{}
			if err := json.Unmarshal(body, &singleObj); err == nil {
				if p, ok := singleObj["path"].(string); ok && p != "" {
					paths = append(paths, p)
				}
			}
		}
	}
	if len(paths) == 0 {
		q := ctx.QueryParam("path")
		if q != "" {
			paths = strings.Split(q, ",")
		}
	}
	if len(paths) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	}

	// Protected paths are skipped, not fatal to the whole request - this
	// used to return immediately (deleting nothing at all) the moment ANY
	// one path in a batch was a mount root/protected directory, so
	// selecting 10 ordinary files plus one USB drive folder by mistake
	// silently deleted none of them, with no indication why. Each path is
	// judged on its own now: a protected one is left alone and reported
	// back; everything else in the same request still gets deleted.
	protected := []string{}
	deletable := make([]string, 0, len(paths))
	for _, v := range paths {
		if v == "" {
			continue
		}
		cleanV := filepath.Clean(v)
		// Protect system root and top-level storage directories from deletion
		if cleanV == "/" || cleanV == "/DATA" || cleanV == "/mnt" || cleanV == "/DATA/Documents" || cleanV == "/DATA/Downloads" || cleanV == "/DATA/Media" || cleanV == "/DATA/Gallery" || cleanV == "/DATA/AppData" || cleanV == "/DATA/Companion" || cleanV == "/DATA/vm-share" {
			protected = append(protected, v)
			continue
		}
		// If cleanV is directly under /mnt (e.g. /mnt/<host> or /mnt/<cloud>), refuse file/delete
		if strings.HasPrefix(cleanV, "/mnt/") && len(strings.Split(strings.TrimPrefix(cleanV, "/mnt/"), "/")) <= 1 {
			protected = append(protected, v)
			continue
		}
		if service.IsMounted(cleanV) {
			protected = append(protected, v)
			continue
		}
		deletable = append(deletable, v)
	}
	if len(protected) > 0 && len(deletable) == 0 {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.MOUNTED_DIRECTIORIES, Message: "Cannot delete a mounted drive, connected location, or system storage directory. Eject or disconnect it instead.", Data: protected})
	}

	for _, v := range deletable {
		cleanV := filepath.Clean(v)
		if dev, phonePath := GetCompanionDeviceByStoragePath(cleanV); dev != nil {
			cleanPhone := filepath.Clean(phonePath)
			root := filepath.Clean(dev.RootPath)
			if root == "" || root == "." {
				root = "/storage/emulated/0"
			}
			// Only proxy delete if NOT the phone root! NEVER wipe the whole phone storage!
			if cleanPhone != "" && cleanPhone != "/" && cleanPhone != "." && cleanPhone != root {
				ProxyCompanionFileDelete(dev, phonePath)
			}
		}
		// Do not run os.RemoveAll if cleanV is a companion device root folder
		companionMu.RLock()
		isCompanionRoot := false
		for _, dev := range companionDevices {
			if dev.StoragePath != "" && cleanV == filepath.Clean(dev.StoragePath) {
				isCompanionRoot = true
				break
			}
		}
		companionMu.RUnlock()
		if isCompanionRoot {
			_ = os.Remove(cleanV)
			continue
		}

		err := os.RemoveAll(v)
		if err != nil && !os.IsNotExist(err) {
			if dev, _ := GetCompanionDeviceByStoragePath(v); dev == nil {
				return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.FILE_DELETE_ERROR, Message: common_err.GetMsg(common_err.FILE_DELETE_ERROR), Data: err})
			}
		}
	}

	if len(protected) > 0 {
		return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: "Deleted, except for a mounted drive, connected location, or system storage directory - eject or disconnect it instead.", Data: protected})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// @Summary update file
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param path body string true "path"
// @Param content body string true "content"
// @Success 200 {string} string "ok"
// @Router /file/update [put]
func PutFileContent(ctx echo.Context) error {
	fi := model.FileUpdate{}
	ctx.Bind(&fi)

	// path := ctx.FormValue("path")
	// content := ctx.FormValue("content")
	if !file.Exists(fi.FilePath) {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.FILE_ALREADY_EXISTS, Message: common_err.GetMsg(common_err.FILE_ALREADY_EXISTS)})
	}
	// err := os.Remove(path)
	f, err := os.Stat(fi.FilePath)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.FILE_ALREADY_EXISTS, Message: common_err.GetMsg(common_err.FILE_ALREADY_EXISTS)})
	}
	fm := f.Mode()
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.FILE_DELETE_ERROR, Message: common_err.GetMsg(common_err.FILE_DELETE_ERROR), Data: err})
	}
	os.OpenFile(fi.FilePath, os.O_CREATE, fm)
	err = file.WriteToFullPath([]byte(fi.FileContent), fi.FilePath, fm)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// @Summary image thumbnail/original image
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param path query string true "path"
// @Param type query string false "original,thumbnail" Enums(original,thumbnail)
// @Success 200 {string} string "ok"
// @Router /file/image [get]
func GetFileImage(ctx echo.Context) error {
	t := ctx.QueryParam("type")
	path := ctx.QueryParam("path")
	if dev, phonePath := GetCompanionDeviceByStoragePath(path); dev != nil {
		if !file.Exists(path) {
			if err := ProxyCompanionStream(dev, phonePath, ctx.Response().Writer, ctx.Request()); err == nil {
				return nil
			}
		}
	}
	if !file.Exists(path) {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.FILE_DOES_NOT_EXIST, Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST)})
	}
	if t == "thumbnail" {
		f, err := file.GetImage(path, 100, 0)
		if err != nil {
			return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
		}
		if kind, _ := filetype.Match(f); kind != filetype.Unknown {
			ctx.Response().Header().Set("Content-Type", kind.MIME.Value)
		}
		ctx.Response().Writer.Write(f)
		// Without this, execution fell through into the full-image path
		// below on every thumbnail request - re-reading and appending the
		// entire original file's bytes right after the thumbnail into the
		// same response body, so every thumbnail request downloaded the
		// whole original image for nothing (and produced a technically
		// invalid response body two images concatenated together).
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}
	defer f.Close()
	data, err := ioutil.ReadAll(f)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}
	if kind, _ := filetype.Match(data); kind != filetype.Unknown {
		ctx.Response().Header().Set("Content-Type", kind.MIME.Value)
	}
	ctx.Response().Writer.Write(data)
	return nil
}

func DeleteOperateFileOrDir(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "0" {
		service.CancelAllOperateTasks()
	} else {
		service.CancelOperateTask(id)
	}

	go service.MyService.Notify().SendFileOperateNotify(true)
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

func GetSize(ctx echo.Context) error {
	json := make(map[string]string)
	ctx.Bind(&json)
	path := json["path"]
	size, err := file.GetFileOrDirSize(path)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: size})
}

func GetFileCount(ctx echo.Context) error {
	json := make(map[string]string)
	ctx.Bind(&json)
	path := json["path"]
	list, err := ioutil.ReadDir(path)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: len(list)})
}

type CenterHandler struct {
	// 广播通道，有数据则循环每个用户广播出去
	broadcast chan []byte
	// 注册通道，有用户进来 则推到用户集合map中
	register chan *Client
	// 注销通道，有用户关闭连接 则将该用户剔出集合map中
	unregister chan *Client
	// 用户集合，每个用户本身也在跑两个协程，监听用户的读、写的状态
	clients map[string]*Client
}

type Client struct {
	handler *CenterHandler
	conn    *websocket.Conn
	// 每个用户自己的循环跑起来的状态监控
	send         chan []byte
	ID           string       `json:"id"`
	IP           string       `json:"ip"`
	Name         service.Name `json:"name"`
	RtcSupported bool         `json:"rtcSupported"`
	TimerId      int          `json:"timerId"`
	LastBeat     time.Time    `json:"lastBeat"`
}

type PeerModel struct {
	ID           string       `json:"id"`
	Name         service.Name `json:"name"`
	RtcSupported bool         `json:"rtcSupported"`
}

func ConnectWebSocket(ctx echo.Context) error {
	peerId := ctx.QueryParam("peer")
	writer := ctx.Response().Writer
	request := ctx.Request()
	key := uuid.NewString()
	// peerModel := service.MyService.Peer().GetPeerByUserAgent(ctx.Request().UserAgent())
	peerModel := model2.PeerDriveDBModel{}
	name := service.GetName(request)
	if conn, err = upgraderFile.Upgrade(writer, request, writer.Header()); err != nil {
		log.Println(err)
	}
	client := &Client{handler: &handler, conn: conn, send: make(chan []byte, 256), ID: service.GetPeerId(request, key), IP: service.GetIP(request), Name: name, RtcSupported: true, TimerId: 0, LastBeat: time.Now()}
	if peerId != "" || len(peerModel.ID) > 0 {
		if len(peerModel.ID) == 0 {
			peerModel = service.MyService.Peer().GetPeerByID(peerId)
		}
		if len(peerModel.ID) > 0 {
			key = peerId
			client.ID = peerModel.ID
			client.Name = service.GetNameByDB(peerModel)
		}
	}
	list := service.MyService.Peer().GetPeers()
	if len(peerModel.ID) == 0 {
		peerModel.ID = key
		peerModel.DisplayName = name.DisplayName
		peerModel.DeviceName = name.DeviceName
		peerModel.Model = name.Model
		peerModel.OS = name.OS
		peerModel.Browser = name.Browser
		peerModel.UserAgent = ctx.Request().UserAgent()
		peerModel.IP = client.IP
		service.MyService.Peer().CreatePeer(&peerModel)
		list = append(list, peerModel)
	}

	cookie := http.Cookie{
		Name:  "peerid",
		Value: key,
		Path:  "/",
	}
	http.SetCookie(writer, &cookie)
	if len(list) > 10 {
		kickoutList := []Client{}
		count := len(list) - 10
		for i := len(list) - 1; count > 0 && i > -1; i-- {
			if _, ok := handler.clients[list[i].ID]; !ok {
				count--
				kickoutList = append(kickoutList, Client{ID: list[i].ID, Name: service.GetNameByDB(list[i]), IP: list[i].IP})
				service.MyService.Peer().DeletePeer(list[i].ID)
			}
		}
		// if len(kickoutList) > 0 {
		// 	other := make(map[string]interface{})
		// 	other["type"] = "kickout"
		// 	other["peers"] = kickoutList
		// 	otherBy, err := json.Marshal(other)
		// 	fmt.Println(err)
		// 	client.handler.broadcast <- otherBy
		// }
	}
	list = service.MyService.Peer().GetPeers()
	if len(list) > 10 {
		fmt.Println("解决完后依然有溢出", list)
	}
	currentPeer := PeerModel{ID: client.ID, Name: client.Name, RtcSupported: client.RtcSupported}
	pmsg := make(map[string]interface{})
	pmsg["type"] = "peer-joined"
	pmsg["peer"] = currentPeer
	pby, err := json.Marshal(pmsg)
	fmt.Println(err)
	for _, v := range handler.clients {
		v.send <- pby
	}
	// client.handler.broadcast <- pby
	clients := []PeerModel{}
	for _, v := range client.handler.clients {
		if _, ok := handler.clients[v.ID]; ok {
			clients = append(clients, PeerModel{ID: v.ID, Name: v.Name, RtcSupported: v.RtcSupported})
		}
	}

	other := make(map[string]interface{})
	other["type"] = "peers"
	other["peers"] = clients
	otherBy, err := json.Marshal(other)
	fmt.Println(err)
	client.send <- otherBy

	// 推给监控中心注册到用户集合中
	handler.register <- client

	client.send <- []byte(`{"type":"ping"}`)

	data := make(map[string]string)
	data["displayName"] = client.Name.DisplayName
	data["deviceName"] = client.Name.DeviceName
	data["id"] = client.ID
	msg := make(map[string]interface{})
	msg["type"] = "display-name"
	msg["message"] = data
	by, _ := json.Marshal(msg)
	client.send <- by

	// 每个 client 都挂起 2 个新的协程，监控读、写状态
	go client.writePump()
	go client.readPump()
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

var handler = CenterHandler{
	broadcast:  make(chan []byte),
	register:   make(chan *Client),
	unregister: make(chan *Client),
	clients:    make(map[string]*Client),
}

func init() {
	// 起个协程跑起来，监听注册、注销、消息 3 个 channel
	go handler.monitoring()

	crontab := cron.New(cron.WithSeconds()) // 精确到秒
	// 定义定时器调用的任务函数

	task := func() {
		handler.broadcast <- []byte(`{"type":"ping"}`)
	}
	// 定时任务
	spec := "*/30 * * * * ?" // cron表达式，每五秒一次
	// 添加定时任务,
	crontab.AddFunc(spec, task)
	// 启动定时器
	crontab.Start()
}

func (c *Client) writePump() {
	defer func() {
		c.handler.unregister <- c

		c.conn.Close()
	}()
	for {
		// 广播推过来的新消息，马上通过websocket推给自己
		message, _ := <-c.send
		fmt.Println("推送消息", string(message), "1")
		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			return
		}
	}
}

// 读，监听客户端是否有推送内容过来服务端
func (c *Client) readPump() {
	defer func() {
		c.handler.unregister <- c
		c.conn.Close()
	}()
	for {
		// 循环监听是否该用户是否要发言
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			// 异常关闭的处理
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			c.handler.broadcast <- []byte(`{"type":"peer-left","peerId":"` + c.ID + `"}`)
			break
		}
		// 要的话，推给广播中心，广播中心再推给每个用户

		t := gjson.GetBytes(message, "type")
		if t.String() == "disconnect" {
			c.handler.unregister <- c
			c.conn.Close()
			// clients := []Client{}
			// list := service.MyService.Peer().GetPeers()
			// for _, v := range list {
			// 	if _, ok := handler.clients[v.ID]; ok {
			// 		clients = append(clients, *handler.clients[v.ID])
			// 	} else {
			// 		clients = append(clients, Client{ID: v.ID, Name: service.GetNameByDB(v), IP: v.IP, Offline: true})
			// 	}
			// }
			// other := make(map[string]interface{})
			// other["type"] = "peers"
			// other["peers"] = clients
			// otherBy, err := json.Marshal(other)
			// fmt.Println(err)
			c.handler.broadcast <- []byte(`{"type":"peer-left","peerId":"` + c.ID + `"}`)
			// c.handler.broadcast <- otherBy
			break
		} else if t.String() == "pong" {
			c.LastBeat = time.Now()
			continue
		}
		to := gjson.GetBytes(message, "to")

		if len(to.String()) > 0 {
			toC := c.handler.clients[to.String()]
			if toC == nil {
				continue
			}
			data := map[string]interface{}{}
			json.Unmarshal(message, &data)
			data["sender"] = c.ID
			delete(data, "to")
			message, err = json.Marshal(data)
			toC.send <- message
			continue
		}

		c.handler.broadcast <- message
	}
}

func (ch *CenterHandler) monitoring() {
	for {
		select {
		// 注册，新用户连接过来会推进注册通道，这里接收推进来的用户指针
		case client := <-ch.register:
			ch.clients[client.ID] = client
			// 注销，关闭连接或连接异常会将用户推出群聊
		case client := <-ch.unregister:
			delete(ch.clients, client.ID)
			// 消息，监听到有新消息到来
		case message := <-ch.broadcast:
			println("消息来了，message：" + string(message))
			// 推送给每个用户的通道，每个用户都有跑协程起了writePump的监听
			for _, client := range ch.clients {
				client.send <- message
			}
		}
	}
}

func GetPeers(ctx echo.Context) error {
	peers := service.MyService.Peer().GetPeers()
	for i := 0; i < len(peers); i++ {
		if _, ok := handler.clients[peers[i].ID]; ok {
			peers[i].Online = true
		}
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: peers})
}
