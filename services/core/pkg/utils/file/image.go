package file

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dsoprea/go-exif/v3"
	exifcommon "github.com/dsoprea/go-exif/v3/common"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
)

// thumbMaxPixels caps what a thumbnail request will decode: a tiny file can
// claim a huge canvas, and the decoder allocates it up front (fatal OOM).
// ponytail: 100 MP covers today's cameras; raise it for bigger panoramas.
const thumbMaxPixels = 100_000_000

func GetImage(path string, width, height int) (thumb []byte, err error) {
	// The EXIF and image decoders parse whatever file the user points at;
	// a panic in them is a bad file, not a reason to drop the request.
	defer func() {
		if r := recover(); r != nil {
			thumb, err = nil, fmt.Errorf("could not read image: %v", r)
		}
	}()
	if thumbnail, err := GetThumbnailByOwnerPhotos(path); err == nil {
		return thumbnail, nil
	} else {
		return GetThumbnailByWebPhoto(path, width, height)
	}
}
func GetThumbnailByOwnerPhotos(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	buff := &bytes.Buffer{}

	defer file.Close()
	offset := 0
	offsets := []int{12, 30}

	head := make([]byte, 0xffff)

	r := io.TeeReader(file, buff)
	_, err = r.Read(head)
	if err != nil {
		return nil, err
	}

	for _, offset = range offsets {
		if _, err = exif.ParseExifHeader(head[offset:]); err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	im, err := exifcommon.NewIfdMappingWithStandard()
	if err != nil {
		return nil, err
	}

	_, index, err := exif.Collect(im, exif.NewTagIndex(), head[offset:])
	if err != nil {
		return nil, err
	}

	ifd := index.RootIfd.NextIfd()
	if ifd == nil {
		return nil, exif.ErrNoThumbnail
	}
	thumbnail, err := ifd.Thumbnail()
	if err != nil {
		return nil, err
	}
	return thumbnail, nil
}
func GetThumbnailByWebPhoto(path string, width, height int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return nil, err
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width*cfg.Height > thumbMaxPixels {
		return nil, fmt.Errorf("image too large to thumbnail: %dx%d", cfg.Width, cfg.Height)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}

	if height == 0 {
		height = max(1, cfg.Height*width/cfg.Width)
	}
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)

	buf := bytes.Buffer{}
	if format == "jpeg" {
		err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 90})
	} else {
		err = png.Encode(&buf, dst)
	}
	return buf.Bytes(), err
}

func ImageExtArray() []string {

	ext := []string{
		"ase",
		"art",
		"bmp",
		"blp",
		"cd5",
		"cit",
		"cpt",
		"cr2",
		"cut",
		"dds",
		"dib",
		"djvu",
		"egt",
		"exif",
		"gif",
		"gpl",
		"grf",
		"icns",
		"ico",
		"iff",
		"jng",
		"jpeg",
		"jpg",
		"jfif",
		"jp2",
		"jps",
		"lbm",
		"max",
		"miff",
		"mng",
		"msp",
		"nitf",
		"ota",
		"pbm",
		"pc1",
		"pc2",
		"pc3",
		"pcf",
		"pcx",
		"pdn",
		"pgm",
		"PI1",
		"PI2",
		"PI3",
		"pict",
		"pct",
		"pnm",
		"pns",
		"ppm",
		"psb",
		"psd",
		"pdd",
		"psp",
		"px",
		"pxm",
		"pxr",
		"qfx",
		"raw",
		"rle",
		"sct",
		"sgi",
		"rgb",
		"int",
		"bw",
		"tga",
		"tiff",
		"tif",
		"vtf",
		"xbm",
		"xcf",
		"xpm",
		"3dv",
		"amf",
		"ai",
		"awg",
		"cgm",
		"cdr",
		"cmx",
		"dxf",
		"e2d",
		"egt",
		"eps",
		"fs",
		"gbr",
		"odg",
		"svg",
		"stl",
		"vrml",
		"x3d",
		"sxd",
		"v2d",
		"vnd",
		"wmf",
		"emf",
		"art",
		"xar",
		"png",
		"webp",
		"jxr",
		"hdp",
		"wdp",
		"cur",
		"ecw",
		"iff",
		"lbm",
		"liff",
		"nrrd",
		"pam",
		"pcx",
		"pgf",
		"sgi",
		"rgb",
		"rgba",
		"bw",
		"int",
		"inta",
		"sid",
		"ras",
		"sun",
		"tga",
	}

	return ext
}

/**
* @description:get a image's ext
* @param {string} path "file path"
* @return {string} ext "file ext"
* @return {error} err "error info"
 */
func GetImageExt(p string) (string, error) {
	file, err := os.Open(p)
	if err != nil {
		return "", err
	}

	buff := make([]byte, 512)

	_, err = file.Read(buff)

	if err != nil {
		return "", err
	}

	filetype := http.DetectContentType(buff)

	ext := ImageExtArray()

	for i := 0; i < len(ext); i++ {
		if strings.Contains(ext[i], filetype[6:]) {
			return ext[i], nil
		}
	}

	return "", errors.New("invalid image type")
}

func GetImageExtByName(p string) (string, error) {

	extArr := ImageExtArray()
	ext := filepath.Ext(p)
	for i := 0; i < len(extArr); i++ {
		if strings.Contains(ext, extArr[i]) {
			return extArr[i], nil
		}
	}
	return "", errors.New("invalid image type")
}
