package v2

import (
	"net/http"
	"strconv"

	"github.com/F-e-n-y-x/NivaroOS/services/core/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	"github.com/labstack/echo/v4"
)

// Path: route/v2/file.go

func (s *NivaroOS) GetFileTest(ctx echo.Context) error {

	//http.ServeFile(w, r, r.URL.Path[1:])
	http.ServeFile(ctx.Response().Writer, ctx.Request(), "/DATA/test.img")

	return ctx.String(200, "pong")
}

func (c *NivaroOS) CheckUploadChunk(ctx echo.Context, params codegen.CheckUploadChunkParams) error {
	chunkNumber, err := strconv.ParseInt(ctx.QueryParam("chunkNumber"), 10, 64)
	if err != nil {
		return ctx.NoContent(http.StatusBadRequest)
	}
	totalSize, _ := strconv.ParseInt(ctx.QueryParam("totalSize"), 10, 64)
	// 200 = the chunk is already here (the uploader skips it: resume);
	// 204 = send it.
	if c.fileUploadService.HasChunk(service.UploadChunk{
		Path:         ctx.QueryParam("path"),
		RelativePath: uploadRelativePath(ctx.QueryParam("relativePath"), ctx.QueryParam("filename")),
		Identifier:   ctx.QueryParam("identifier"),
		ChunkNumber:  chunkNumber,
		TotalSize:    totalSize,
	}) {
		return ctx.NoContent(http.StatusOK)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func uploadRelativePath(rel, name string) string {
	if rel == "" {
		return name
	}
	return rel
}

type uploadError struct {
	Success int    `json:"success"`
	Message string `json:"message"`
}

func (c *NivaroOS) PostUploadFile(ctx echo.Context) error {
	num := func(k string) (int64, bool) {
		v, err := strconv.ParseInt(ctx.FormValue(k), 10, 64)
		return v, err == nil
	}
	chunkNumber, ok1 := num("chunkNumber")
	chunkSize, ok2 := num("chunkSize")
	currentChunkSize, ok3 := num("currentChunkSize")
	totalChunks, ok4 := num("totalChunks")
	totalSize, ok5 := num("totalSize")
	if !(ok1 && ok2 && ok3 && ok4 && ok5) {
		return ctx.JSON(http.StatusBadRequest, uploadError{http.StatusBadRequest, "missing or invalid chunk fields"})
	}
	bin, err := ctx.FormFile("file")
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, uploadError{http.StatusBadRequest, "missing file data"})
	}
	src, err := bin.Open()
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, uploadError{http.StatusInternalServerError, err.Error()})
	}
	defer src.Close()

	res, err := c.fileUploadService.Upload(service.UploadChunk{
		Path:             ctx.FormValue("path"),
		RelativePath:     uploadRelativePath(ctx.FormValue("relativePath"), ctx.FormValue("filename")),
		Identifier:       ctx.FormValue("identifier"),
		ChunkNumber:      chunkNumber,
		ChunkSize:        chunkSize,
		CurrentChunkSize: currentChunkSize,
		TotalChunks:      totalChunks,
		TotalSize:        totalSize,
		Data:             src,
	})
	if err != nil {
		// A real message (the old handler returned `{}`), so the upload
		// tray can say why.
		return ctx.JSON(http.StatusInternalServerError, uploadError{http.StatusInternalServerError, err.Error()})
	}
	return ctx.JSON(http.StatusOK, map[string]interface{}{"success": 200, "complete": res.Complete})
}
